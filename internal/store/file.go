package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// cipher optionally transforms blobs on their way to and from disk.
type cipher struct {
	seal, open func([]byte) ([]byte, error)
}

// fileBackend stores each key in its own file, readable only by the user.
type fileBackend struct {
	dir    string
	name   string
	cipher *cipher
}

func newFileBackend(dir, name string, c *cipher) *fileBackend {
	return &fileBackend{dir: filepath.Join(dir, "tokens"), name: name, cipher: c}
}

func (f *fileBackend) Name() string {
	return f.name + " (" + f.dir + ")"
}

func (f *fileBackend) path(key string) string {
	return filepath.Join(f.dir, key+".json")
}

func (f *fileBackend) Load(key string) ([]byte, error) {
	data, err := os.ReadFile(f.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if f.cipher != nil {
		return f.cipher.open(data)
	}
	return data, nil
}

func (f *fileBackend) Save(key string, data []byte) error {
	if f.cipher != nil {
		var err error
		if data, err = f.cipher.seal(data); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return err
	}
	// Write to a temp file and rename so a crash never leaves a truncated
	// token file. CreateTemp already uses mode 0600.
	tmp, err := os.CreateTemp(f.dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path(key))
}

func (f *fileBackend) Delete(key string) error {
	err := os.Remove(f.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
