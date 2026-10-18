package app

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestManualReadingAndAudioWithoutTranslationService(t *testing.T) {
	for _, unavailable := range []string{"missing-key", "service-failure"} {
		t.Run(unavailable, func(t *testing.T) {
			var calls atomic.Int32
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(401)
				fmt.Fprint(w, `{"error":"unavailable"}`)
			}))
			defer remote.Close()
			c := testConfig(t)
			c.ResponsesURL = remote.URL
			if unavailable == "missing-key" {
				c.ResponsesKey = ""
			}
			s, e := NewServer(c, nil, &fakeSynth{})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			b, ch := seed(t, s.Store, "風が吹いた。\n本を開いた。")
			translation, e := s.Engine.Create(ch.ID, "translation", ch.Revision, []string{ch.Segments[0].ID}, false)
			if e != nil {
				t.Fatal(e)
			}
			if waitJob(t, s.Store, translation.ID).Status != "failed" {
				t.Fatal("expected unavailable translator")
			}
			baseline := calls.Load()
			audio, e := s.Engine.Create(ch.ID, "audio", ch.Revision, nil, false)
			if e != nil {
				t.Fatal(e)
			}
			if waitJob(t, s.Store, audio.ID).Status != "succeeded" {
				t.Fatal("audio depended on translation")
			}
			before, e := s.Store.Chapter(ch.ID)
			if e != nil {
				t.Fatal(e)
			}
			for _, seg := range before.Segments {
				if seg.Translation != "" || seg.Clips[0].AssetID == "" {
					t.Fatal("unexpected translation/audio state")
				}
			}
			first := before.Segments[0]
			if _, e = s.Store.SaveProgress(b.ID, Progress{ChapterID: ch.ID, SegmentID: first.ID, ClipID: first.Clips[0].ID, Seconds: .05}); e != nil {
				t.Fatal(e)
			}
			if e = s.Store.EditChapterTranslations(ch.ID, ch.Revision, []ManualTranslationEdit{{SegmentID: first.ID, Text: "风吹了过来。", Revision: 0}}); e != nil {
				t.Fatal(e)
			}
			after, _ := s.Store.Chapter(ch.ID)
			if !after.Segments[0].Manual || after.Segments[1].Translation != "" || after.Raw != before.Raw || after.Segments[0].Clips[0].AssetID != first.Clips[0].AssetID {
				t.Fatal("manual input changed unrelated data")
			}
			if e = s.Store.EditSpeech(after.Segments[1].ID, "ほんをひらいた。", 1); e != nil {
				t.Fatal(e)
			}
			if _, e = s.Store.Progress(b.ID); e != nil {
				t.Fatal(e)
			}
			if _, _, e = s.Audio.Read(first.Clips[0].AssetID, true); e != nil {
				t.Fatal(e)
			}
			if calls.Load() != baseline {
				t.Fatal("manual reading flow called translator")
			}
		})
	}
}

func TestManualChapterTranslationConflictRollsBackAllEdits(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	_, c := seed(t, s.Store, "一。\n二。")
	if e := s.Store.EditTranslation(c.Segments[1].ID, "另一页面保存的译文", 0); e != nil {
		t.Fatal(e)
	}
	e := s.Store.EditChapterTranslations(c.ID, 1, []ManualTranslationEdit{{SegmentID: c.Segments[0].ID, Text: "不应部分保存", Revision: 0}, {SegmentID: c.Segments[1].ID, Text: "旧版本草稿", Revision: 0}})
	if !errors.Is(e, ErrConflict) {
		t.Fatal("expected conflict", e)
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].Translation != "" || after.Segments[1].Translation != "另一页面保存的译文" {
		t.Fatal("batch was partially saved")
	}
}

func TestManualChapterTranslationsValidateOwnershipAndSourceRevision(t *testing.T) {
	s := newTestServer(t, &fakeTranslator{}, &fakeSynth{})
	_, c := seed(t, s.Store, "一。")
	_, other := seed(t, s.Store, "別の章。")
	for _, edits := range [][]ManualTranslationEdit{
		{{SegmentID: other.Segments[0].ID, Text: "跨章写入", Revision: 0}},
		{{SegmentID: c.Segments[0].ID, Text: "一", Revision: 0}, {SegmentID: c.Segments[0].ID, Text: "重复", Revision: 0}},
		{{SegmentID: c.Segments[0].ID, Text: " ", Revision: 0}},
	} {
		if e := s.Store.EditChapterTranslations(c.ID, 1, edits); e == nil {
			t.Fatal("invalid batch accepted")
		}
	}
	if e := s.Store.EditSource(c.ID, c.Title, "新しい原文。", 1); e != nil {
		t.Fatal(e)
	}
	if e := s.Store.EditChapterTranslations(c.ID, 1, []ManualTranslationEdit{{SegmentID: c.Segments[0].ID, Text: "过期草稿", Revision: 0}}); !errors.Is(e, ErrConflict) {
		t.Fatal("stale chapter accepted", e)
	}
}

func TestManualBatchWinsAgainstInFlightTranslation(t *testing.T) {
	tr := &fakeTranslator{gate: make(chan struct{}), started: make(chan struct{})}
	s := newTestServer(t, tr, &fakeSynth{})
	_, c := seed(t, s.Store, "一。\n二。")
	j, e := s.Engine.Create(c.ID, "translation", 1, []string{c.Segments[0].ID}, false)
	if e != nil {
		t.Fatal(e)
	}
	<-tr.started
	if e = s.Store.EditChapterTranslations(c.ID, 1, []ManualTranslationEdit{{SegmentID: c.Segments[0].ID, Text: "人工输入", Revision: 0}}); e != nil {
		t.Fatal(e)
	}
	close(tr.gate)
	if waitJob(t, s.Store, j.ID).Status != "failed" {
		t.Fatal("stale model output was published")
	}
	after, _ := s.Store.Chapter(c.ID)
	if after.Segments[0].Translation != "人工输入" || after.Segments[1].Translation != "" {
		t.Fatal("model changed manual or partial result")
	}
}
