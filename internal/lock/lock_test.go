package lock

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireExcludes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "x.lock")
	var held atomic.Int32
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			l, err := Acquire(context.Background(), path)
			if err != nil {
				t.Error(err)
				return
			}
			if n := held.Add(1); n != 1 {
				t.Errorf("%d holders at once", n)
			}
			time.Sleep(10 * time.Millisecond)
			held.Add(-1)
			if err := l.Unlock(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func TestAcquireCanceledWhileWaiting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	held, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Unlock() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		l, err := Acquire(ctx, path)
		if err == nil {
			_ = l.Unlock()
		}
		done <- err
	}()
	time.Sleep(2 * pollInterval)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Acquire = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Acquire still waiting after cancel")
	}
}
