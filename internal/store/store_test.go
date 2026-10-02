package store

import (
	"crypto/rand"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

func roundTrip(t *testing.T, s *Store) {
	t.Helper()
	key := "test-" + rand.Text()
	t.Cleanup(func() { _ = s.Delete(key) })

	if _, err := s.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before Set: %v, want ErrNotFound", err)
	}
	want := &Entry{IDToken: "id", RefreshToken: "rt", Issuer: "https://i", Subject: "alice", Expiry: time.Unix(1_800_000_000, 0).UTC()}
	for _, rt := range []string{"rt", "rt2"} {
		want.RefreshToken = rt
		if err := s.Set(key, want); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := s.Get(key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if *got != *want {
			t.Fatalf("Get = %+v, want %+v", got, want)
		}
	}
	if err := s.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Delete(key); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if _, err := s.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: %v, want ErrNotFound", err)
	}
}

func TestFileStore(t *testing.T) {
	dir := t.TempDir()
	s, err := Open("file", dir)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, s)

	if err := s.Set("perm", &Entry{IDToken: "x"}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(s.b.(*fileBackend).path("perm"))
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("token file mode %o, want 600", perm)
		}
	}
}

// TestPlatformStore exercises the real Keychain / DPAPI / Secret Service.
// It is opt-in because it writes to the user's secret storage.
func TestPlatformStore(t *testing.T) {
	if os.Getenv("AGW_TEST_NATIVE_STORE") == "" {
		t.Skip("set AGW_TEST_NATIVE_STORE=1 to test the platform secret store")
	}
	s, err := Open("auto", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("backend: %s", s.Name())
	roundTrip(t, s)
}
