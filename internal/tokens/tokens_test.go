package tokens

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nversion/agent-gateway-token-helper/internal/config"
	"github.com/nversion/agent-gateway-token-helper/internal/oidctest"
	"github.com/nversion/agent-gateway-token-helper/internal/store"
)

// syncBuffer is a bytes.Buffer safe for concurrent writers.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// browser plays the user's browser: it follows the authorization URL, which
// the fake provider immediately redirects to the loopback callback.
func browser(u string) error {
	go func() {
		resp, err := http.Get(u)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	return nil
}

func newManager(t *testing.T, p *oidctest.Provider, home string) (*Manager, *syncBuffer) {
	t.Helper()
	st, err := store.Open("file", home)
	if err != nil {
		t.Fatal(err)
	}
	var stderr syncBuffer
	return &Manager{
		Config: &config.Config{
			Issuer:       p.Issuer(),
			ClientID:     p.ClientID(),
			Scopes:       []string{"openid", "email", "offline_access"},
			MinValid:     5 * time.Minute,
			RedirectHost: "127.0.0.1",
			PortSearch:   1,
			CallbackPath: "/callback",
			LoginTimeout: 10 * time.Second,
			Dirs:         config.Dirs{Config: home, Cache: filepath.Join(home, "cache"), State: home},
		},
		Store:       st,
		OpenBrowser: browser,
		Stderr:      &stderr,
	}, &stderr
}

func TestLoginCacheRefresh(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{RotateRefreshTokens: true})
	m, stderr := newManager(t, p, t.TempDir())
	ctx := context.Background()

	first, err := m.Token(ctx, false)
	if err != nil {
		t.Fatalf("first Token: %v\n%s", err, stderr)
	}
	if first.Subject != "alice" || first.RefreshToken == "" {
		t.Fatalf("unexpected entry %+v", first)
	}

	// Cached: no new requests to the provider.
	before := p.Counters()
	second, err := m.Token(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if second.IDToken != first.IDToken || p.Counters() != before {
		t.Fatalf("expected cached token without provider requests; counters %+v -> %+v", before, p.Counters())
	}

	// Close to expiry: refreshed, rotated refresh token persisted.
	m.Now = func() time.Time { return first.Expiry.Add(-time.Minute) }
	third, err := m.Token(ctx, false)
	if err != nil {
		t.Fatalf("refresh Token: %v\n%s", err, stderr)
	}
	if c := p.Counters(); c.Refresh != 1 || c.Authorize != 1 {
		t.Fatalf("counters %+v, want 1 refresh and 1 authorize", c)
	}
	if third.RefreshToken == first.RefreshToken {
		t.Fatal("rotated refresh token was not stored")
	}
	stored, err := m.Stored()
	if err != nil || stored.RefreshToken != third.RefreshToken {
		t.Fatalf("stored %+v, %v", stored, err)
	}
}

func TestRefreshFallsBackToLogin(t *testing.T) {
	tests := []struct {
		name    string
		breakIt func(*oidctest.Provider)
	}{
		{"refresh fails", (*oidctest.Provider).FailNextRefresh},
		{"refresh tokens revoked", (*oidctest.Provider).RevokeRefreshTokens},
		{"no id_token on refresh", func(p *oidctest.Provider) { p.SetOmitIDTokenOnRefresh(true) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := oidctest.Start(t, oidctest.Options{})
			m, stderr := newManager(t, p, t.TempDir())
			first, err := m.Token(context.Background(), false)
			if err != nil {
				t.Fatal(err)
			}
			tt.breakIt(p)
			m.Now = func() time.Time { return first.Expiry.Add(-time.Minute) }
			if _, err := m.Token(context.Background(), false); err != nil {
				t.Fatalf("Token: %v\n%s", err, stderr)
			}
			if c := p.Counters(); c.Refresh != 1 || c.Authorize != 2 {
				t.Fatalf("counters %+v, want 1 refresh and 2 authorizes", c)
			}
			if !strings.Contains(stderr.String(), "signing in again") {
				t.Fatalf("stderr does not explain the fallback:\n%s", stderr)
			}
		})
	}
}

func TestRejectsBadIDToken(t *testing.T) {
	for _, override := range []map[string]any{
		{"aud": "someone-else"},
		{"iss": "https://evil.example"},
		{"nonce": "replayed"},
	} {
		p := oidctest.Start(t, oidctest.Options{})
		p.SetClaimOverrides(override)
		m, _ := newManager(t, p, t.TempDir())
		if _, err := m.Token(context.Background(), false); err == nil {
			t.Errorf("override %v: expected error", override)
		}
		if _, err := m.Stored(); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("override %v: rejected token was stored", override)
		}
	}
}

func TestConcurrentCallsLoginOnce(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	home := t.TempDir()
	var wg sync.WaitGroup
	results := make([]string, 4)
	for i := range results {
		m, stderr := newManager(t, p, home)
		wg.Go(func() {
			e, err := m.Token(context.Background(), false)
			if err != nil {
				t.Errorf("Token: %v\n%s", err, stderr)
				return
			}
			results[i] = e.IDToken
		})
	}
	wg.Wait()
	if c := p.Counters(); c.Authorize != 1 {
		t.Fatalf("%d logins, want 1", c.Authorize)
	}
	for _, r := range results[1:] {
		if r != results[0] {
			t.Fatal("concurrent calls returned different tokens")
		}
	}
}

func TestForceLoginAndLogout(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	m, _ := newManager(t, p, t.TempDir())
	ctx := context.Background()
	if _, err := m.Token(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Token(ctx, true); err != nil {
		t.Fatal(err)
	}
	if c := p.Counters(); c.Authorize != 2 {
		t.Fatalf("%d logins, want 2", c.Authorize)
	}
	if err := m.Logout(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stored(); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Stored after Logout: %v", err)
	}
}

func TestEntraTenantIssuer(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{EntraTenantID: "11111111-2222-3333-4444-555555555555"})
	m, stderr := newManager(t, p, t.TempDir())
	m.Config.Issuer = strings.ReplaceAll(p.Issuer(), "{tenant}", "organizations")
	e, err := m.Token(context.Background(), false)
	if err != nil {
		t.Fatalf("Token: %v\n%s", err, stderr)
	}
	if want := p.BaseURL() + "/11111111-2222-3333-4444-555555555555/v2.0"; e.Issuer != want {
		t.Fatalf("issuer %q, want %q", e.Issuer, want)
	}
}

func TestClientSecretSent(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{ClientSecret: "s3cret"})
	m, stderr := newManager(t, p, t.TempDir())
	if _, err := m.Token(context.Background(), false); err == nil {
		t.Fatal("expected failure without the client secret")
	}
	m.Config.ClientSecret = "s3cret"
	if _, err := m.Token(context.Background(), false); err != nil {
		t.Fatalf("Token: %v\n%s", err, stderr)
	}
}
