package main

import (
	"context"
	"log"
	"net/http"
	"novel-reader/internal/app"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := app.LoadConfig()
	service, e := app.NewServer(cfg, nil, nil)
	if e != nil {
		log.Fatal(e)
	}
	defer service.Close()
	server := &http.Server{Addr: cfg.Addr, Handler: service.Handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("栞 · http://%s", cfg.Addr)
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Print(e)
	}
}
