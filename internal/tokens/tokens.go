// Package tokens hands out a valid ID token: from the store while it has
// enough life left, otherwise by refreshing it, and as a last resort by
// signing in through the browser.
package tokens

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"golang.org/x/oauth2"

	"github.com/nversion/agent-gateway-token-helper/internal/config"
	"github.com/nversion/agent-gateway-token-helper/internal/discovery"
	"github.com/nversion/agent-gateway-token-helper/internal/idtoken"
	"github.com/nversion/agent-gateway-token-helper/internal/lock"
	"github.com/nversion/agent-gateway-token-helper/internal/login"
	"github.com/nversion/agent-gateway-token-helper/internal/store"
)

// ErrNoIDToken is returned when a token response has no id_token.
var ErrNoIDToken = errors.New("token response has no id_token")

// Manager gets tokens for one configuration.
type Manager struct {
	Config *config.Config
	Store  *store.Store
	// HTTPClient is used for discovery and the token endpoint.
	HTTPClient *http.Client
	// OpenBrowser opens the authorization URL unless Config.NoBrowser is set.
	OpenBrowser func(url string) error
	// Stderr receives messages for the user.
	Stderr io.Writer
	Now    func() time.Time
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(m.Stderr, "agw-token: "+format+"\n", args...)
}

func (m *Manager) lock() (*lock.Lock, error) {
	return lock.Acquire(filepath.Join(m.Config.Dirs.State, "locks", m.Config.Key()+".lock"))
}

// Token returns an ID token valid for at least Config.MinValid when the
// provider allows it. forceLogin skips the store and refresh token.
func (m *Manager) Token(ctx context.Context, forceLogin bool) (*store.Entry, error) {
	l, err := m.lock()
	if err != nil {
		return nil, err
	}
	defer func() { _ = l.Unlock() }()

	key := m.Config.Key()
	cached, err := m.Store.Get(key)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		m.logf("ignoring stored token: %v", err)
	}

	if !forceLogin && cached != nil {
		if cached.Expiry.Sub(m.now()) > m.Config.MinValid {
			return cached, nil
		}
		if cached.RefreshToken != "" {
			e, err := m.refresh(ctx, cached)
			if err == nil {
				m.save(key, e)
				return e, nil
			}
			m.logf("refreshing the token failed, signing in again: %v", err)
		}
	}

	e, err := m.login(ctx)
	if err != nil {
		return nil, err
	}
	m.save(key, e)
	return e, nil
}

// save stores e. Failing to store is reported but not fatal: the caller still
// gets its token, it just has to sign in again next time.
func (m *Manager) save(key string, e *store.Entry) {
	if err := m.Store.Set(key, e); err != nil {
		m.logf("warning: %v", err)
	}
}

// Stored returns the stored entry without refreshing it.
func (m *Manager) Stored() (*store.Entry, error) {
	return m.Store.Get(m.Config.Key())
}

// Logout deletes the stored tokens.
func (m *Manager) Logout() error {
	l, err := m.lock()
	if err != nil {
		return err
	}
	defer func() { _ = l.Unlock() }()
	return m.Store.Delete(m.Config.Key())
}

func (m *Manager) client() *http.Client {
	if m.HTTPClient != nil {
		return m.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (m *Manager) discover(ctx context.Context) (*discovery.Document, error) {
	return discovery.Get(ctx, m.client(), m.Config.Issuer, m.Config.Dirs.Cache, m.now())
}

func (m *Manager) oauthConfig(doc *discovery.Document) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     m.Config.ClientID,
		ClientSecret: m.Config.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   doc.AuthorizationEndpoint,
			TokenURL:  doc.TokenEndpoint,
			AuthStyle: authStyle(m.Config.ClientSecret, doc.TokenEndpointAuthMethodsSupported),
		},
		Scopes: m.Config.Scopes,
	}
}

// authStyle picks how the client authenticates at the token endpoint, so the
// oauth2 package doesn't have to probe (which costs a failed request).
func authStyle(secret string, supported []string) oauth2.AuthStyle {
	if secret == "" {
		return oauth2.AuthStyleInParams
	}
	if len(supported) == 0 || slices.Contains(supported, "client_secret_basic") {
		return oauth2.AuthStyleInHeader
	}
	return oauth2.AuthStyleInParams
}

func (m *Manager) refresh(ctx context.Context, cached *store.Entry) (*store.Entry, error) {
	doc, err := m.discover(ctx)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, m.client())
	tok, err := m.oauthConfig(doc).TokenSource(ctx, &oauth2.Token{RefreshToken: cached.RefreshToken}).Token()
	if err != nil {
		return nil, err
	}
	return m.entry(tok, idtoken.Expect{
		Issuer:   doc.Issuer,
		ClientID: m.Config.ClientID,
		Subject:  cached.Subject,
		Now:      m.now(),
	}, cached.RefreshToken)
}

func (m *Manager) login(ctx context.Context) (*store.Entry, error) {
	doc, err := m.discover(ctx)
	if err != nil {
		return nil, err
	}
	conf := m.oauthConfig(doc)
	open := m.OpenBrowser
	if m.Config.NoBrowser {
		open = nil
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, m.client())
	res, err := login.Run(ctx, login.Params{
		AuthURL:      conf.Endpoint.AuthURL,
		TokenURL:     conf.Endpoint.TokenURL,
		AuthStyle:    conf.Endpoint.AuthStyle,
		ClientID:     conf.ClientID,
		ClientSecret: conf.ClientSecret,
		Scopes:       conf.Scopes,
		AuthParams:   m.Config.AuthParams,
		RedirectHost: m.Config.RedirectHost,
		RedirectPort: m.Config.RedirectPort,
		PortSearch:   m.Config.PortSearch,
		CallbackPath: m.Config.CallbackPath,
		OpenBrowser:  open,
		Out:          m.Stderr,
		Timeout:      m.Config.LoginTimeout,
	})
	if err != nil {
		return nil, err
	}
	return m.entry(res.Token, idtoken.Expect{
		Issuer:   doc.Issuer,
		ClientID: m.Config.ClientID,
		Nonce:    res.Nonce,
		Now:      m.now(),
	}, "")
}

// entry validates the ID token in tok and builds the store entry.
// prevRefresh is kept when the provider did not rotate the refresh token.
func (m *Manager) entry(tok *oauth2.Token, expect idtoken.Expect, prevRefresh string) (*store.Entry, error) {
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, ErrNoIDToken
	}
	claims, err := idtoken.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := claims.Validate(expect); err != nil {
		return nil, err
	}
	refresh := tok.RefreshToken
	if refresh == "" {
		refresh = prevRefresh
	}
	return &store.Entry{
		IDToken:           raw,
		RefreshToken:      refresh,
		AccessToken:       tok.AccessToken,
		AccessTokenExpiry: tok.Expiry,
		Issuer:            claims.Issuer,
		Subject:           claims.Subject,
		Email:             claims.Email,
		Expiry:            claims.Expiry.Time(),
	}, nil
}
