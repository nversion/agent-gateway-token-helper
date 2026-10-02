// Package oidctest is a fake OpenID Connect provider for tests and local
// experiments. It serves discovery and JWKS documents and a minimal
// authorization code + PKCE flow with refresh tokens, and signs RS256 ID
// tokens with an in-memory key. /authorize has no login UI: it redirects back
// immediately for the configured user, simulating a browser session that is
// already signed in.
//
// The package does not import "testing" so cmd/fake-oidc can use it.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultClientID is the client ID used when Options.ClientID is empty.
const DefaultClientID = "agw-test-client"

// User is the signed-in user the fake issues tokens for.
type User struct {
	Subject string
	Email   string
	// Extra is merged into the ID token claims, e.g. "groups".
	Extra map[string]any
}

// Options configures a Provider.
type Options struct {
	// ClientID is the only client accepted; DefaultClientID if empty.
	ClientID string
	// ClientSecret, when set, must be presented at /token (basic auth or
	// form post). Empty means a public client.
	ClientSecret string
	// User defaults to alice@example.com.
	User User
	// TokenTTL is the lifetime of issued ID and access tokens; 1h if zero.
	TokenTTL time.Duration
	// RotateRefreshTokens issues a new refresh token on every refresh and
	// invalidates the old one.
	RotateRefreshTokens bool
	// EntraTenantID switches to Microsoft Entra ID's multi-tenant style:
	// discovery is served under any "/<tenant>/v2.0" prefix and reports the
	// issuer "<base>/{tenantid}/v2.0", and tokens carry
	// iss "<base>/<EntraTenantID>/v2.0" and a matching tid claim.
	EntraTenantID string
	// Logf, when set, receives one line per request.
	Logf func(format string, args ...any)
}

// Counters counts requests per endpoint.
type Counters struct {
	Discovery    int
	Authorize    int
	CodeExchange int
	Refresh      int
}

// Provider is the fake OIDC provider.
type Provider struct {
	key *rsa.PrivateKey
	kid string

	mu                   sync.Mutex
	base                 string
	opts                 Options
	codes                map[string]*authCode
	refreshTokens        map[string]*session
	counters             Counters
	lastAuthorize        url.Values
	failNextRefresh      bool
	omitIDTokenOnRefresh bool
	claimOverrides       map[string]any
}

type session struct {
	clientID string
	scope    string
	user     User
}

type authCode struct {
	session
	redirectURI   string
	nonce         string
	codeChallenge string
	expiresAt     time.Time
}

// New creates a provider; set its base URL with SetBaseURL once it is
// listening, or use Start.
func New(opts Options) (*Provider, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("oidctest: generating RSA key: %w", err)
	}
	if opts.ClientID == "" {
		opts.ClientID = DefaultClientID
	}
	if opts.User.Subject == "" {
		opts.User = User{Subject: "alice", Email: "alice@example.com"}
	}
	if opts.TokenTTL == 0 {
		opts.TokenTTL = time.Hour
	}
	return &Provider{
		key:           key,
		kid:           "oidctest-1",
		opts:          opts,
		codes:         map[string]*authCode{},
		refreshTokens: map[string]*session{},
	}, nil
}

// TB is the subset of testing.TB Start needs.
type TB interface {
	Helper()
	Cleanup(func())
	Fatalf(format string, args ...any)
}

// Start runs a provider on an httptest server on 127.0.0.1 and stops it when
// the test ends.
func Start(t TB, opts Options) *Provider {
	t.Helper()
	p, err := New(opts)
	if err != nil {
		t.Fatalf("%v", err)
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	p.SetBaseURL(srv.URL)
	return p
}

// SetBaseURL sets the URL the provider is reachable at.
func (p *Provider) SetBaseURL(u string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.base = strings.TrimRight(u, "/")
}

// BaseURL is the URL the provider is reachable at.
func (p *Provider) BaseURL() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.base
}

// ClientID is the accepted client ID.
func (p *Provider) ClientID() string {
	return p.opts.ClientID
}

// Issuer is the issuer to configure a client with. In Entra mode it contains
// "{tenant}" for the client to fill in.
func (p *Provider) Issuer() string {
	if p.opts.EntraTenantID != "" {
		return p.BaseURL() + "/{tenant}/v2.0"
	}
	return p.BaseURL()
}

// SetUser changes the user future authorizations are for.
func (p *Provider) SetUser(u User) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opts.User = u
}

// SetTokenTTL changes the lifetime of tokens issued from now on.
func (p *Provider) SetTokenTTL(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.opts.TokenTTL = d
}

// FailNextRefresh makes the next refresh_token grant fail with invalid_grant.
func (p *Provider) FailNextRefresh() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failNextRefresh = true
}

// RevokeRefreshTokens invalidates every refresh token issued so far.
func (p *Provider) RevokeRefreshTokens() {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.refreshTokens)
}

// SetOmitIDTokenOnRefresh makes refresh responses leave out the ID token,
// which OIDC Core §12.2 allows.
func (p *Provider) SetOmitIDTokenOnRefresh(omit bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.omitIDTokenOnRefresh = omit
}

// SetClaimOverrides replaces claims in every ID token issued from now on; a
// nil value removes the claim. Use it to issue tokens with a wrong aud, iss
// or nonce.
func (p *Provider) SetClaimOverrides(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claimOverrides = maps.Clone(claims)
}

// Counters returns the request counts so far.
func (p *Provider) Counters() Counters {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counters
}

// LastAuthorizeParams returns the query of the last /authorize request.
func (p *Provider) LastAuthorizeParams() url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return maps.Clone(p.lastAuthorize)
}

// Handler serves the provider's endpoints.
func (p *Provider) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.opts.Logf != nil {
			p.opts.Logf("%s %s", r.Method, r.URL.Path)
		}
		switch path := r.URL.Path; {
		case strings.HasSuffix(path, "/.well-known/openid-configuration"):
			p.serveDiscovery(w, r)
		case path == "/jwks":
			p.serveJWKS(w)
		case path == "/authorize":
			p.serveAuthorize(w, r)
		case path == "/token":
			p.serveToken(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// tokenIssuer is the iss claim of issued tokens; callers hold p.mu.
func (p *Provider) tokenIssuer() string {
	if p.opts.EntraTenantID != "" {
		return p.base + "/" + p.opts.EntraTenantID + "/v2.0"
	}
	return p.base
}

func (p *Provider) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()

	prefix := strings.TrimSuffix(r.URL.Path, "/.well-known/openid-configuration")
	issuer := p.base
	if p.opts.EntraTenantID != "" {
		if !strings.HasSuffix(prefix, "/v2.0") {
			http.NotFound(w, r)
			return
		}
		issuer = p.base + "/{tenantid}/v2.0"
	} else if prefix != "" {
		http.NotFound(w, r)
		return
	}
	p.counters.Discovery++

	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                p.base + "/authorize",
		"token_endpoint":                        p.base + "/token",
		"jwks_uri":                              p.base + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
		"scopes_supported":                      []string{"openid", "email", "profile", "offline_access"},
	})
}

func (p *Provider) serveJWKS(w http.ResponseWriter) {
	pub := p.key.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]any{{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": p.kid,
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

func (p *Provider) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()

	q := r.URL.Query()
	p.counters.Authorize++
	p.lastAuthorize = q

	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme != "http" || !isLoopback(redirect.Hostname()) {
		http.Error(w, "redirect_uri must be an http loopback URL", http.StatusBadRequest)
		return
	}
	if q.Get("client_id") != p.opts.ClientID {
		http.Error(w, "unknown client_id", http.StatusBadRequest)
		return
	}

	fail := func(desc string) {
		v := redirect.Query()
		v.Set("error", "invalid_request")
		v.Set("error_description", desc)
		v.Set("state", q.Get("state"))
		redirect.RawQuery = v.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	}
	switch {
	case q.Get("response_type") != "code":
		fail("response_type must be code")
		return
	case !slices.Contains(strings.Fields(q.Get("scope")), "openid"):
		fail("scope must include openid")
		return
	case q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		fail("PKCE with S256 is required")
		return
	}

	code := rand.Text()
	p.codes[code] = &authCode{
		clientID:      p.opts.ClientID,
		scope:         q.Get("scope"),
		user:          p.opts.User,
		redirectURI:   q.Get("redirect_uri"),
		nonce:         q.Get("nonce"),
		codeChallenge: q.Get("code_challenge"),
		expiresAt:     time.Now().Add(time.Minute),
	}
	v := redirect.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	redirect.RawQuery = v.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *Provider) serveToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		tokenError(w, "invalid_request", err.Error())
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	clientID, secret := r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	if u, s, ok := r.BasicAuth(); ok {
		// RFC 6749 §2.3.1: basic auth credentials are form-urlencoded.
		clientID, _ = url.QueryUnescape(u)
		secret, _ = url.QueryUnescape(s)
	}
	if clientID != p.opts.ClientID || (p.opts.ClientSecret != "" && secret != p.opts.ClientSecret) {
		tokenError(w, "invalid_client", "unknown client or bad secret")
		return
	}

	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		p.counters.CodeExchange++
		code, ok := p.codes[r.PostForm.Get("code")]
		delete(p.codes, r.PostForm.Get("code"))
		switch {
		case !ok || time.Now().After(code.expiresAt):
			tokenError(w, "invalid_grant", "unknown or expired code")
		case code.redirectURI != r.PostForm.Get("redirect_uri"):
			tokenError(w, "invalid_grant", "redirect_uri mismatch")
		case s256(r.PostForm.Get("code_verifier")) != code.codeChallenge:
			tokenError(w, "invalid_grant", "PKCE verification failed")
		default:
			p.issue(w, code.session, code.nonce, "", true)
		}
	case "refresh_token":
		p.counters.Refresh++
		rt := r.PostForm.Get("refresh_token")
		sess, ok := p.refreshTokens[rt]
		switch {
		case p.failNextRefresh:
			p.failNextRefresh = false
			tokenError(w, "invalid_grant", "refresh failure injected by test")
		case !ok:
			tokenError(w, "invalid_grant", "unknown refresh token")
		default:
			if p.opts.RotateRefreshTokens {
				delete(p.refreshTokens, rt)
				rt = ""
			}
			p.issue(w, *sess, "", rt, !p.omitIDTokenOnRefresh)
		}
	default:
		tokenError(w, "unsupported_grant_type", r.PostForm.Get("grant_type"))
	}
}

// issue writes a token response. An empty refreshToken mints a new one.
// Callers hold p.mu.
func (p *Provider) issue(w http.ResponseWriter, sess session, nonce, refreshToken string, withIDToken bool) {
	if refreshToken == "" {
		refreshToken = rand.Text()
		p.refreshTokens[refreshToken] = &sess
	}
	resp := map[string]any{
		"access_token":  rand.Text(),
		"token_type":    "Bearer",
		"expires_in":    int(p.opts.TokenTTL.Seconds()),
		"refresh_token": refreshToken,
		"scope":         sess.scope,
	}
	if withIDToken {
		now := time.Now()
		claims := map[string]any{
			"iss":       p.tokenIssuer(),
			"sub":       sess.user.Subject,
			"aud":       sess.clientID,
			"iat":       now.Unix(),
			"exp":       now.Add(p.opts.TokenTTL).Unix(),
			"auth_time": now.Unix(),
		}
		if sess.user.Email != "" {
			claims["email"] = sess.user.Email
		}
		if nonce != "" {
			claims["nonce"] = nonce
		}
		if p.opts.EntraTenantID != "" {
			claims["tid"] = p.opts.EntraTenantID
		}
		maps.Copy(claims, sess.user.Extra)
		for k, v := range p.claimOverrides {
			if v == nil {
				delete(claims, k)
			} else {
				claims[k] = v
			}
		}
		resp["id_token"] = p.Sign(claims)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// Sign signs claims as an RS256 JWT with the provider's key.
func (p *Provider) Sign(claims map[string]any) string {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": p.kid})
	if err != nil {
		panic(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(fmt.Sprintf("oidctest: marshaling claims: %v", err))
	}
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, digest[:])
	if err != nil {
		panic(fmt.Sprintf("oidctest: signing: %v", err))
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func tokenError(w http.ResponseWriter, code, desc string) {
	status := http.StatusBadRequest
	if code == "invalid_client" {
		status = http.StatusUnauthorized
	}
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
