package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newServer(t *testing.T, doc func(base string) map[string]any) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		_ = json.NewEncoder(w).Encode(doc(srv.URL))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func goodDoc(base string) map[string]any {
	return map[string]any{
		"issuer":                           base,
		"authorization_endpoint":           base + "/authorize",
		"token_endpoint":                   base + "/token",
		"response_types_supported":         []string{"code"},
		"code_challenge_methods_supported": []string{"S256"},
	}
}

func TestGetCaches(t *testing.T) {
	srv, hits := newServer(t, goodDoc)
	dir := t.TempDir()
	now := time.Now()
	ctx := context.Background()

	for range 2 {
		doc, err := Get(ctx, srv.Client(), srv.URL, dir, now)
		if err != nil {
			t.Fatal(err)
		}
		if doc.TokenEndpoint != srv.URL+"/token" {
			t.Fatalf("token endpoint = %q", doc.TokenEndpoint)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("fetched %d times, want 1", got)
	}

	if _, err := Get(ctx, srv.Client(), srv.URL, dir, now.Add(CacheTTL+time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("fetched %d times after expiry, want 2", got)
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(base string, d map[string]any)
		want   string
	}{
		{"other host issuer", func(_ string, d map[string]any) { d["issuer"] = "http://127.0.0.2:1" }, "does not match"},
		{"no pkce", func(_ string, d map[string]any) { d["code_challenge_methods_supported"] = []string{"plain"} }, "PKCE"},
		{"no code flow", func(_ string, d map[string]any) { d["response_types_supported"] = []string{"id_token"} }, "authorization code"},
		{"missing token endpoint", func(_ string, d map[string]any) { delete(d, "token_endpoint") }, "token_endpoint"},
		{"insecure endpoint", func(_ string, d map[string]any) { d["token_endpoint"] = "http://evil.example/token" }, "https"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := newServer(t, func(base string) map[string]any {
				d := goodDoc(base)
				tt.mutate(base, d)
				return d
			})
			_, err := Get(context.Background(), srv.Client(), srv.URL, "", time.Now())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestEntraTenantTemplateAccepted(t *testing.T) {
	srv, _ := newServer(t, func(base string) map[string]any {
		d := goodDoc(base)
		d["issuer"] = base + "/{tenantid}/v2.0"
		return d
	})
	doc, err := Get(context.Background(), srv.Client(), srv.URL+"/organizations/v2.0", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(doc.Issuer, "/{tenantid}/v2.0") {
		t.Fatalf("issuer = %q", doc.Issuer)
	}
}
