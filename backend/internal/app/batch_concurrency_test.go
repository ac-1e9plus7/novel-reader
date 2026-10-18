package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitBatch(t *testing.T, s *Store, id string) TranslationBatch {
	t.Helper()
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		b, e := s.TranslationBatch(id)
		if e != nil {
			t.Fatal(e)
		}
		if b.Status != "queued" && b.Status != "running" {
			return b
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("batch timed out")
	return TranslationBatch{}
}
func TestChapterBatchCreatesIndependentJobsAndRetriesOnlyMissing(t *testing.T) {
	tr := &fakeTranslator{failAt: 1}
	s := newTestServer(t, tr, &fakeSynth{})
	_, c := seed(t, s.Store, "一。\n二。\n三。\n四。")
	s.Engine.Close()
	if e := s.Store.EditTranslation(c.Segments[0].ID, "人工译文", 0); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Store.DB.Exec("INSERT INTO translations VALUES(?, '已有模型译文',1,0,'test','')", c.Segments[1].ID); e != nil {
		t.Fatal(e)
	}
	b, e := s.Engine.CreateTranslationBatch(c.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if b.Total != 2 {
		t.Fatal("did not skip existing translations")
	}
	duplicate, e := s.Engine.CreateTranslationBatch(c.ID, 1)
	if e != nil || duplicate.ID != b.ID {
		t.Fatal("duplicate batch created", e)
	}
	jobs, _ := s.Store.Jobs(c.ID)
	if len(jobs) != 2 {
		t.Fatal("wrong child count")
	}
	for _, j := range jobs {
		detail, _ := s.Store.Job(j.ID)
		var p ItemPayload
		json.Unmarshal([]byte(detail.Items[0].Payload), &p)
		if j.Total != 1 || len(detail.Items) != 1 || len(p.Targets) != 1 {
			t.Fatal("batch merged paragraph jobs")
		}
	}
	reused, e := s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[2].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	if reused.ID != jobs[0].ID && reused.ID != jobs[1].ID {
		t.Fatal("single request duplicated batch child")
	}
	s.Engine = NewEngine(s.Store, s.Config, tr, s.Audio)
	b = waitBatch(t, s.Store, b.ID)
	if b.Status != "partial_failed" || b.Done != 1 || b.Failed != 1 {
		t.Fatal("bad aggregate result", b)
	}
	retry, e := s.Engine.RetryTranslationBatch(b.ID)
	if e != nil {
		t.Fatal(e)
	}
	if retry.Total != 1 || waitBatch(t, s.Store, retry.ID).Status != "succeeded" || tr.calls.Load() != 3 {
		t.Fatal("successful paragraphs were translated again")
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].Translation != "人工译文" || after.Segments[1].Translation != "已有模型译文" || after.Segments[3].TranslationRevision != 1 {
		t.Fatal("existing results overwritten")
	}
	for i := 0; i < 11; i++ {
		if _, e = s.Engine.CreateTranslationBatch(c.ID, 1); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.Store.TranslationBatch(b.ID); e != nil {
		t.Fatal("older batch became inaccessible", e)
	}
}
func TestConcurrentBatchCreationAndCancellation(t *testing.T) {
	tr := &fakeTranslator{gate: make(chan struct{}), started: make(chan struct{})}
	s := newTestServer(t, tr, &fakeSynth{})
	_, c := seed(t, s.Store, "一。\n二。\n三。")
	var wg sync.WaitGroup
	ids := make(chan string, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := s.Engine.CreateTranslationBatch(c.ID, 1)
			if e != nil {
				t.Error(e)
				return
			}
			ids <- b.ID
		}()
	}
	wg.Wait()
	close(ids)
	batch := ""
	for id := range ids {
		if batch == "" {
			batch = id
		} else if batch != id {
			t.Fatal("multiple active batches")
		}
	}
	select {
	case <-tr.started:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	if e := s.Engine.CancelTranslationBatch(batch); e != nil {
		t.Fatal(e)
	}
	if waitBatch(t, s.Store, batch).Status != "cancelled" || tr.calls.Load() != 1 {
		t.Fatal("cancelled batch dispatched more paragraphs")
	}
	jobs, _ := s.Store.Jobs(c.ID)
	if len(jobs) != 3 {
		t.Fatal("duplicate jobs")
	}
	if e := s.Engine.DeleteChapter(c.ID, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Store.TranslationBatch(batch); !errors.Is(e, ErrNotFound) {
		t.Fatal("deleted chapter batch remained")
	}
}

type gatedSynth struct {
	entered         chan string
	release         chan struct{}
	active, maximum atomic.Int32
}

func (g *gatedSynth) Synthesize(ctx context.Context, text string) ([]byte, error) {
	n := g.active.Add(1)
	defer g.active.Add(-1)
	for {
		old := g.maximum.Load()
		if n <= old || g.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	g.entered <- text
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-g.release:
		return makeWAV(make([]byte, 480), 24000, 1), nil
	}
}
func newGatedSynth() *gatedSynth {
	return &gatedSynth{entered: make(chan string, 100), release: make(chan struct{}, 100)}
}
func entered(t *testing.T, g *gatedSynth, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		select {
		case <-g.entered:
		case <-time.After(3 * time.Second):
			t.Fatal("expected concurrent synthesis did not start")
		}
	}
}
func noExtra(t *testing.T, g *gatedSynth) {
	t.Helper()
	select {
	case <-g.entered:
		t.Fatal("concurrency limit exceeded")
	case <-time.After(80 * time.Millisecond):
	}
}
func audioTestServer(t *testing.T, limit int, g *gatedSynth) *Server {
	t.Helper()
	c := testConfig(t)
	c.TTSConcurrency = limit
	s, e := NewServer(c, &fakeTranslator{}, g)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}
func TestAudioUsesTenConcurrentRequestsAndResizesLive(t *testing.T) {
	g := newGatedSynth()
	s := audioTestServer(t, 10, g)
	lines := []string{}
	for i := 0; i < 12; i++ {
		lines = append(lines, fmt.Sprintf("第%d文。", i))
	}
	_, c := seed(t, s.Store, strings.Join(lines, "\n"))
	j, e := s.Engine.Create(c.ID, "audio", 1, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	entered(t, g, 10)
	noExtra(t, g)
	if g.maximum.Load() != 10 {
		t.Fatal("default concurrency was not used")
	}
	if e = s.Audio.SetConcurrency(2); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 8; i++ {
		g.release <- struct{}{}
	}
	until := time.Now().Add(2 * time.Second)
	for g.active.Load() != 2 && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if g.active.Load() != 2 {
		t.Fatal("active calls did not finish")
	}
	noExtra(t, g)
	g.release <- struct{}{}
	entered(t, g, 1)
	if e = s.Audio.SetConcurrency(4); e != nil {
		t.Fatal(e)
	}
	entered(t, g, 1)
	for i := 0; i < 3; i++ {
		g.release <- struct{}{}
	}
	if waitJob(t, s.Store, j.ID).Status != "succeeded" {
		t.Fatal("concurrent audio job failed")
	}
}
func TestAudioConcurrencyIsGlobalAndCancelledWaitersExit(t *testing.T) {
	g := newGatedSynth()
	s := audioTestServer(t, 3, g)
	_, a := seed(t, s.Store, "一。\n二。\n三。\n四。")
	_, b := seed(t, s.Store, "五。\n六。\n七。\n八。")
	j1, e := s.Engine.Create(a.ID, "audio", 1, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	j2, e := s.Engine.Create(b.ID, "audio", 1, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	entered(t, g, 3)
	noExtra(t, g)
	if e = s.Engine.Cancel(j1.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Engine.Cancel(j2.ID); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{j1.ID, j2.ID} {
		if waitJob(t, s.Store, id).Status != "cancelled" {
			t.Fatal("cancel did not release blocked synthesis")
		}
	}
	if g.active.Load() != 0 || g.maximum.Load() > 3 {
		t.Fatal("global limiter leak")
	}
}
func TestAudioConcurrencyValidationPersistenceAndCache(t *testing.T) {
	c := testConfig(t)
	c.TTSConcurrency = 0
	s, e := NewServer(c, &fakeTranslator{}, &fakeSynth{})
	if e != nil {
		t.Fatal(e)
	}
	if s.Audio.limiter.value() != 10 {
		t.Fatal("fallback must be ten")
	}
	asset, e := s.Audio.Generate(context.Background(), "缓存内容", false)
	if e != nil {
		t.Fatal(e)
	}
	host := httptest.NewServer(s.Handler)
	for _, body := range []string{`{"concurrency":0}`, `{"concurrency":33}`, `{"concurrency":1.5}`} {
		req, _ := http.NewRequest("PATCH", host.URL+"/api/settings/audio", strings.NewReader(body))
		req.Header.Set("X-Reader-Request", "1")
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 400 {
			t.Fatal("invalid concurrency accepted")
		}
	}
	host.Close()
	if e = s.Audio.SetConcurrency(3); e != nil {
		t.Fatal(e)
	}
	s.Close()
	synth := &fakeSynth{}
	next, e := NewServer(c, &fakeTranslator{}, synth)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	if next.Audio.limiter.value() != 3 {
		t.Fatal("saved concurrency lost")
	}
	reused, e := next.Audio.Generate(context.Background(), "缓存内容", false)
	if e != nil || reused != asset || synth.calls.Load() != 0 {
		t.Fatal("concurrency change invalidated cache", e)
	}
}

func TestV3MigrationPreservesV2TasksAndAddsSettings(t *testing.T) {
	root := t.TempDir()
	s, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	_, c := seed(t, s, "原文。")
	job := id()
	if _, e = s.DB.Exec("INSERT INTO jobs VALUES(?,?,'translation','queued',1,0,1,'',?,0)", job, c.ID, now()); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("DELETE FROM schema_migrations WHERE version=3; DROP TABLE translation_batch_jobs; DROP TABLE translation_batches; DROP TABLE app_settings"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	upgraded, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	defer upgraded.Close()
	j, e := upgraded.Job(job)
	if e != nil || j.Status != "interrupted" {
		t.Fatal("v3 wrongly invalidated a v2 single-paragraph task", e)
	}
	value, e := upgraded.AudioConcurrency(10)
	if e != nil || value != 10 || upgraded.MigrationBackup == "" {
		t.Fatal("settings or migration backup missing", e)
	}
}
