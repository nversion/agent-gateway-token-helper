// Package lock provides an exclusive, cross-process file lock. It serializes
// concurrent agw-token invocations for the same profile so only one of them
// opens a browser or uses a (possibly rotating) refresh token.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
)

// Lock is a held lock; release it with Unlock.
type Lock struct {
	f *os.File
}

// Acquire blocks until it holds the exclusive lock on path, creating the file
// and its directory if needed.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return &Lock{f: f}, nil
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
