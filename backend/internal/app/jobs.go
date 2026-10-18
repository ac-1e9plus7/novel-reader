package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Engine struct {
	Store      *Store
	Config     Config
	Translator Translator
	Audio      *Audio
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.Mutex
	cancels    map[string]context.CancelFunc
}

func NewEngine(s *Store, c Config, t Translator, a *Audio) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{Store: s, Config: c, Translator: t, Audio: a, ctx: ctx, cancel: cancel, cancels: map[string]context.CancelFunc{}}
	for i := 0; i < 3; i++ {
		e.wg.Add(1)
		go e.worker()
	}
	return e
}
func (e *Engine) Close() { e.cancel(); e.wg.Wait() }
func (s *Store) Jobs(chapter string) ([]Job, error) {
	query := "SELECT id,chapter_id,kind,status,revision,done,total,message,created,cancel FROM jobs"
	args := []any{}
	if chapter != "" {
		query += " WHERE chapter_id=?"
		args = append(args, chapter)
	}
	query += " ORDER BY created DESC LIMIT 50"
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []Job{}
	for rows.Next() {
		var j Job
		if err = rows.Scan(&j.ID, &j.ChapterID, &j.Kind, &j.Status, &j.Revision, &j.Done, &j.Total, &j.Message, &j.Created, &j.Cancel); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
func (s *Store) Job(identity string) (Job, error) {
	var j Job
	err := s.DB.QueryRow("SELECT id,chapter_id,kind,status,revision,done,total,message,created,cancel FROM jobs WHERE id=?", identity).Scan(&j.ID, &j.ChapterID, &j.Kind, &j.Status, &j.Revision, &j.Done, &j.Total, &j.Message, &j.Created, &j.Cancel)
	if err == sql.ErrNoRows {
		return j, ErrNotFound
	}
	if err != nil {
		return j, err
	}
	j.Items = []JobItem{}
	rows, err := s.DB.Query("SELECT id,job_id,position,status,message,payload FROM job_items WHERE job_id=? ORDER BY position", identity)
	if err != nil {
		return j, err
	}
	defer rows.Close()
	for rows.Next() {
		var i JobItem
		if err = rows.Scan(&i.ID, &i.JobID, &i.Position, &i.Status, &i.Message, &i.Payload); err != nil {
			return j, err
		}
		j.Items = append(j.Items, i)
	}
	return j, rows.Err()
}
func (e *Engine) Create(chapter, kind string, revision int, selected []string, force bool) (Job, error) {
	s := e.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.chapter(chapter)
	if err != nil {
		return Job{}, err
	}
	if c.Revision != revision {
		return Job{}, ErrConflict
	}
	if kind != "translation" && kind != "audio" {
		return Job{}, fmt.Errorf("任务类型无效")
	}
	var active string
	err = s.DB.QueryRow("SELECT id FROM jobs WHERE chapter_id=? AND kind=? AND status IN ('queued','running') LIMIT 1", chapter, kind).Scan(&active)
	if err == nil {
		return s.Job(active)
	}
	if err != sql.ErrNoRows {
		return Job{}, err
	}
	wanted := map[string]bool{}
	for _, v := range selected {
		wanted[v] = true
	}
	available := map[string]bool{}
	for _, seg := range c.Segments {
		available[seg.ID] = true
	}
	for v := range wanted {
		if !available[v] {
			return Job{}, fmt.Errorf("段落不属于当前章节")
		}
	}
	g, err := s.glossary(c.BookID)
	if err != nil {
		return Job{}, err
	}
	payloads := []ItemPayload{}
	batch := ItemPayload{Terms: g.Terms, GlossaryRevision: g.Revision, ConfigKey: e.Config.translationKey(), Force: force}
	size := 0
	flush := func() {
		if len(batch.Targets) > 0 {
			payloads = append(payloads, batch)
			batch = ItemPayload{Terms: g.Terms, GlossaryRevision: g.Revision, ConfigKey: e.Config.translationKey(), Force: force}
			size = 0
		}
	}
	for n, seg := range c.Segments {
		if len(wanted) > 0 && !wanted[seg.ID] {
			continue
		}
		if kind == "translation" {
			if seg.Manual || (!force && seg.Translation != "") {
				continue
			}
			if size+len([]rune(seg.Text)) > e.Config.BatchChars {
				flush()
			}
			if len(batch.Targets) == 0 {
				start := n - 2
				if start < 0 {
					start = 0
				}
				for _, contextSeg := range c.Segments[start:n] {
					batch.Context += contextSeg.Text + "\n"
				}
				if n+1 < len(c.Segments) {
					batch.Context += "\n后续原文参考：" + c.Segments[n+1].Text
				}
			}
			batch.Targets = append(batch.Targets, Target{ID: seg.ID, Text: seg.Text, Version: seg.TranslationRevision})
			size += len([]rune(seg.Text))
		} else {
			for _, clip := range seg.Clips {
				if clip.AssetID != "" && !force {
					var assetPath, cacheKey string
					if err = s.DB.QueryRow("SELECT path,cache_key FROM audio_assets WHERE id=?", clip.AssetID).Scan(&assetPath, &cacheKey); err == nil && cacheKey == digest([]string{e.Config.speechKey(), clip.Text}) && e.Audio.assetExists(assetPath) {
						continue
					}
				}
				payloads = append(payloads, ItemPayload{Targets: []Target{{ID: clip.ID, Text: clip.Text, Version: seg.SpeechRevision}}, ConfigKey: e.Config.speechKey(), Force: force})
			}
		}
	}
	flush()
	j := Job{ID: id(), ChapterID: chapter, Kind: kind, Status: "queued", Revision: revision, Total: len(payloads), Created: now()}
	if j.Total == 0 {
		j.Status = "succeeded"
		j.Message = "所选内容已准备完成"
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return j, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO jobs(id,chapter_id,kind,status,revision,done,total,message,created) VALUES(?,?,?,?,?,0,?,?,?)", j.ID, chapter, kind, j.Status, revision, j.Total, j.Message, j.Created); err != nil {
		return j, err
	}
	for n, p := range payloads {
		raw, _ := json.Marshal(p)
		if _, err = tx.Exec("INSERT INTO job_items VALUES(?,?,?,'queued','',?)", id(), j.ID, n, string(raw)); err != nil {
			return j, err
		}
	}
	return j, tx.Commit()
}
func (e *Engine) Cancel(identity string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	res, err := e.Store.DB.Exec("UPDATE jobs SET cancel=1,status=CASE WHEN status='queued' THEN 'cancelled' ELSE status END WHERE id=? AND status IN ('queued','running')", identity)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("任务已结束或不存在")
	}
	if cancel := e.cancels[identity]; cancel != nil {
		cancel()
	}
	return nil
}
func (e *Engine) Retry(identity string) (Job, error) {
	s := e.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.Job(identity)
	if err != nil {
		return j, err
	}
	if j.Status == "running" || j.Status == "queued" || j.Status == "succeeded" {
		return j, fmt.Errorf("此任务无需重试")
	}
	var rev, active int
	if err = s.DB.QueryRow("SELECT revision FROM chapters WHERE id=?", j.ChapterID).Scan(&rev); err != nil {
		return j, err
	}
	if rev != j.Revision {
		return j, ErrConflict
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM jobs WHERE chapter_id=? AND kind=? AND status IN ('queued','running')", j.ChapterID, j.Kind).Scan(&active); err != nil {
		return j, err
	}
	if active > 0 {
		return j, fmt.Errorf("已有同类任务运行")
	}
	for _, item := range j.Items {
		if item.Status == "succeeded" {
			continue
		}
		var p ItemPayload
		if json.Unmarshal([]byte(item.Payload), &p) != nil {
			return j, fmt.Errorf("任务数据损坏")
		}
		key := e.Config.translationKey()
		if j.Kind == "audio" {
			key = e.Config.speechKey()
		}
		if key != p.ConfigKey {
			return j, fmt.Errorf("模型配置已变化，请创建新任务")
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return j, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE job_items SET status='queued',message='' WHERE job_id=? AND status!='succeeded'", identity); err != nil {
		return j, err
	}
	if _, err = tx.Exec("UPDATE jobs SET status='queued',cancel=0,message='' WHERE id=?", identity); err != nil {
		return j, err
	}
	if err = tx.Commit(); err != nil {
		return j, err
	}
	return s.Job(identity)
}
func (e *Engine) worker() {
	defer e.wg.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			identity, err := e.claim()
			if err == nil && identity != "" {
				e.run(identity)
			}
		}
	}
}
func (e *Engine) claim() (string, error) {
	s := e.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	var identity string
	err := s.DB.QueryRow("SELECT id FROM jobs WHERE status='queued' AND cancel=0 ORDER BY created LIMIT 1").Scan(&identity)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	_, err = s.DB.Exec("UPDATE jobs SET status='running' WHERE id=?", identity)
	return identity, err
}
func (e *Engine) run(identity string) {
	ctx, cancel := context.WithCancel(e.ctx)
	e.mu.Lock()
	e.cancels[identity] = cancel
	e.mu.Unlock()
	defer func() { cancel(); e.mu.Lock(); delete(e.cancels, identity); e.mu.Unlock() }()
	j, err := e.Store.Job(identity)
	if err != nil {
		return
	}
	if j.Cancel {
		cancel()
	}
	summary := ""
	for _, item := range j.Items {
		if ctx.Err() != nil {
			break
		}
		if item.Status == "succeeded" {
			var v string
			_ = e.Store.DB.QueryRow("SELECT summary FROM job_summaries WHERE job_id=? AND position=?", identity, item.Position).Scan(&v)
			if v != "" {
				summary = v
			}
			continue
		}
		_, _ = e.Store.DB.Exec("UPDATE job_items SET status='running' WHERE id=?", item.ID)
		var p ItemPayload
		err = json.Unmarshal([]byte(item.Payload), &p)
		start := time.Now()
		var usage json.RawMessage
		if err == nil {
			err = e.checkInput(j, p)
		}
		if err == nil {
			if j.Kind == "translation" {
				p.Context += "\n前文情节摘要：" + summary
				var result TranslateResult
				result, err = e.Translator.Translate(ctx, p)
				usage = result.Usage
				if err == nil {
					err = validateTranslation(p.Targets, result.Items)
				}
				if err == nil {
					err = e.commitTranslation(ctx, j, item, p, result)
					if err == nil {
						summary = result.Summary
					}
				}
			} else {
				var asset string
				asset, err = e.Audio.Generate(ctx, p.Targets[0].Text, p.Force)
				if err == nil {
					err = e.commitAudio(ctx, j, item, p, asset)
				}
			}
		}
		status, message := "succeeded", ""
		if err != nil {
			status = "failed"
			message = safeMessage(err)
		}
		usageText := any(nil)
		if len(usage) > 0 {
			usageText = string(usage)
		}
		_, _ = e.Store.DB.Exec("INSERT INTO generation_attempts VALUES(?,?,?,?,?,?)", id(), item.ID, now(), time.Since(start).Milliseconds(), usageText, status)
		if err != nil {
			_, _ = e.Store.DB.Exec("UPDATE job_items SET status=?,message=? WHERE id=?", status, message, item.ID)
		}
		_, _ = e.Store.DB.Exec("UPDATE jobs SET done=(SELECT count(*) FROM job_items WHERE job_id=? AND status='succeeded') WHERE id=?", identity, identity)
		if err != nil && j.Kind == "translation" {
			break
		}
	}
	var done, total int
	_ = e.Store.DB.QueryRow("SELECT sum(CASE WHEN status='succeeded' THEN 1 ELSE 0 END),count(*) FROM job_items WHERE job_id=?", identity).Scan(&done, &total)
	status, message := "succeeded", "处理完成"
	if done < total {
		status = "failed"
		message = "部分内容未完成，可重试"
		if done > 0 {
			status = "partial_failed"
		}
	}
	if ctx.Err() != nil {
		status = "cancelled"
		message = "已停止，已完成内容已保留"
		if e.ctx.Err() != nil {
			status = "interrupted"
			message = "服务停止，可继续未完成部分"
		}
	}
	_, _ = e.Store.DB.Exec("UPDATE jobs SET status=?,message=?,done=? WHERE id=?", status, message, done, identity)
}
func safeMessage(err error) string {
	if err == context.Canceled {
		return "任务已停止"
	}
	if err == context.DeadlineExceeded {
		return "模型调用超时，可以重试"
	}
	v := err.Error()
	if strings.Contains(v, "SQL") || strings.Contains(v, "constraint") {
		return "存储操作失败，请查看服务日志"
	}
	return v
}
func (e *Engine) checkInput(j Job, p ItemPayload) error {
	var rev, glossary int
	err := e.Store.DB.QueryRow("SELECT c.revision,b.glossary_revision FROM chapters c JOIN books b ON b.id=c.book_id WHERE c.id=?", j.ChapterID).Scan(&rev, &glossary)
	if err != nil {
		return err
	}
	if rev != j.Revision {
		return ErrConflict
	}
	if j.Kind == "translation" && glossary != p.GlossaryRevision {
		return fmt.Errorf("术语表已变化，请创建新的翻译任务")
	}
	for _, target := range p.Targets {
		var version int
		if j.Kind == "translation" {
			var manual bool
			err = e.Store.DB.QueryRow("SELECT coalesce(t.revision,0),coalesce(t.manual,0) FROM segments s LEFT JOIN translations t ON t.segment_id=s.id WHERE s.id=? AND s.chapter_id=?", target.ID, j.ChapterID).Scan(&version, &manual)
			if err != nil || manual || version != target.Version {
				return ErrConflict
			}
		} else {
			err = e.Store.DB.QueryRow("SELECT s.speech_revision FROM audio_clips a JOIN segments s ON s.id=a.segment_id WHERE a.id=? AND s.chapter_id=? AND a.text=?", target.ID, j.ChapterID, target.Text).Scan(&version)
			if err != nil || version != target.Version {
				return ErrConflict
			}
		}
	}
	return nil
}
func (e *Engine) commitTranslation(ctx context.Context, j Job, item JobItem, p ItemPayload, result TranslateResult) error {
	s := e.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.checkInput(j, p); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	byID := map[string]string{}
	for _, v := range result.Items {
		byID[v.SegmentID] = v.Translation
	}
	for _, target := range p.Targets {
		var rev int
		var manual bool
		err = tx.QueryRow("SELECT coalesce(t.revision,0),coalesce(t.manual,0) FROM segments s LEFT JOIN translations t ON t.segment_id=s.id WHERE s.id=? AND s.chapter_id=?", target.ID, j.ChapterID).Scan(&rev, &manual)
		if err != nil || rev != target.Version || manual {
			return ErrConflict
		}
		_, err = tx.Exec("INSERT INTO translation_history SELECT ?,?,s.id,s.text,t.text,t.manual,? FROM translations t JOIN segments s ON s.id=t.segment_id WHERE s.id=?", id(), j.ChapterID, now(), target.ID)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO translations VALUES(?,?,?,0,?,?) ON CONFLICT(segment_id) DO UPDATE SET text=excluded.text,revision=excluded.revision,manual=0,model=excluded.model,context_hash=excluded.context_hash", target.ID, byID[target.ID], rev+1, e.Config.Model, digest(p))
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT OR REPLACE INTO job_summaries VALUES(?,?,?)", j.ID, item.Position, string([]rune(result.Summary)[:min(500, len([]rune(result.Summary)))])); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE job_items SET status='succeeded',message='' WHERE id=?", item.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (e *Engine) commitAudio(ctx context.Context, j Job, item JobItem, p ItemPayload, asset string) error {
	s := e.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.checkInput(j, p); err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	target := p.Targets[0]
	res, err := tx.Exec("UPDATE audio_clips SET asset_id=? WHERE id=? AND text=? AND segment_id IN (SELECT id FROM segments WHERE chapter_id=? AND speech_revision=?)", asset, target.ID, target.Text, j.ChapterID, target.Version)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if _, err = tx.Exec("UPDATE job_items SET status='succeeded',message='' WHERE id=?", item.ID); err != nil {
		return err
	}
	return tx.Commit()
}
