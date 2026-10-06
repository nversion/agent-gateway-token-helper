// Package lock provides an exclusive, cross-process file lock. It serializes
// concurrent agw-token invocations for the same profile so only one of them
// opens a browser or uses a (possibly rotating) refresh token.
package lock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// pollInterval is how often Acquire retries while another process holds the
// lock. Waiting is rare and usually behind a browser login, so this only has
// to be short next to a human.
const pollInterval = 100 * time.Millisecond

// Lock is a held lock; release it with Unlock.
type Lock struct {
	f *os.File
}

// Acquire waits until it holds the exclusive lock on path, creating the file
// and its directory if needed. It gives up with ctx's error when ctx is done,
// so Ctrl-C works while another invocation is signing in.
func Acquire(ctx context.Context, path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		ok, err := tryLockFile(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if ok {
			return &Lock{f: f}, nil
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}

// Unlock releases the lock. The lock file is left in place: removing it would
// let a waiter and a newcomer lock different inodes.
func (l *Lock) Unlock() error {
	err := unlockFile(l.f)
	if cerr := l.f.Close(); err == nil {
		err = cerr
	}
	return err
}
