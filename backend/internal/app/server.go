package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Server struct {
	Store   *Store
	Config  Config
	Engine  *Engine
	Audio   *Audio
	Handler http.Handler
}

func NewServer(c Config, t Translator, synth Synthesizer) (*Server, error) {
	store, e := OpenStore(c.DataDir)
	if e != nil {
		return nil, e
	}
	c.TTSConcurrency, e = store.AudioConcurrency(c.TTSConcurrency)
	if e != nil {
		store.Close()
		return nil, e
	}
	provider := &Provider{Config: c}
	if t == nil {
		t = provider
	}
	if synth == nil {
		synth = provider
	}
	audio := NewAudio(store, c, synth)
	s := &Server{Store: store, Config: c, Audio: audio}
	s.Engine = NewEngine(store, c, t, audio)
	s.Handler = s.routes()
	return s, nil
}
func (s *Server) Close() { s.Engine.Close(); s.Audio.Close(); s.Store.Close() }
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, e error) {
	status := 400
	if errors.Is(e, ErrNotFound) {
		status = 404
	}
	if errors.Is(e, ErrConflict) {
		status = 409
	}
	jsonResponse(w, status, map[string]string{"error": safeMessage(e)})
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		failure(w, fmt.Errorf("请求 JSON 无效或过大"))
		return false
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		failure(w, fmt.Errorf("请求只能包含一个 JSON 对象"))
		return false
	}
	return true
}
func (s *Server) routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]any{"status": "ok"}) })
	m.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 200, map[string]any{"translation_ready": s.Config.translationReady(), "tts_ready": s.Config.speechReady(), "model": s.Config.Model, "tts_provider": s.Config.TTSProvider, "tts_model": s.Config.TTSModel, "voice": s.Config.Voice, "flac_enabled": s.Audio.enabled, "tts_concurrency": s.Audio.limiter.value()})
	})
	m.HandleFunc("PATCH /api/settings/audio", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Concurrency int `json:"concurrency"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := s.Audio.SetConcurrency(in.Concurrency); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]int{"tts_concurrency": s.Audio.limiter.value()})
	})
	m.HandleFunc("GET /api/books", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Books()
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("POST /api/books", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title  string `json:"title"`
			Author string `json:"author"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Store.CreateBook(in.Title, in.Author)
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 201, v)
	})
	m.HandleFunc("GET /api/books/{book}/chapters", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Chapters(r.PathValue("book"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("POST /api/books/{book}/chapters", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title string `json:"title"`
			Raw   string `json:"raw"`
		}
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Store.CreateChapter(r.PathValue("book"), in.Title, in.Raw)
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 201, map[string]string{"id": v})
	})
	m.HandleFunc("PUT /api/books/{book}/chapters/order", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Expected []string `json:"expected_order"`
			Ordered  []string `json:"chapter_ids"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := s.Store.ReorderChapters(r.PathValue("book"), in.Expected, in.Ordered); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("DELETE /api/chapters/{chapter}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := s.Engine.DeleteChapter(r.PathValue("chapter"), in.Revision); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("GET /api/chapters/{chapter}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Chapter(r.PathValue("chapter"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("GET /api/chapters/{chapter}/translation-batches", func(w http.ResponseWriter, r *http.Request) {
		b, e := s.Store.TranslationBatches(r.PathValue("chapter"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, b)
	})
	m.HandleFunc("POST /api/chapters/{chapter}/translation-batches", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		b, e := s.Engine.CreateTranslationBatch(r.PathValue("chapter"), in.Revision)
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 202, b)
	})
	m.HandleFunc("POST /api/translation-batches/{batch}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Engine.CancelTranslationBatch(r.PathValue("batch")); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/translation-batches/{batch}/retry", func(w http.ResponseWriter, r *http.Request) {
		b, e := s.Engine.RetryTranslationBatch(r.PathValue("batch"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 202, b)
	})
	m.HandleFunc("PATCH /api/chapters/{chapter}/source", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title    string `json:"title"`
			Raw      string `json:"raw"`
			Revision int    `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := s.Store.EditSource(r.PathValue("chapter"), in.Title, in.Raw, in.Revision); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	for _, kind := range []string{"translation", "audio"} {
		m.HandleFunc("POST /api/chapters/{chapter}/"+kind+"-jobs", func(w http.ResponseWriter, r *http.Request) {
			var in struct {
				Revision int      `json:"revision"`
				Segments []string `json:"segments"`
				Force    bool     `json:"force"`
			}
			if !decode(w, r, &in) {
				return
			}
			v, e := s.Engine.Create(r.PathValue("chapter"), kind, in.Revision, in.Segments, in.Force)
			if e != nil {
				failure(w, e)
				return
			}
			jsonResponse(w, 202, v)
		})
	}
	m.HandleFunc("PATCH /api/chapters/{chapter}/translations", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Revision int                     `json:"revision"`
			Items    []ManualTranslationEdit `json:"items"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := s.Store.EditChapterTranslations(r.PathValue("chapter"), in.Revision, in.Items); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("PATCH /api/segments/{segment}/translation", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text     string `json:"text"`
			Revision int    `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := s.Store.EditTranslation(r.PathValue("segment"), in.Text, in.Revision); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("PATCH /api/segments/{segment}/speech-text", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text     string `json:"text"`
			Revision int    `json:"revision"`
		}
		if !decode(w, r, &in) {
			return
		}
		if e := s.Store.EditSpeech(r.PathValue("segment"), in.Text, in.Revision); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("GET /api/books/{book}/glossary", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Glossary(r.PathValue("book"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("PUT /api/books/{book}/glossary", func(w http.ResponseWriter, r *http.Request) {
		var in Glossary
		if !decode(w, r, &in) {
			return
		}
		if e := s.Store.SaveGlossary(r.PathValue("book"), in); e != nil {
			failure(w, e)
			return
		}
		v, e := s.Store.Glossary(r.PathValue("book"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("GET /api/books/{book}/progress", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Progress(r.PathValue("book"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("PUT /api/books/{book}/progress", func(w http.ResponseWriter, r *http.Request) {
		var in Progress
		if !decode(w, r, &in) {
			return
		}
		v, e := s.Store.SaveProgress(r.PathValue("book"), in)
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Jobs(r.URL.Query().Get("chapter"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("GET /api/jobs/{job}", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Store.Job(r.PathValue("job"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, v)
	})
	m.HandleFunc("POST /api/jobs/{job}/cancel", func(w http.ResponseWriter, r *http.Request) {
		if e := s.Engine.Cancel(r.PathValue("job")); e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/jobs/{job}/retry", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Engine.Retry(r.PathValue("job"))
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 202, v)
	})
	m.HandleFunc("GET /api/jobs/{job}/attempts", func(w http.ResponseWriter, r *http.Request) {
		rows, e := s.Store.DB.Query("SELECT a.created,a.elapsed_ms,a.usage,a.status FROM generation_attempts a JOIN job_items i ON i.id=a.item_id WHERE i.job_id=? ORDER BY a.created", r.PathValue("job"))
		if e != nil {
			failure(w, e)
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var created, status string
			var ms int
			var usage *string
			if e = rows.Scan(&created, &ms, &usage, &status); e != nil {
				failure(w, e)
				return
			}
			var parsed any
			if usage != nil {
				_ = json.Unmarshal([]byte(*usage), &parsed)
			}
			out = append(out, map[string]any{"created": created, "elapsed_ms": ms, "usage": parsed, "status": status})
		}
		jsonResponse(w, 200, out)
	})
	m.HandleFunc("GET /api/audio/{asset}", func(w http.ResponseWriter, r *http.Request) {
		raw, format, e := s.Audio.Read(r.PathValue("asset"), r.URL.Query().Get("format") == "wav")
		if e != nil {
			failure(w, e)
			return
		}
		etag := `"` + digest(raw) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("Content-Type", "audio/"+format)
		http.ServeContent(w, r, r.PathValue("asset")+"."+format, time.Time{}, bytes.NewReader(raw))
	})
	m.HandleFunc("POST /api/cache/cleanup", func(w http.ResponseWriter, r *http.Request) {
		n, e := s.Audio.Cleanup()
		if e != nil {
			failure(w, e)
			return
		}
		jsonResponse(w, 200, map[string]int{"removed": n})
	})
	m.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			jobs, e := s.Store.Jobs(r.URL.Query().Get("chapter"))
			if e != nil {
				return
			}
			data, _ := json.Marshal(jobs)
			if _, e = fmt.Fprintf(w, "data: %s\n\n", data); e != nil {
				return
			}
			batches, e := s.Store.TranslationBatches(r.URL.Query().Get("chapter"))
			if e != nil {
				return
			}
			batchData, _ := json.Marshal(batches)
			if _, e = fmt.Fprintf(w, "event: batches\ndata: %s\n\n", batchData); e != nil {
				return
			}
			f.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
		}
	})
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, 404, map[string]string{"error": "接口不存在"})
	})
	files := http.FileServer(http.Dir(s.Config.FrontendDir))
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(405)
			return
		}
		path := filepath.Join(s.Config.FrontendDir, filepath.Clean("/"+r.URL.Path))
		if _, e := os.Stat(path); e != nil {
			if strings.Contains(filepath.Base(path), ".") {
				http.NotFound(w, r)
				return
			}
			http.ServeFile(w, r, filepath.Join(s.Config.FrontendDir, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("HTTP handler panic: %v", v)
				jsonResponse(w, 500, map[string]string{"error": "服务内部错误"})
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		host := r.Host
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
		}
		listenHost, _, _ := net.SplitHostPort(s.Config.Addr)
		if listenHost != "0.0.0.0" && listenHost != "::" && host != "localhost" && host != "127.0.0.1" && host != "::1" && host != listenHost {
			http.Error(w, "Host not allowed", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, e := url.Parse(origin)
				if e != nil || u.Host != r.Host {
					http.Error(w, "Origin not allowed", 403)
					return
				}
			}
			if r.Header.Get("X-Reader-Request") != "1" {
				http.Error(w, "Missing request marker", 403)
				return
			}
		}
		m.ServeHTTP(w, r)
	})
}
