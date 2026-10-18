package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewerDatabaseIsRefusedWithoutDowngrade(t *testing.T) {
	root := t.TempDir()
	s, e := OpenStore(root)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("INSERT INTO schema_migrations VALUES(2)"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	next, e := OpenStore(root)
	if e == nil {
		next.Close()
		t.Fatal("newer DB opened by older application")
	}
}
func TestGlossaryChangesBlockQueuedTranslation(t *testing.T) {
	tr := &fakeTranslator{}
	s := newTestServer(t, tr, &fakeSynth{})
	b, c := seed(t, s.Store, "人物の名前。")
	s.Engine.Close()
	j, e := s.Engine.Create(c.ID, "translation", 1, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.SaveGlossary(b.ID, Glossary{Revision: 1, Terms: []Term{{Source: "人物", Target: "人物"}}}); e != nil {
		t.Fatal(e)
	}
	s.Engine = NewEngine(s.Store, s.Config, tr, s.Audio)
	j = waitJob(t, s.Store, j.ID)
	if j.Status != "failed" || tr.calls.Load() != 0 {
		t.Fatal("obsolete glossary sent to model")
	}
}
func TestAudioSettingsChangeCreatesNewAsset(t *testing.T) {
	synth := &fakeSynth{}
	s := newTestServer(t, &fakeTranslator{}, synth)
	_, c := seed(t, s.Store, "音声。")
	j, e := s.Engine.Create(c.ID, "audio", 1, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	waitJob(t, s.Store, j.ID)
	before, _ := s.Store.Chapter(c.ID)
	s.Engine.Close()
	s.Audio.Close()
	cfg := s.Config
	cfg.Voice = "different"
	s.Audio = NewAudio(s.Store, cfg, synth)
	s.Engine = NewEngine(s.Store, cfg, &fakeTranslator{}, s.Audio)
	j, e = s.Engine.Create(c.ID, "audio", 1, nil, false)
	if e != nil || j.Total != 1 {
		t.Fatal("changed voice reused old attachment", e)
	}
	waitJob(t, s.Store, j.ID)
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].Clips[0].AssetID == before.Segments[0].Clips[0].AssetID || synth.calls.Load() != 2 {
		t.Fatal("voice cache not isolated")
	}
}
func TestSpeechEditClearsObsoletePlaybackPosition(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	b, c := seed(t, s.Store, "原文。")
	seg := c.Segments[0]
	p, e := s.Store.SaveProgress(b.ID, Progress{ChapterID: c.ID, SegmentID: seg.ID, ClipID: seg.Clips[0].ID, Seconds: 0.05})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.EditSpeech(seg.ID, "げんぶん。", 1); e != nil {
		t.Fatal(e)
	}
	next, _ := s.Store.Progress(b.ID)
	if next.ClipID != "" || next.Revision != p.Revision+1 {
		t.Fatal("obsolete clip survived speech edit")
	}
	if _, e = s.Store.SaveProgress(b.ID, p); e == nil {
		t.Fatal("old progress accepted")
	}
}
func TestCleanupRetainsReferencesAndRecoversCrashFiles(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	_, c := seed(t, s.Store, "キャッシュ。")
	j, e := s.Engine.Create(c.ID, "audio", 1, nil, false)
	if e != nil {
		t.Fatal(e)
	}
	waitJob(t, s.Store, j.ID)
	c, _ = s.Store.Chapter(c.ID)
	asset := c.Segments[0].Clips[0].AssetID
	orphan := filepath.Join(s.Store.Root, "audio", id()+".wav")
	if e = os.WriteFile(orphan, []byte("interrupted"), 0600); e != nil {
		t.Fatal(e)
	}
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(orphan, old, old)
	n, e := s.Audio.Cleanup()
	if e != nil || n != 0 {
		t.Fatal("referenced asset removed", e)
	}
	if _, e = os.Stat(orphan); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("crash file retained")
	}
	if _, _, e = s.Audio.Read(asset, true); e != nil {
		t.Fatal("referenced asset missing")
	}
}
func TestReadAudioDoesNotInvokePaidService(t *testing.T) {
	synth := &fakeSynth{}
	s := newTestServer(t, &fakeTranslator{}, synth)
	if _, _, e := s.Audio.Read("missing", true); e == nil {
		t.Fatal("missing asset should fail")
	}
	if synth.calls.Load() != 0 {
		t.Fatal("GET generated audio")
	}
	if _, e := s.Audio.Generate(context.Background(), "hello", false); e != nil {
		t.Fatal(e)
	}
	if len(s.Audio.keys) != 0 {
		t.Fatal("unused keyed locks leaked")
	}
}
