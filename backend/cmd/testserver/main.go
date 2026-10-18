// testserver is an isolated, offline fixture. It never loads .env or calls model services.
package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"net/http"
	"novel-reader/internal/app"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type translator struct{}

func (translator) Translate(ctx context.Context, p app.ItemPayload) (app.TranslateResult, error) {
	select {
	case <-ctx.Done():
		return app.TranslateResult{}, ctx.Err()
	case <-time.After(120 * time.Millisecond):
	}
	r := app.TranslateResult{Summary: "测试摘要"}
	for _, t := range p.Targets {
		r.Items = append(r.Items, app.Translation{SegmentID: t.ID, Translation: "窗外的风轻轻吹过，故事从这里开始。"})
	}
	return r, nil
}

type speech struct{}

func (speech) Synthesize(ctx context.Context, text string) ([]byte, error) {
	rate := 24000
	n := rate
	data := make([]byte, 44+n*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], uint32(rate))
	binary.LittleEndian.PutUint32(data[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(n*2))
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(int16(2000*math.Sin(2*math.Pi*220*float64(i)/float64(rate)))))
	}
	return data, nil
}
func main() {
	dir, e := os.MkdirTemp("", "novel-reader-e2e-")
	if e != nil {
		log.Fatal(e)
	}
	defer os.RemoveAll(dir)
	front := os.Getenv("TEST_FRONTEND_DIR")
	if front == "" {
		front = "../frontend/dist"
	}
	c := app.Config{Addr: "127.0.0.1:8788", DataDir: dir, FrontendDir: front, BatchChars: 2400, TTSConcurrency: 2, Model: "offline-fixture", TTSModel: "offline-tone", TTSProvider: "fixture", FFmpeg: "disabled-in-fixture"}
	s, e := app.NewServer(c, translator{}, speech{})
	if e != nil {
		log.Fatal(e)
	}
	defer s.Close()
	httpServer := &http.Server{Addr: c.Addr, Handler: s.Handler, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { <-ctx.Done(); _ = httpServer.Close() }()
	fmt.Println("Offline test fixture http://" + c.Addr)
	if e = httpServer.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Print(e)
	}
}
