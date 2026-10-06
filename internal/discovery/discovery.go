// Package discovery fetches and caches OpenID Provider metadata
// (/.well-known/openid-configuration).
package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nversion/agent-gateway-token-helper/internal/config"
	"github.com/nversion/agent-gateway-token-helper/internal/idtoken"
)

// CacheTTL is how long a fetched document is reused.
const CacheTTL = 24 * time.Hour

// maxDocumentSize bounds the response body read from the provider.
const maxDocumentSize = 1 << 20

// Document is the subset of the provider metadata this helper uses.
type Document struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	JWKSURI                           string   `json:"jwks_uri,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

type cacheEntry struct {
	FetchedAt time.Time `json:"fetched_at"`
	Document  Document  `json:"document"`
}

// Get returns the provider metadata for issuer, from cacheDir when a fresh
// copy is there. An empty cacheDir disables caching.
func Get(ctx context.Context, client *http.Client, issuer, cacheDir string, now time.Time) (*Document, error) {
	path := ""
	if cacheDir != "" {
		sum := sha256.Sum256([]byte(issuer))
		path = filepath.Join(cacheDir, "discovery-"+hex.EncodeToString(sum[:16])+".json")
		if doc := readCache(path, issuer, now); doc != nil {
			return doc, nil
		}
	}

	doc, err := fetch(ctx, client, issuer)
	if err != nil {
		return nil, err
	}
	if path != "" {
		// The cache is only an optimization; failing to write it is fine.
		_ = writeCache(path, cacheEntry{FetchedAt: now, Document: *doc})
	}
	return doc, nil
}

func fetch(ctx context.Context, client *http.Client, issuer string) (*Document, error) {
	wellKnown := issuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", wellKnown, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocumentSize))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", wellKnown, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", wellKnown, resp.Status)
	}
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", wellKnown, err)
	}
	if err := doc.validate(issuer); err != nil {
		return nil, fmt.Errorf("%s: %w", wellKnown, err)
	}
	return &doc, nil
}

func (d *Document) validate(issuer string) error {
	if !sameIssuer(issuer, d.Issuer) {
		return fmt.Errorf("issuer %q does not match configured issuer %q", d.Issuer, issuer)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" {
		return errors.New("authorization_endpoint and token_endpoint are required")
	}
	for _, ep := range []string{d.AuthorizationEndpoint, d.TokenEndpoint} {
		if err := config.CheckIssuerURL(ep); err != nil {
			return fmt.Errorf("endpoint %q: must be https (http only for loopback hosts)", ep)
		}
	}
	if len(d.ResponseTypesSupported) > 0 && !slices.Contains(d.ResponseTypesSupported, "code") {
		return errors.New("provider does not support the authorization code flow")
	}
	if len(d.CodeChallengeMethodsSupported) > 0 && !slices.Contains(d.CodeChallengeMethodsSupported, "S256") {
		return errors.New("provider does not support PKCE with S256")
	}
	return nil
}

// sameIssuer reports whether the discovered issuer belongs to the configured
// one. OIDC Discovery §4.3 requires an exact match; the only exception is
// Microsoft Entra ID (see entraAlias). The ID token's iss is checked against
// the discovered issuer later.
func sameIssuer(configured, discovered string) bool {
	discovered = strings.TrimRight(discovered, "/")
	return discovered == configured || entraAlias(configured, discovered)
}

// entraAlias reports whether discovered is what Microsoft Entra ID reports
// for the configured issuer: a tenant alias ("contoso.com", "consumers")
// resolves to the tenant GUID, and the multi-tenant endpoints ("common",
// "organizations") report the {tenantid} template. Both must have the form
// "<base>/<tenant>/v2.0" with the same base, and a configured tenant GUID
// must match exactly.
func entraAlias(configured, discovered string) bool {
	cBase, cTenant, ok := splitEntraIssuer(configured)
	if !ok || isGUID(cTenant) {
		return false
	}
	dBase, dTenant, ok := splitEntraIssuer(discovered)
	if !ok || dBase != cBase {
		return false
	}
	return dTenant == idtoken.TenantIDPlaceholder || isGUID(dTenant)
}

// splitEntraIssuer splits "<scheme>://<host>[/path]/<tenant>/v2.0" into its
// base and tenant.
func splitEntraIssuer(issuer string) (base, tenant string, ok bool) {
	rest, ok := strings.CutSuffix(issuer, "/v2.0")
	if !ok {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, '/')
	if i < 0 {
		return "", "", false
	}
	base, tenant = rest[:i], rest[i+1:]
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" || tenant == "" {
		return "", "", false
	}
	return base, tenant, true
}

// isGUID reports whether s has the 8-4-4-4-12 hex form of a GUID.
func isGUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return false
			}
		}
	}
	return true
}

func readCache(path, issuer string, now time.Time) *Document {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var e cacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil
	}
	if now.Sub(e.FetchedAt) > CacheTTL || now.Before(e.FetchedAt) || e.Document.validate(issuer) != nil {
		return nil
	}
	return &e.Document
}

func writeCache(path string, e cacheEntry) error {
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".discovery-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
