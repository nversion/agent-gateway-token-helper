// Package store persists tokens in the platform's secret storage: the macOS
// Keychain, a DPAPI-encrypted file on Windows, and the Secret Service (via
// secret-tool) on Linux, falling back to a 0600 file where those are
// unavailable.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound is returned by Get when nothing is stored under the key.
var ErrNotFound = errors.New("no stored token")

// Entry is what gets stored for a configuration.
type Entry struct {
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	// AccessTokenExpiry is when AccessToken expires; it is not tracked
	// further since the helper hands out the ID token.
	AccessTokenExpiry time.Time `json:"access_token_expiry,omitzero"`
	Issuer            string    `json:"issuer"`
	Subject           string    `json:"subject"`
	Email             string    `json:"email,omitempty"`
	// Expiry is the ID token's exp.
	Expiry time.Time `json:"expiry"`
}

// Backend stores opaque blobs by key.
type Backend interface {
	// Name describes the backend for "agw-token status".
	Name() string
	// Load returns ErrNotFound when nothing is stored under key.
	Load(key string) ([]byte, error)
	Save(key string, data []byte) error
	// Delete succeeds when nothing is stored under key.
	Delete(key string) error
}

// Store stores Entries in a Backend.
type Store struct {
	b Backend
}

// New wraps a Backend.
func New(b Backend) *Store {
	return &Store{b: b}
}

// Open returns the store for kind "auto" (the platform's secret storage) or
// "file" (a 0600 file under dir).
func Open(kind, dir string) (*Store, error) {
	switch kind {
	case "file":
		return New(newFileBackend(dir, "file", nil)), nil
	case "auto":
		return New(platformBackend(dir)), nil
	default:
		return nil, fmt.Errorf("unknown store %q", kind)
	}
}

// Name describes the backend.
func (s *Store) Name() string {
	return s.b.Name()
}

// Get returns the entry for key, or ErrNotFound.
func (s *Store) Get(key string) (*Entry, error) {
	data, err := s.b.Load(key)
	if err != nil {
		return nil, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("decoding stored token from %s: %w", s.b.Name(), err)
	}
	return &e, nil
}

// Set stores e under key.
func (s *Store) Set(key string, e *Entry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := s.b.Save(key, data); err != nil {
		return fmt.Errorf("saving token to %s: %w", s.b.Name(), err)
	}
	return nil
}

// Delete removes the entry for key.
func (s *Store) Delete(key string) error {
	if err := s.b.Delete(key); err != nil {
		return fmt.Errorf("deleting token from %s: %w", s.b.Name(), err)
	}
	return nil
}
