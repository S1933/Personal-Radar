package scheduler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureLogger buffers Warn calls so a test can assert the panic was
// caught without depending on stderr output.
type captureLogger struct {
	mu   sync.Mutex
	warns []string
}

func (c *captureLogger) Info(msg string, kv ...any) {}
func (c *captureLogger) Warn(msg string, kv ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warns = append(c.warns, msg)
}

func (c *captureLogger) hasPanic() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, w := range c.warns {
		if strings.Contains(w, "panicked") {
			return true
		}
	}
	return false
}

func TestSchedulerRecoversFromPanic(t *testing.T) {
	log := &captureLogger{}
	s := New(log)
	done := make(chan struct{}, 4)
	s.Add("boom", Spec{Every: 5 * time.Millisecond, Run: func(context.Context) {
		done <- struct{}{}
		panic("boom")
	}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	// Two executions: if the first panic took down the goroutine, the
	// second would never happen.
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("le job ne s'est pas ré-exécuté après une panique")
		}
	}

	// Give the deferred recover + Warn call a moment to land.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !log.hasPanic() {
		time.Sleep(5 * time.Millisecond)
	}
	if !log.hasPanic() {
		t.Fatal("la panique n'a pas été capturée et loguée")
	}
}

func TestRunJobDoesNotPanicOnCleanRun(t *testing.T) {
	log := &captureLogger{}
	s := New(log)
	called := 0
	// Use a 5ms interval job (not a daily one) — the scheduler loop
	// path that exercises runJob directly. The previous version of
	// this test mutated s.jobs[0].spec from the test goroutine
	// while loop() was reading it; that race was caught by -race
	// in CI (passing locally only because the Pi env cannot run
	// -race: VMA limit).
	s.Add("ok", Spec{Every: 5 * time.Millisecond, Run: func(context.Context) {
		called++
	}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	time.Sleep(30 * time.Millisecond)
	if called == 0 {
		t.Fatal("runJob n'a pas été appelée")
	}
	if log.hasPanic() {
		t.Fatal("clean run ne devrait pas loguer de panique")
	}
}
