package app

import (
	"database/sql"
	"encoding/json"
)

type rowQuerier interface{ QueryRow(string, ...any) *sql.Row }

func activeTranslationJob(q rowQuerier, chapter, segment string) (string, error) {
	var id string
	err := q.QueryRow(`SELECT j.id FROM jobs j JOIN job_items i ON i.job_id=j.id WHERE j.chapter_id=? AND j.kind='translation' AND j.status IN ('queued','running') AND json_extract(i.payload,'$.targets[0].id')=? LIMIT 1`, chapter, segment).Scan(&id)
	return id, err
}
func translationPayload(c Chapter, n int, g Glossary, config Config, force bool) ItemPayload {
	seg := c.Segments[n]
	p := ItemPayload{Targets: []Target{{ID: seg.ID, Text: seg.Text, Version: seg.TranslationRevision}}, Terms: g.Terms, GlossaryRevision: g.Revision, ConfigKey: config.translationKey(), Force: force}
	for _, neighbor := range c.Segments[max(0, n-2):n] {
		p.Context += neighbor.Text + "\n"
	}
	if n+1 < len(c.Segments) {
		p.Context += "\n后续原文参考：" + c.Segments[n+1].Text
	}
	return p
}

type TranslationBatch struct {
	ID        string `json:"id"`
	ChapterID string `json:"chapter_id"`
	Revision  int    `json:"revision"`
	Created   string `json:"created"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	Failed    int    `json:"failed"`
}

func (s *Store) TranslationBatches(chapter string) ([]TranslationBatch, error) {
	return s.translationBatches(chapter, "")
}
func (s *Store) translationBatches(chapter, identity string) ([]TranslationBatch, error) {
	rows, err := s.DB.Query(`SELECT b.id,b.chapter_id,b.revision,b.created,count(j.id),coalesce(sum(j.status='succeeded'),0),coalesce(sum(j.status IN ('failed','partial_failed')),0),coalesce(sum(j.status='running'),0),coalesce(sum(j.status='queued'),0),coalesce(sum(j.status='interrupted'),0),coalesce(sum(j.status='invalidated'),0)
 FROM translation_batches b LEFT JOIN translation_batch_jobs bj ON bj.batch_id=b.id LEFT JOIN jobs j ON j.id=bj.job_id WHERE b.chapter_id=? AND (?='' OR b.id=?) GROUP BY b.id ORDER BY b.rowid DESC LIMIT 10`, chapter, identity, identity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TranslationBatch{}
	for rows.Next() {
		var b TranslationBatch
		var running, queued, interrupted, invalidated int
		if err = rows.Scan(&b.ID, &b.ChapterID, &b.Revision, &b.Created, &b.Total, &b.Done, &b.Failed, &running, &queued, &interrupted, &invalidated); err != nil {
			return nil, err
		}
		switch {
		case running > 0:
			b.Status = "running"
		case queued > 0:
			b.Status = "queued"
		case b.Done == b.Total:
			b.Status = "succeeded"
		case b.Failed > 0:
			if b.Done > 0 {
				b.Status = "partial_failed"
			} else {
				b.Status = "failed"
			}
		case interrupted > 0:
			b.Status = "interrupted"
		case invalidated > 0:
			b.Status = "invalidated"
		default:
			b.Status = "cancelled"
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (s *Store) TranslationBatch(id string) (TranslationBatch, error) {
	var chapter string
	if err := s.DB.QueryRow("SELECT chapter_id FROM translation_batches WHERE id=?", id).Scan(&chapter); err == sql.ErrNoRows {
		return TranslationBatch{}, ErrNotFound
	} else if err != nil {
		return TranslationBatch{}, err
	}
	batches, err := s.translationBatches(chapter, id)
	if err != nil {
		return TranslationBatch{}, err
	}
	for _, b := range batches {
		if b.ID == id {
			return b, nil
		}
	}
	return TranslationBatch{}, ErrNotFound
}

// A batch is only a group of independent single-paragraph jobs. Creation is
// atomic, and active jobs for the same paragraph are reused rather than billed twice.
func (e *Engine) CreateTranslationBatch(chapter string, revision int) (TranslationBatch, error) {
	s := e.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.chapter(chapter)
	if err != nil {
		return TranslationBatch{}, err
	}
	if c.Revision != revision {
		return TranslationBatch{}, ErrConflict
	}
	var existing string
	err = s.DB.QueryRow(`SELECT b.id FROM translation_batches b JOIN translation_batch_jobs bj ON bj.batch_id=b.id JOIN jobs j ON j.id=bj.job_id WHERE b.chapter_id=? AND b.revision=? AND j.status IN ('queued','running') LIMIT 1`, chapter, revision).Scan(&existing)
	if err == nil {
		return s.TranslationBatch(existing)
	}
	if err != sql.ErrNoRows {
		return TranslationBatch{}, err
	}
	g, err := s.glossary(c.BookID)
	if err != nil {
		return TranslationBatch{}, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return TranslationBatch{}, err
	}
	defer tx.Rollback()
	batch := id()
	if _, err = tx.Exec("INSERT INTO translation_batches VALUES(?,?,?,?)", batch, chapter, revision, now()); err != nil {
		return TranslationBatch{}, err
	}
	for n, seg := range c.Segments {
		if seg.Manual || seg.Translation != "" {
			continue
		}
		job, lookup := activeTranslationJob(tx, chapter, seg.ID)
		if lookup == sql.ErrNoRows {
			job = id()
			payload, _ := json.Marshal(translationPayload(c, n, g, e.Config, false))
			if _, err = tx.Exec("INSERT INTO jobs VALUES(?,?,'translation','queued',?,0,1,'',?,0)", job, chapter, revision, now()); err != nil {
				return TranslationBatch{}, err
			}
			if _, err = tx.Exec("INSERT INTO job_items VALUES(?,?,0,'queued','',?)", id(), job, string(payload)); err != nil {
				return TranslationBatch{}, err
			}
		} else if lookup != nil {
			return TranslationBatch{}, lookup
		}
		if _, err = tx.Exec("INSERT INTO translation_batch_jobs VALUES(?,?)", batch, job); err != nil {
			return TranslationBatch{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return TranslationBatch{}, err
	}
	return s.TranslationBatch(batch)
}
func (e *Engine) CancelTranslationBatch(id string) error {
	s := e.Store
	s.mu.Lock()
	ids, err := func() ([]string, error) {
		tx, err := s.DB.Begin()
		if err != nil {
			return nil, err
		}
		defer tx.Rollback()
		var exists int
		if err = tx.QueryRow("SELECT count(*) FROM translation_batches WHERE id=?", id).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			return nil, ErrNotFound
		}
		rows, err := tx.Query("SELECT j.id FROM jobs j JOIN translation_batch_jobs bj ON bj.job_id=j.id WHERE bj.batch_id=? AND j.status IN ('queued','running')", id)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for rows.Next() {
			var job string
			if err = rows.Scan(&job); err != nil {
				rows.Close()
				return nil, err
			}
			ids = append(ids, job)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if _, err = tx.Exec("UPDATE jobs SET cancel=1,status=CASE WHEN status='queued' THEN 'cancelled' ELSE status END WHERE id IN (SELECT job_id FROM translation_batch_jobs WHERE batch_id=?) AND status IN ('queued','running')", id); err != nil {
			return nil, err
		}
		return ids, tx.Commit()
	}()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, id := range ids {
		if cancel := e.cancels[id]; cancel != nil {
			cancel()
		}
	}
	return nil
}
func (e *Engine) RetryTranslationBatch(id string) (TranslationBatch, error) {
	b, err := e.Store.TranslationBatch(id)
	if err != nil {
		return b, err
	}
	return e.CreateTranslationBatch(b.ChapterID, b.Revision)
}
