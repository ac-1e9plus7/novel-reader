package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestReorderChaptersPreservesContentAndRejectsStaleOrder(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	b, c1 := seed(t, s.Store, "一。")
	id2, e := s.Store.CreateChapter(b.ID, "第二章", "二。")
	if e != nil {
		t.Fatal(e)
	}
	id3, e := s.Store.CreateChapter(b.ID, "第三章", "三。")
	if e != nil {
		t.Fatal(e)
	}
	old := []string{c1.ID, id2, id3}
	ordered := []string{id3, c1.ID, id2}
	if e = s.Store.ReorderChapters(b.ID, old, ordered); e != nil {
		t.Fatal(e)
	}
	list, e := s.Store.Chapters(b.ID)
	if e != nil {
		t.Fatal(e)
	}
	for i, c := range list {
		if c.ID != ordered[i] || c.Position != i || c.Revision != 1 {
			t.Fatal("order or source revision changed incorrectly")
		}
	}
	after, _ := s.Store.Chapter(c1.ID)
	if after.Raw != c1.Raw || after.Segments[0].ID != c1.Segments[0].ID {
		t.Fatal("reorder recreated content")
	}
	if e = s.Store.ReorderChapters(b.ID, old, old); !errors.Is(e, ErrConflict) {
		t.Fatal("stale reorder accepted", e)
	}
	_, other := seed(t, s.Store, "別の本。")
	for _, invalid := range [][]string{{id3, c1.ID}, {id3, c1.ID, c1.ID}, {id3, c1.ID, other.ID}} {
		if e = s.Store.ReorderChapters(b.ID, ordered, invalid); e == nil {
			t.Fatal("invalid order accepted")
		}
	}
}

func TestDeleteChapterCascadesAndKeepsSharedAudio(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	b, c := seed(t, s.Store, "同じ音声。")
	otherID, e := s.Store.CreateChapter(b.ID, "保留章节", c.Raw)
	if e != nil {
		t.Fatal(e)
	}
	for _, cid := range []string{c.ID, otherID} {
		j, e := s.Engine.Create(cid, "audio", 1, nil, false)
		if e != nil {
			t.Fatal(e)
		}
		if waitJob(t, s.Store, j.ID).Status != "succeeded" {
			t.Fatal("audio failed")
		}
	}
	c, _ = s.Store.Chapter(c.ID)
	other, _ := s.Store.Chapter(otherID)
	asset := c.Segments[0].Clips[0].AssetID
	if asset != other.Segments[0].Clips[0].AssetID {
		t.Fatal("fixture did not share audio")
	}
	if e = s.Store.EditTranslation(c.Segments[0].ID, "旧译文", 0); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.EditTranslation(c.Segments[0].ID, "新译文", 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.SaveProgress(b.ID, Progress{ChapterID: c.ID, SegmentID: c.Segments[0].ID}); e != nil {
		t.Fatal(e)
	}
	if e = s.Engine.DeleteChapter(c.ID, 99); !errors.Is(e, ErrConflict) {
		t.Fatal("stale deletion accepted", e)
	}
	if e = s.Engine.DeleteChapter(c.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Store.Chapter(c.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted chapter still exists")
	}
	for _, table := range []string{"segments", "jobs", "chapter_revisions", "translation_history"} {
		var n int
		if e = s.Store.DB.QueryRow("SELECT count(*) FROM "+table+" WHERE chapter_id=?", c.ID).Scan(&n); e != nil || n != 0 {
			t.Fatal("dependent data remains", table, e)
		}
	}
	p, _ := s.Store.Progress(b.ID)
	if p.ChapterID != "" {
		t.Fatal("deleted chapter progress remained")
	}
	list, _ := s.Store.Chapters(b.ID)
	if len(list) != 1 || list[0].ID != otherID || list[0].Position != 0 {
		t.Fatal("remaining chapter order wrong")
	}
	if _, _, e = s.Audio.Read(asset, true); e != nil {
		t.Fatal("shared audio was removed", e)
	}
	newID, e := s.Store.CreateChapter(b.ID, "新章节", "三。")
	if e != nil {
		t.Fatal(e)
	}
	newChapter, _ := s.Store.Chapter(newID)
	if newChapter.Position != 1 {
		t.Fatal("new chapter position not compact")
	}
}

func TestDeleteCancelsInFlightTranslation(t *testing.T) {
	tr := &fakeTranslator{gate: make(chan struct{}), started: make(chan struct{})}
	s := newTestServer(t, tr, &fakeSynth{})
	_, c := seed(t, s.Store, "消える章。")
	j, e := s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[0].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-tr.started:
	case <-time.After(3 * time.Second):
		t.Fatal("translation did not start")
	}
	if e = s.Engine.DeleteChapter(c.ID, 1); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.Engine.mu.Lock()
		n := len(s.Engine.cancels)
		s.Engine.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Engine.mu.Lock()
	n := len(s.Engine.cancels)
	s.Engine.mu.Unlock()
	if n != 0 {
		t.Fatal("request was not cancelled")
	}
	if _, e = s.Store.Job(j.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted task resurrected")
	}
}

type recordingTranslator struct{ inputs chan ItemPayload }

func (t recordingTranslator) Translate(ctx context.Context, p ItemPayload) (TranslateResult, error) {
	t.inputs <- p
	return TranslateResult{Items: []Translation{{SegmentID: p.Targets[0].ID, Translation: "本段译文"}}}, nil
}
func TestTranslationJobOnlyTargetsExplicitParagraph(t *testing.T) {
	tr := recordingTranslator{inputs: make(chan ItemPayload, 1)}
	s := newTestServer(t, tr, &fakeSynth{})
	_, c := seed(t, s.Store, "一。\n二。\n三。")
	for _, selection := range [][]string{nil, {}, {c.Segments[0].ID, c.Segments[1].ID}, {c.Segments[0].ID, c.Segments[0].ID}, {"unknown"}} {
		if _, e := s.Engine.Create(c.ID, "translation", 1, selection, false); e == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	j, e := s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[1].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	if j.Total != 1 || waitJob(t, s.Store, j.ID).Status != "succeeded" {
		t.Fatal("not a single-paragraph job")
	}
	p := <-tr.inputs
	if len(p.Targets) != 1 || p.Targets[0].ID != c.Segments[1].ID || p.Targets[0].Text != c.Segments[1].Text {
		t.Fatal("multiple paragraphs submitted")
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].Translation != "" || after.Segments[2].Translation != "" || after.Segments[1].Translation == "" {
		t.Fatal("wrong translation targets")
	}
}

func TestV2MigrationInvalidatesAllUnfinishedLegacyTranslationsOnce(t *testing.T) {
	root := t.TempDir()
	s, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	_, c := seed(t, s, "原文。")
	if e = s.EditTranslation(c.Segments[0].ID, "已保存的译文", 0); e != nil {
		t.Fatal(e)
	}
	statuses := []string{"queued", "running", "failed", "partial_failed", "cancelled", "interrupted", "succeeded"}
	ids := []string{}
	for _, status := range statuses {
		id := id()
		ids = append(ids, id)
		if _, e = s.DB.Exec("INSERT INTO jobs VALUES(?,?,?, ?,1,1,2,'',?,0)", id, c.ID, "translation", status, now()); e != nil {
			t.Fatal(e)
		}
		for n, state := range []string{"succeeded", "failed"} {
			if status == "succeeded" {
				state = "succeeded"
			}
			if _, e = s.DB.Exec("INSERT INTO job_items VALUES(?,?,?,?,'','{}')", id+string(rune('a'+n)), id, n, state); e != nil {
				t.Fatal(e)
			}
		}
	}
	audioID := id()
	if _, e = s.DB.Exec("INSERT INTO jobs VALUES(?,?,'audio','failed',1,0,1,'',?,0)", audioID, c.ID, now()); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("DELETE FROM schema_migrations WHERE version>=2; DROP TABLE translation_batch_jobs; DROP TABLE translation_batches; DROP TABLE app_settings"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	migrated, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	if migrated.MigrationBackup == "" {
		t.Fatal("backup missing")
	}
	if _, e = os.Stat(migrated.MigrationBackup); e != nil {
		t.Fatal(e)
	}
	backup, e := sql.Open("sqlite", migrated.MigrationBackup)
	if e != nil {
		t.Fatal(e)
	}
	var version int
	e = backup.QueryRow("SELECT max(version) FROM schema_migrations").Scan(&version)
	backup.Close()
	if e != nil || version != 1 {
		t.Fatal("backup not pre-migration", e)
	}
	for n, id := range ids {
		job, e := migrated.Job(id)
		if e != nil {
			t.Fatal(e)
		}
		if statuses[n] == "succeeded" {
			if job.Status != "succeeded" {
				t.Fatal("completed job invalidated")
			}
		} else {
			if job.Status != "invalidated" || !job.Cancel || job.Items[0].Status != "succeeded" || job.Items[1].Status != "invalidated" {
				t.Fatal("unfinished legacy task was not invalidated", job)
			}
		}
	}
	after, _ := migrated.Chapter(c.ID)
	if after.Segments[0].Translation != "已保存的译文" {
		t.Fatal("translation removed")
	}
	audioJob, _ := migrated.Job(audioID)
	if audioJob.Status != "failed" {
		t.Fatal("audio task invalidated")
	}
	cfg := testConfig(t)
	engine := NewEngine(migrated, cfg, &fakeTranslator{}, nil)
	if _, e = engine.Retry(ids[0]); e == nil {
		t.Fatal("invalidated task can retry")
	}
	engine.Close()
	freshID := id()
	payload, _ := json.Marshal(ItemPayload{Targets: []Target{{ID: c.Segments[0].ID}}})
	if _, e = migrated.DB.Exec("INSERT INTO jobs VALUES(?,?,'translation','queued',1,0,1,'',?,0)", freshID, c.ID, now()); e != nil {
		t.Fatal(e)
	}
	if _, e = migrated.DB.Exec("INSERT INTO job_items VALUES(?,?,0,'queued','',?)", id(), freshID, string(payload)); e != nil {
		t.Fatal(e)
	}
	migrated.Close()
	again, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	fresh, _ := again.Job(freshID)
	if fresh.Status != "interrupted" || again.MigrationBackup != "" {
		t.Fatal("migration repeated")
	}
	backups, _ := filepath.Glob(filepath.Join(root, "backups", "*.sqlite3"))
	if len(backups) != 1 {
		t.Fatal("unexpected backup count")
	}
	list, _ := again.Chapters(c.BookID)
	if !slices.Equal([]string{list[0].ID}, []string{c.ID}) {
		t.Fatal("chapter changed")
	}
}
