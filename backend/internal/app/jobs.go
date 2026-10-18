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
	for _, kind := range []string{"translation", "audio"} {
		count := 3
		if kind == "audio" {
			count = 2
		}
		for i := 0; i < count; i++ {
			e.wg.Add(1)
			go e.worker(kind)
		}
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
	if kind == "translation" && len(selected) != 1 {
		return Job{}, fmt.Errorf("每次翻译必须且只能选择一个段落")
	}
	var active string
	if kind == "translation" {
		active, err = activeTranslationJob(s.DB, chapter, selected[0])
	} else {
		err = s.DB.QueryRow("SELECT id FROM jobs WHERE chapter_id=? AND kind=? AND status IN ('queued','running') LIMIT 1", chapter, kind).Scan(&active)
	}
	if err == nil {
		job, loadErr := s.Job(active)
		return job, loadErr
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
	for n, seg := range c.Segments {
		if len(wanted) > 0 && !wanted[seg.ID] {
			continue
		}
		if kind == "translation" {
			if seg.Manual || (!force && seg.Translation != "") {
				continue
			}
			part := translationPayload(c, n, g, e.Config, force)
			payloads = append(payloads, part)
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
	if j.Status == "invalidated" {
		return j, fmt.Errorf("此翻译任务已作废，请在段落旁创建新任务")
	}
	if j.Kind == "translation" && len(j.Items) != 1 {
		return j, fmt.Errorf("旧翻译任务不支持重试，请逐段创建新任务")
	}
	var rev, active int
	if err = s.DB.QueryRow("SELECT revision FROM chapters WHERE id=?", j.ChapterID).Scan(&rev); err != nil {
		return j, err
	}
	if rev != j.Revision {
		return j, ErrConflict
	}
	if j.Kind == "translation" {
		var p ItemPayload
		if json.Unmarshal([]byte(j.Items[0].Payload), &p) != nil || len(p.Targets) != 1 {
			return j, fmt.Errorf("任务数据无效")
		}
		_, err = activeTranslationJob(s.DB, j.ChapterID, p.Targets[0].ID)
		if err == nil {
			active = 1
		} else if err != sql.ErrNoRows {
			return j, err
		}
	} else if err = s.DB.QueryRow("SELECT count(*) FROM jobs WHERE chapter_id=? AND kind=? AND status IN ('queued','running')", j.ChapterID, j.Kind).Scan(&active); err != nil {
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
		if j.Kind == "translation" && len(p.Targets) != 1 {
			return j, fmt.Errorf("旧翻译任务不支持重试，请逐段创建新任务")
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
func (e *Engine) worker(kind string) {
	defer e.wg.Done()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			identity, err := e.claim(kind)
			if err == nil && identity != "" {
				e.run(identity)
			}
		}
	}
}
func (e *Engine) claim(kind string) (string, error) {
	s := e.Store
	s.mu.Lock()
	defer s.mu.Unlock()
	var identity string
	err := s.DB.QueryRow(`SELECT j.id FROM jobs j WHERE j.status='queued' AND j.cancel=0 AND j.kind=? AND (j.kind!='translation' OR NOT EXISTS(SELECT 1 FROM jobs running WHERE running.chapter_id=j.chapter_id AND running.kind='translation' AND running.status='running')) ORDER BY j.rowid LIMIT 1`, kind).Scan(&identity)
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
	if j.Kind == "audio" {
		e.runAudioItems(ctx, j)
	} else {
		for _, item := range j.Items {
			if ctx.Err() != nil {
				break
			}
			if item.Status == "succeeded" {
				continue
			}
			if e.runItem(ctx, j, item) != nil {
				break
			}
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
func (e *Engine) runAudioItems(ctx context.Context, j Job) {
	items := make(chan JobItem)
	var workers sync.WaitGroup
	for i := 0; i < min(32, len(j.Items)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range items {
				if ctx.Err() != nil {
					return
				}
				_ = e.runItem(ctx, j, item)
			}
		}()
	}
dispatch:
	for _, item := range j.Items {
		if item.Status == "succeeded" {
			continue
		}
		select {
		case <-ctx.Done():
			break dispatch
		case items <- item:
		}
	}
	close(items)
	workers.Wait()
}
func (e *Engine) runItem(ctx context.Context, j Job, item JobItem) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, _ = e.Store.DB.Exec("UPDATE job_items SET status='running' WHERE id=?", item.ID)
	var p ItemPayload
	err := json.Unmarshal([]byte(item.Payload), &p)
	started := time.Now()
	var usage json.RawMessage
	if err == nil {
		err = e.checkInput(j, p)
	}
	if err == nil {
		if j.Kind == "translation" {
			var result TranslateResult
			result, err = e.Translator.Translate(ctx, p)
			usage = result.Usage
			if err == nil {
				err = validateTranslation(p.Targets, result.Items)
			}
			if err == nil {
				err = e.commitTranslation(ctx, j, item, p, result)
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
	var usageText any
	if len(usage) > 0 {
		usageText = string(usage)
	}
	_, _ = e.Store.DB.Exec("INSERT INTO generation_attempts VALUES(?,?,?,?,?,?)", id(), item.ID, now(), time.Since(started).Milliseconds(), usageText, status)
	if err != nil {
		_, _ = e.Store.DB.Exec("UPDATE job_items SET status=?,message=? WHERE id=?", status, message, item.ID)
	}
	_, _ = e.Store.DB.Exec("UPDATE jobs SET done=(SELECT count(*) FROM job_items WHERE job_id=? AND status='succeeded') WHERE id=?", j.ID, j.ID)
	return err
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
	if j.Kind == "translation" && (j.Total != 1 || len(p.Targets) != 1) {
		return fmt.Errorf("每次翻译必须且只能处理一个段落")
	}
	var rev, glossary int
	var cancelled bool
	err := e.Store.DB.QueryRow("SELECT c.revision,b.glossary_revision,j.cancel FROM chapters c JOIN books b ON b.id=c.book_id JOIN jobs j ON j.chapter_id=c.id WHERE c.id=? AND j.id=?", j.ChapterID, j.ID).Scan(&rev, &glossary, &cancelled)
	if err != nil {
		return err
	}
	if cancelled {
		return context.Canceled
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
