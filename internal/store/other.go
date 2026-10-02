//go:build !darwin && !windows && !linux

package store

func platformBackend(dir string) Backend {
	return newFileBackend(dir, "file", nil)
}
