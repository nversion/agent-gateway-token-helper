//go:build unix

package lock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLockFile takes the lock without waiting; it reports false if another
// process holds it.
func tryLockFile(f *os.File) (bool, error) {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, unix.EWOULDBLOCK):
			return false, nil
		case err != unix.EINTR:
			return false, err
		}
	}
}

func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
