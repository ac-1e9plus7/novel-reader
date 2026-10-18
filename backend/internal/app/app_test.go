package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{Addr: "127.0.0.1:8787", DataDir: t.TempDir(), FrontendDir: t.TempDir(), ResponsesURL: "http://invalid", ResponsesKey: "test-secret", Model: "test-model", Structured: true, ResponseTimeout: 5 * time.Second, TTSProvider: "openai", TTSURL: "http://invalid", TTSKey: "test-secret", TTSModel: "test-tts", Voice: "test", TTSTimeout: 5 * time.Second, TTSConcurrency: 2, FFmpeg: "missing-ffmpeg-for-tests"}
}

type fakeTranslator struct {
	calls   atomic.Int32
	failAt  int32
	gate    chan struct{}
	started chan struct{}
	once    sync.Once
}

func (f *fakeTranslator) Translate(ctx context.Context, p ItemPayload) (TranslateResult, error) {
	n := f.calls.Add(1)
	if f.started != nil {
		f.once.Do(func() { close(f.started) })
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return TranslateResult{}, ctx.Err()
		}
	}
	if n == f.failAt {
		return TranslateResult{}, fmt.Errorf("模拟模型失败")
	}
	r := TranslateResult{Summary: "已确认前文摘要", Usage: json.RawMessage(`{"input_tokens":12}`)}
	for _, t := range p.Targets {
		r.Items = append(r.Items, Translation{SegmentID: t.ID, Translation: "中文译文：" + t.Text})
	}
	return r, nil
}

type fakeSynth struct{ calls atomic.Int32 }

func (f *fakeSynth) Synthesize(ctx context.Context, text string) ([]byte, error) {
	f.calls.Add(1)
	return makeWAV(make([]byte, 4800), 24000, 1), nil
}
func newTestServer(t *testing.T, tr Translator, synth Synthesizer) *Server {
	t.Helper()
	s, e := NewServer(testConfig(t), tr, synth)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}
func seed(t *testing.T, s *Store, raw string) (Book, Chapter) {
	t.Helper()
	b, e := s.CreateBook("测试小说", "作者")
	if e != nil {
		t.Fatal(e)
	}
	identity, e := s.CreateChapter(b.ID, "第一章", raw)
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Chapter(identity)
	if e != nil {
		t.Fatal(e)
	}
	return b, c
}
func waitJob(t *testing.T, s *Store, identity string) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, e := s.Job(identity)
		if e != nil {
			t.Fatal(e)
		}
		if j.Status != "queued" && j.Status != "running" {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return Job{}
}
func TestSourcePreservesUnicodeAndOffsets(t *testing.T) {
	raw := "\r\n　「こんばんは。」\r\n\n" + strings.Repeat("雨が降る。", 400) + "🌙\n"
	segs := segments(raw)
	runes := []rune(raw)
	for _, s := range segs {
		if string(runes[s.Start:s.End]) != s.Text {
			t.Fatal("offset mismatch")
		}
		if strings.Join(Split(s.Text, 280), "") != s.Text {
			t.Fatal("audio split lost source")
		}
	}
	if len(segs) < 3 {
		t.Fatal("long paragraph not split")
	}
	if segs[1].Paragraph != segs[len(segs)-1].Paragraph {
		t.Fatal("paragraph grouping lost")
	}
}
func TestTranslationCoverageValidation(t *testing.T) {
	targets := []Target{{ID: "a"}, {ID: "b"}}
	for _, items := range [][]Translation{{{SegmentID: "a", Translation: "一"}}, {{SegmentID: "a", Translation: "一"}, {SegmentID: "a", Translation: "二"}}, {{SegmentID: "a", Translation: "一"}, {SegmentID: "x", Translation: "二"}}, {{SegmentID: "a", Translation: "一"}, {SegmentID: "b", Translation: " "}}} {
		if validateTranslation(targets, items) == nil {
			t.Fatal("invalid coverage accepted")
		}
	}
	if e := validateTranslation(targets, []Translation{{SegmentID: "b", Translation: "二"}, {SegmentID: "a", Translation: "一"}}); e != nil {
		t.Fatal(e)
	}
}
func TestSingleParagraphRetryPreservesOtherTranslations(t *testing.T) {
	tr := &fakeTranslator{failAt: 2}
	s := newTestServer(t, tr, &fakeSynth{})
	_, c := seed(t, s.Store, strings.Repeat("あ", 800)+"\n"+strings.Repeat("い", 800)+"\n"+strings.Repeat("う", 800))
	first, e := s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[0].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	if waitJob(t, s.Store, first.ID).Status != "succeeded" {
		t.Fatal("first paragraph failed")
	}
	j, e := s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[1].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	j = waitJob(t, s.Store, j.ID)
	if j.Status != "failed" || j.Done != 0 || j.Total != 1 {
		t.Fatalf("unexpected job: %+v", j)
	}
	before, _ := s.Store.Chapter(c.ID)
	if before.Segments[0].Translation == "" || before.Segments[1].Translation != "" {
		t.Fatal("partial results lost")
	}
	if _, e = s.Engine.Retry(j.ID); e != nil {
		t.Fatal(e)
	}
	j = waitJob(t, s.Store, j.ID)
	if j.Status != "succeeded" || tr.calls.Load() != 3 {
		t.Fatalf("retry redid successful batches: %+v calls=%d", j, tr.calls.Load())
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].TranslationRevision != 1 {
		t.Fatal("successful translation overwritten")
	}
	if after.Segments[2].Translation != "" {
		t.Fatal("unselected paragraph translated")
	}
}
func TestManualEditWinsAgainstInFlightModel(t *testing.T) {
	tr := &fakeTranslator{gate: make(chan struct{}), started: make(chan struct{})}
	s := newTestServer(t, tr, &fakeSynth{})
	_, c := seed(t, s.Store, "「こんにちは。」")
	j, e := s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[0].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	select {
	case <-tr.started:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	if e = s.Store.EditTranslation(c.Segments[0].ID, "人工校对译文", 0); e != nil {
		t.Fatal(e)
	}
	close(tr.gate)
	j = waitJob(t, s.Store, j.ID)
	if j.Status != "failed" {
		t.Fatal("stale result accepted")
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].Translation != "人工校对译文" || !after.Segments[0].Manual {
		t.Fatal("manual text overwritten")
	}
	j, e = s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[0].ID}, true)
	if e != nil || j.Total != 0 {
		t.Fatal("force should preserve manual text")
	}
}
func TestSourceEditRejectsStaleTaskAndPreservesHistory(t *testing.T) {
	tr := &fakeTranslator{gate: make(chan struct{}), started: make(chan struct{})}
	s := newTestServer(t, tr, &fakeSynth{})
	_, c := seed(t, s.Store, "旧原文。")
	if e := s.Store.EditTranslation(c.Segments[0].ID, "历史译文", 0); e != nil {
		t.Fatal(e)
	}
	if e := s.Store.EditSource(c.ID, c.Title, "新原文。", 1); e != nil {
		t.Fatal(e)
	}
	if e := s.Store.EditSource(c.ID, c.Title, "非法覆盖", 1); !errors.Is(e, ErrConflict) {
		t.Fatal("revision conflict missing")
	}
	updated, _ := s.Store.Chapter(c.ID)
	j, e := s.Engine.Create(c.ID, "translation", 2, []string{updated.Segments[0].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	<-tr.started
	if e = s.Store.EditSource(c.ID, c.Title, "再次修改。", 2); e != nil {
		t.Fatal(e)
	}
	close(tr.gate)
	j = waitJob(t, s.Store, j.ID)
	if j.Status != "failed" {
		t.Fatal("old version wrote results")
	}
	var count int
	s.Store.DB.QueryRow("SELECT count(*) FROM translation_history").Scan(&count)
	if count != 1 {
		t.Fatal("translation history missing")
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].Translation != "" {
		t.Fatal("stale translation attached")
	}
}
func TestIdenticalSourceDoesNotInvalidate(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	_, c := seed(t, s.Store, "同じ文章。")
	_ = s.Store.EditTranslation(c.Segments[0].ID, "相同文字", 0)
	if e := s.Store.EditSource(c.ID, "新标题", c.Raw, 1); e != nil {
		t.Fatal(e)
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Revision != 1 || after.Segments[0].ID != c.Segments[0].ID || after.Segments[0].Translation == "" {
		t.Fatal("identical save destroyed results")
	}
}
func TestGlossaryRevisionAndProgressOwnership(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	b, c := seed(t, s.Store, "原文。")
	g := Glossary{Revision: 1, Terms: []Term{{Source: "夏目", Target: "夏目"}}}
	if e := s.Store.SaveGlossary(b.ID, g); e != nil {
		t.Fatal(e)
	}
	if e := s.Store.SaveGlossary(b.ID, g); !errors.Is(e, ErrConflict) {
		t.Fatal("glossary conflict missing")
	}
	p := Progress{ChapterID: c.ID, SegmentID: c.Segments[0].ID, ClipID: c.Segments[0].Clips[0].ID, Seconds: .1}
	saved, e := s.Store.SaveProgress(b.ID, p)
	if e != nil || saved.Revision != 1 {
		t.Fatal(e)
	}
	if _, e = s.Store.SaveProgress(b.ID, p); !errors.Is(e, ErrConflict) {
		t.Fatal("progress conflict missing")
	}
	other, _ := s.Store.CreateBook("其他", " ")
	if _, e = s.Store.SaveProgress(other.ID, p); e == nil {
		t.Fatal("cross-book progress accepted")
	}
}
func TestAudioDedupAndSpeechInvalidation(t *testing.T) {
	synth := &fakeSynth{}
	s := newTestServer(t, &fakeTranslator{}, synth)
	_, c := seed(t, s.Store, "同じ文章。\n同じ文章。")
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			asset, e := s.Audio.Generate(context.Background(), "同じ文章。", false)
			if e != nil {
				t.Error(e)
			}
			ids <- asset
		}()
	}
	wg.Wait()
	close(ids)
	first := ""
	for asset := range ids {
		if first == "" {
			first = asset
		}
		if first != asset {
			t.Fatal("duplicate asset")
		}
	}
	if synth.calls.Load() != 1 {
		t.Fatal("duplicate paid calls")
	}
	j, e := s.Engine.Create(c.ID, "audio", 1, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	j = waitJob(t, s.Store, j.ID)
	if j.Status != "succeeded" {
		t.Fatal(j)
	}
	before, _ := s.Store.Chapter(c.ID)
	oldAsset := before.Segments[0].Clips[0].AssetID
	if oldAsset == "" {
		t.Fatal("no audio attached")
	}
	if e = s.Store.EditSpeech(before.Segments[0].ID, "おなじぶんしょう。", 1); e != nil {
		t.Fatal(e)
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].Text != before.Segments[0].Text || after.Segments[0].Clips[0].AssetID != "" || after.Segments[1].Clips[0].AssetID == "" {
		t.Fatal("speech edit invalidated wrong data")
	}
	if _, _, e = s.Audio.Read(oldAsset, true); e != nil {
		t.Fatal("shared asset deleted")
	}
}
func TestCancelAndRestartRecovery(t *testing.T) {
	cfg := testConfig(t)
	tr := &fakeTranslator{gate: make(chan struct{}), started: make(chan struct{})}
	s, e := NewServer(cfg, tr, &fakeSynth{})
	if e != nil {
		t.Fatal(e)
	}
	_, c := seed(t, s.Store, "テスト。")
	j, e := s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[0].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	<-tr.started
	if e = s.Engine.Cancel(j.ID); e != nil {
		t.Fatal(e)
	}
	j = waitJob(t, s.Store, j.ID)
	if j.Status != "cancelled" {
		t.Fatal(j.Status)
	}
	_, e = s.Store.DB.Exec("UPDATE jobs SET status='running',cancel=0 WHERE id=?", j.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = OpenStore(cfg.DataDir); e == nil {
		t.Fatal("two service instances allowed")
	}
	s.Close()
	next, e := NewServer(cfg, &fakeTranslator{}, &fakeSynth{})
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	restored, _ := next.Store.Job(j.ID)
	if restored.Status != "interrupted" {
		t.Fatal("unfinished job not recovered")
	}
	time.Sleep(400 * time.Millisecond)
	restored, _ = next.Store.Job(j.ID)
	if restored.Status != "interrupted" {
		t.Fatal("restart automatically charged model")
	}
	if _, e = next.Engine.Retry(j.ID); e != nil {
		t.Fatal(e)
	}
	if waitJob(t, next.Store, j.ID).Status != "succeeded" {
		t.Fatal("recovered job failed")
	}
}
func TestWAVValidationAndFLACRoundTrip(t *testing.T) {
	data := make([]byte, 4800)
	for i := 0; i < len(data); i += 2 {
		binary.LittleEndian.PutUint16(data[i:], uint16(i*7))
	}
	wav := makeWAV(data, 24000, 1)
	if _, e := normalizeWAV(wav[:len(wav)-1]); e == nil {
		t.Fatal("truncated wav accepted")
	}
	broken := append([]byte{}, wav...)
	binary.LittleEndian.PutUint16(broken[34:], 24)
	if _, e := normalizeWAV(broken); e == nil {
		t.Fatal("unsupported bit depth accepted")
	}
	streamed := append([]byte{}, wav...)
	binary.LittleEndian.PutUint32(streamed[40:], ^uint32(0))
	normal, e := normalizeWAV(streamed)
	if e != nil || !bytes.Equal(normal, wav) {
		t.Fatal("streaming header normalization failed")
	}
	path, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("optional FFmpeg integration")
	}
	encoded, e := ffmpeg(context.Background(), path, wav, "-i", "pipe:0", "-c:a", "flac", "-f", "flac", "pipe:1")
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := decodeFLAC(encoded)
	if e != nil || !bytes.Equal(decoded, wav) {
		t.Fatal("FLAC round trip changed samples", e)
	}
}
func TestProviderProtocolsAndSecretRedaction(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing backend auth")
		}
		if r.URL.Path == "/responses" {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["store"] != false || body["text"] == nil {
				t.Error("missing privacy/schema settings")
			}
			fmt.Fprint(w, `{"status":"completed","usage":{"input_tokens":5},"output":[{"content":[{"type":"output_text","text":"{\"items\":[{\"segment_id\":\"one\",\"translation\":\"你好\"}],\"summary\":\"问候\"}"}]}]}`)
		} else {
			w.Header().Set("Content-Type", "audio/pcm;rate=24000;channels=1")
			w.Write(make([]byte, 480))
		}
	}))
	defer server.Close()
	cfg := testConfig(t)
	cfg.ResponsesURL = server.URL
	cfg.TTSURL = server.URL
	cfg.TTSProvider = "openrouter"
	cfg.TTSModel = "google/gemini-test"
	p := &Provider{Config: cfg}
	result, e := p.Translate(context.Background(), ItemPayload{Targets: []Target{{ID: "one", Text: "こんにちは"}}})
	if e != nil || result.Items[0].Translation != "你好" {
		t.Fatal(e)
	}
	raw, e := p.Synthesize(context.Background(), "テスト")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = parseWAV(raw); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, "test-secret must not appear")
	}))
	defer bad.Close()
	p.Config.ResponsesURL = bad.URL
	_, e = p.Translate(context.Background(), ItemPayload{})
	if e == nil || strings.Contains(e.Error(), "test-secret") {
		t.Fatal("unsafe error")
	}
}
func TestHTTPRangeOriginAndSourceConflict(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	_, c := seed(t, s.Store, "原文。")
	host := httptest.NewServer(s.Handler)
	defer host.Close()
	asset, e := s.Audio.Generate(context.Background(), "テスト", false)
	if e != nil {
		t.Fatal(e)
	}
	req, _ := http.NewRequest("GET", host.URL+"/api/audio/"+asset+"?format=wav", nil)
	req.Header.Set("Range", "bytes=0-15")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || len(raw) != 16 || resp.Header.Get("ETag") == "" {
		t.Fatal("range missing")
	}
	for _, marker := range []bool{false, true} {
		req, _ = http.NewRequest("PATCH", host.URL+"/api/chapters/"+c.ID+"/source", strings.NewReader(`{"title":"标题","raw":"修改","revision":99}`))
		if marker {
			req.Header.Set("X-Reader-Request", "1")
		}
		resp, e = http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		resp.Body.Close()
		expected := 403
		if marker {
			expected = 409
		}
		if resp.StatusCode != expected {
			t.Fatalf("status=%d want=%d", resp.StatusCode, expected)
		}
	}
	req, _ = http.NewRequest("POST", host.URL+"/api/books", strings.NewReader(`{"title":"x"}`))
	req.Header.Set("X-Reader-Request", "1")
	req.Header.Set("Origin", "https://evil.invalid")
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("cross-origin write allowed")
	}
}
func TestCompressionPublicationAndCleanup(t *testing.T) {
	path, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("optional FFmpeg integration")
	}
	cfg := testConfig(t)
	cfg.FFmpeg = path
	s, e := NewServer(cfg, &fakeTranslator{}, &fakeSynth{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	asset, e := s.Audio.Generate(context.Background(), "音声", false)
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, format, e := s.Audio.Read(asset, false)
		if e != nil {
			t.Fatal(e)
		}
		if format == "flac" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, format, e := s.Audio.Read(asset, true)
	if e != nil || format != "wav" {
		t.Fatal("wav fallback failed", e)
	}
	if _, e = parseWAV(raw); e != nil {
		t.Fatal(e)
	}
	count, e := s.Audio.Cleanup()
	if e != nil || count != 1 {
		t.Fatal("orphan cleanup failed", e)
	}
	if _, _, e = s.Audio.Read(asset, true); e == nil {
		t.Fatal("deleted audio resurrected")
	}
	files, _ := os.ReadDir(filepath.Join(cfg.DataDir, "audio"))
	for _, f := range files {
		if strings.HasPrefix(f.Name(), asset) {
			t.Fatal("audio file not removed")
		}
	}
}
