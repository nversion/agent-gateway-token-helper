//go:build darwin && !cgo

package store

// Without cgo there is no Security.framework, so a binary cross-compiled
// with CGO_ENABLED=0 falls back to the file store; status shows this.
func platformBackend(dir string) Backend {
	return newFileBackend(dir, "file (built without cgo, Keychain unavailable)", nil)
}
