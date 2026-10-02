package lock

import (
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
			l, err := Acquire(path)
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
