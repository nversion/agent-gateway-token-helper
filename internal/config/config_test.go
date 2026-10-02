package config

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func load(t *testing.T, home string, flags map[string]string, env map[string]string) (*Config, error) {
	t.Helper()
	return Load(Sources{
		Flags: flags,
		Getenv: func(k string) string {
			if k == "AGW_HOME" {
				return home
			}
			return env[k]
		},
	})
}

func TestPrecedence(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "config.json"), `{"profiles": {
		"default": {"provider": "entra", "tenant": "from-profile", "min-valid": "10m", "scopes": ["openid", "email"]},
		"work": {"issuer": "https://work.example", "client-id": "work-client"}
	}}`)

	// Profile over preset.
	c, err := load(t, home, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Issuer != "https://login.microsoftonline.com/from-profile/v2.0" {
		t.Errorf("issuer = %q", c.Issuer)
	}
	if c.ClientID != Presets["entra"]["client-id"] {
		t.Errorf("client-id = %q, want the entra preset", c.ClientID)
	}
	if c.MinValid != 10*time.Minute {
		t.Errorf("min-valid = %v", c.MinValid)
	}
	if !slices.Equal(c.Scopes, []string{"openid", "email"}) {
		t.Errorf("scopes = %q", c.Scopes)
	}

	// Env over profile, flag over env.
	c, err = load(t, home,
		map[string]string{"tenant": "from-flag"},
		map[string]string{"AGW_TENANT": "from-env", "AGW_MIN_VALID": "1m"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Tenant != "from-flag" || c.MinValid != time.Minute {
		t.Errorf("tenant = %q, min-valid = %v", c.Tenant, c.MinValid)
	}

	// An explicitly empty flag still overrides.
	c, err = load(t, home, map[string]string{"callback-path": ""}, map[string]string{"AGW_PROVIDER": "generic", "AGW_ISSUER": "https://x.example", "AGW_CLIENT_ID": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if c.CallbackPath != "" {
		t.Errorf("callback-path = %q", c.CallbackPath)
	}

	// Named profile.
	c, err = load(t, home, map[string]string{"profile": "work"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "generic" || c.Issuer != "https://work.example" || c.CallbackPath != "/callback" {
		t.Errorf("got %+v", c)
	}

	if _, err := load(t, home, map[string]string{"profile": "missing"}, nil); err == nil {
		t.Error("expected error for missing explicit profile")
	}
}

func TestPresets(t *testing.T) {
	home := t.TempDir()

	g, err := load(t, home, map[string]string{"provider": "google"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.RedirectPort != 8085 || g.PortSearch != portSearchRange || g.CallbackPath != "/" {
		t.Errorf("google redirect: port %d search %d path %q", g.RedirectPort, g.PortSearch, g.CallbackPath)
	}
	if g.AuthParams["access_type"] != "offline" || slices.Contains(g.Scopes, "offline_access") {
		t.Errorf("google params %v scopes %v", g.AuthParams, g.Scopes)
	}

	e, err := load(t, home, map[string]string{"provider": "entra"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.Issuer != "https://login.microsoftonline.com/organizations/v2.0" || e.CallbackPath != "" || e.RedirectPort != 0 {
		t.Errorf("entra: %+v", e)
	}
	if e.AuthParams["prompt"] != "select_account" {
		t.Errorf("entra params %v lack prompt=select_account", e.AuthParams)
	}
}

func TestExtraAuthParamsMergeWithPreset(t *testing.T) {
	home := t.TempDir()
	c, err := load(t, home, map[string]string{
		"provider":          "google",
		"extra-auth-params": "hd=example.com,prompt=consent",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"access_type": "offline", "prompt": "consent", "hd": "example.com"}
	if !maps.Equal(c.AuthParams, want) {
		t.Fatalf("params %v, want %v", c.AuthParams, want)
	}

	// An empty value removes a preset parameter.
	c, err = load(t, home, map[string]string{"provider": "entra", "extra-auth-params": "prompt="}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AuthParams) != 0 {
		t.Fatalf("params %v, want none", c.AuthParams)
	}
}

func TestNoAccountPicker(t *testing.T) {
	home := t.TempDir()
	tests := []struct {
		provider string
		want     map[string]string
	}{
		{"google", map[string]string{"access_type": "offline", "prompt": "consent"}},
		{"entra", map[string]string{}},
	}
	for _, tt := range tests {
		c, err := load(t, home, map[string]string{"provider": tt.provider, "no-account-picker": "true"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !maps.Equal(c.AuthParams, tt.want) {
			t.Errorf("%s: params %v, want %v", tt.provider, c.AuthParams, tt.want)
		}
	}

	// The picker is UI only, so it must not change the token cache key.
	with, err := load(t, home, map[string]string{"provider": "entra"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	without, err := load(t, home, map[string]string{"provider": "entra", "extra-auth-params": "prompt="}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if with.Key() != without.Key() {
		t.Error("prompt changed the cache key")
	}
}

func TestValidationErrors(t *testing.T) {
	home := t.TempDir()
	base := map[string]string{"issuer": "https://issuer.example", "client-id": "c"}
	tests := []struct {
		name  string
		flags map[string]string
		want  string
	}{
		{"no issuer", map[string]string{"client-id": "c"}, "--issuer"},
		{"no client", map[string]string{"issuer": "https://issuer.example"}, "--client-id"},
		{"http issuer", map[string]string{"issuer": "http://issuer.example"}, "https"},
		{"unknown provider", map[string]string{"provider": "nope"}, "unknown provider"},
		{"no openid", map[string]string{"scopes": "email"}, "openid"},
		{"bad port", map[string]string{"redirect-port": "x"}, "redirect-port"},
		{"bad host", map[string]string{"redirect-host": "0.0.0.0"}, "redirect-host"},
		{"bad params", map[string]string{"extra-auth-params": "novalue"}, "key=value"},
		{"bad store", map[string]string{"store": "s3"}, "store"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags := map[string]string{}
			if tt.name != "no issuer" && tt.name != "no client" {
				maps.Copy(flags, base)
			}
			maps.Copy(flags, tt.flags)
			_, err := load(t, home, flags, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want one containing %q", err, tt.want)
			}
		})
	}
}

func TestLoopbackHTTPIssuerAllowed(t *testing.T) {
	for _, issuer := range []string{"http://127.0.0.1:9999", "http://localhost:1", "http://[::1]:2/x"} {
		if err := CheckIssuerURL(issuer); err != nil {
			t.Errorf("%s: %v", issuer, err)
		}
	}
}

func TestKeyChangesWithConfig(t *testing.T) {
	a := &Config{Issuer: "https://i", ClientID: "c", Scopes: []string{"openid", "email"}}
	b := &Config{Issuer: "https://i", ClientID: "c", Scopes: []string{"email", "openid"}}
	if a.Key() != b.Key() {
		t.Error("scope order should not change the key")
	}
	b.AuthParams = map[string]string{"audience": "gateway"}
	if a.Key() == b.Key() {
		t.Error("auth params should change the key")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
