package app

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// A resizable limiter never replaces a semaphore that callers still hold.
// Lowering the limit lets existing synthesis finish and gates new requests.
type synthesisLimiter struct {
	mu            sync.Mutex
	limit, active int
	changed       chan struct{}
}

func newSynthesisLimiter(limit int) *synthesisLimiter {
	if limit < 1 || limit > 32 {
		limit = 10
	}
	return &synthesisLimiter{limit: limit, changed: make(chan struct{})}
}
func (l *synthesisLimiter) acquire(ctx context.Context) error {
	for {
		l.mu.Lock()
		if err := ctx.Err(); err != nil {
			l.mu.Unlock()
			return err
		}
		if l.active < l.limit {
			l.active++
			l.mu.Unlock()
			return nil
		}
		changed := l.changed
		l.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}
func (l *synthesisLimiter) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active--
	close(l.changed)
	l.changed = make(chan struct{})
}
func (l *synthesisLimiter) set(limit int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = limit
	close(l.changed)
	l.changed = make(chan struct{})
}
func (l *synthesisLimiter) value() int { l.mu.Lock(); defer l.mu.Unlock(); return l.limit }
func (s *Store) AudioConcurrency(fallback int) (int, error) {
	if fallback < 1 || fallback > 32 {
		fallback = 10
	}
	var value int
	err := s.DB.QueryRow("SELECT value FROM app_settings WHERE key='tts_concurrency'").Scan(&value)
	if err == sql.ErrNoRows {
		return fallback, nil
	}
	if err != nil {
		return 0, err
	}
	if value < 1 || value > 32 {
		return 0, fmt.Errorf("已保存的语音并发设置无效")
	}
	return value, nil
}
func (a *Audio) SetConcurrency(value int) error {
	if value < 1 || value > 32 {
		return fmt.Errorf("语音生成并发量必须为 1–32 的整数")
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if _, err := a.Store.DB.Exec("INSERT INTO app_settings VALUES('tts_concurrency',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", value); err != nil {
		return err
	}
	a.limiter.set(value)
	return nil
}
