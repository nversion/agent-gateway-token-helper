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
	return loadSrc(t, home, Sources{Flags: flags}, env)
}

// loadSrc is load with the executable and build-time fields of src set.
func loadSrc(t *testing.T, home string, src Sources, env map[string]string) (*Config, error) {
	t.Helper()
	src.Getenv = func(k string) string {
		if k == "AGW_HOME" {
			return home
		}
		return env[k]
	}
	return Load(src)
}

func TestPrecedence(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, FileName), `{"profiles": {
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

func TestExeNames(t *testing.T) {
	tests := []struct {
		exe  string
		want []string
	}{
		{"agw-token", nil},
		{"agw-token.exe", nil},
		{"agw-token-", nil},
		{"other-google", nil},
		{"agw-token-google", []string{"google"}},
		{"AGW-TOKEN-Google.EXE", []string{"google"}},
		{"agw-token-google-darwin-arm64", []string{"google-darwin-arm64", "google"}},
		{"agw-token-windows-amd64.exe", []string{"windows-amd64", "windows"}},
	}
	for _, tt := range tests {
		if got := exeNames(tt.exe); !slices.Equal(got, tt.want) {
			t.Errorf("exeNames(%q) = %q, want %q", tt.exe, got, tt.want)
		}
	}
}

func TestExeNameSelectsProviderOrProfile(t *testing.T) {
	home := t.TempDir()
	exe := func(name string) Sources { return Sources{ExeName: name} }

	c, err := loadSrc(t, home, exe("agw-token-google"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "google" || c.Profile != "default" {
		t.Errorf("provider %q profile %q", c.Provider, c.Profile)
	}

	// A release build's name keeps working.
	c, err = loadSrc(t, home, exe("agw-token-entra-windows-amd64.exe"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "entra" {
		t.Errorf("provider %q", c.Provider)
	}

	// Names that are neither a profile nor a provider are ignored.
	if _, err := loadSrc(t, home, exe("agw-token-linux-amd64"), nil); err == nil || !strings.Contains(err.Error(), "--issuer") {
		t.Errorf("error %v, want the generic provider's missing issuer", err)
	}

	writeFile(t, filepath.Join(home, FileName), `{"profiles": {
		"work": {"issuer": "https://work.example", "client-id": "work-client"},
		"google": {"provider": "entra"}
	}}`)

	// A profile of that name.
	c, err = loadSrc(t, home, exe("agw-token-work"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile != "work" || c.Issuer != "https://work.example" {
		t.Errorf("profile %q issuer %q", c.Profile, c.Issuer)
	}

	// A profile wins over the provider preset of the same name.
	c, err = loadSrc(t, home, exe("agw-token-google"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile != "google" || c.Provider != "entra" {
		t.Errorf("profile %q provider %q", c.Profile, c.Provider)
	}

	// --profile, AGW_PROFILE and --provider win over the name.
	c, err = loadSrc(t, home, Sources{ExeName: "agw-token-google", Flags: map[string]string{"profile": "work"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile != "work" {
		t.Errorf("profile %q", c.Profile)
	}
	c, err = loadSrc(t, home, exe("agw-token-google"), map[string]string{"AGW_PROFILE": "work"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile != "work" {
		t.Errorf("profile %q", c.Profile)
	}
	c, err = loadSrc(t, home, Sources{ExeName: "agw-token-entra", Flags: map[string]string{"provider": "google"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "google" {
		t.Errorf("provider %q", c.Provider)
	}
}

func TestExeNameBelowDefaultProfile(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, FileName), `{"profiles": {"default": {"provider": "entra"}}}`)
	c, err := loadSrc(t, home, Sources{ExeName: "agw-token-google"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "entra" {
		t.Errorf("provider %q, want the default profile's entra", c.Provider)
	}
}

func TestBuildDefaultProvider(t *testing.T) {
	home := t.TempDir()

	c, err := loadSrc(t, home, Sources{DefaultProvider: "entra"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "entra" {
		t.Errorf("provider %q", c.Provider)
	}

	// The executable name and flags win over it.
	c, err = loadSrc(t, home, Sources{DefaultProvider: "entra", ExeName: "agw-token-google"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "google" {
		t.Errorf("provider %q, want google from the name", c.Provider)
	}
	c, err = loadSrc(t, home, Sources{DefaultProvider: "entra", Flags: map[string]string{"provider": "google"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "google" {
		t.Errorf("provider %q, want google from the flag", c.Provider)
	}

	// So does a profile.
	writeFile(t, filepath.Join(home, FileName), `{"profiles": {"default": {"provider": "google"}}}`)
	c, err = loadSrc(t, home, Sources{DefaultProvider: "entra"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "google" {
		t.Errorf("provider %q, want google from the profile", c.Provider)
	}

	// A bad build fails even when something else picks the provider.
	_, err = loadSrc(t, home, Sources{DefaultProvider: "nope", Flags: map[string]string{"provider": "google"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "build-time default provider") {
		t.Errorf("error %v", err)
	}
}

func TestExeDirConfig(t *testing.T) {
	home, exeDir := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(home, FileName), `{"profiles": {
		"default": {"provider": "entra", "tenant": "from-home", "min-valid": "10m"},
		"home-only": {"provider": "google"}
	}}`)
	writeFile(t, filepath.Join(exeDir, FileName), `{"profiles": {
		"default": {"tenant": "from-exe"},
		"exe-only": {"provider": "google"}
	}}`)
	src := func(flags map[string]string) Sources { return Sources{ExeDir: exeDir, Flags: flags} }

	// Per key, the file next to the executable wins and the other fills in.
	c, err := loadSrc(t, home, src(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Provider != "entra" || c.Tenant != "from-exe" || c.MinValid != 10*time.Minute {
		t.Errorf("provider %q tenant %q min-valid %v", c.Provider, c.Tenant, c.MinValid)
	}
	want := []string{filepath.Join(exeDir, FileName), filepath.Join(home, FileName)}
	if !slices.Equal(c.ConfigFiles, want) {
		t.Errorf("config files %q, want %q", c.ConfigFiles, want)
	}

	// A named profile may live in either file.
	for _, p := range []string{"exe-only", "home-only"} {
		c, err := loadSrc(t, home, src(map[string]string{"profile": p}), nil)
		if err != nil {
			t.Fatal(err)
		}
		if c.Provider != "google" {
			t.Errorf("%s: provider %q", p, c.Provider)
		}
	}
	if _, err := loadSrc(t, home, src(map[string]string{"profile": "missing"}), nil); err == nil {
		t.Error("expected error for a profile in neither file")
	}

	// Only the executable's directory has a config file.
	c, err = loadSrc(t, t.TempDir(), src(map[string]string{"profile": "exe-only"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ConfigFiles) != 1 || c.Provider != "google" {
		t.Errorf("config files %q provider %q", c.ConfigFiles, c.Provider)
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

func TestExtraAuthParamsRejectManaged(t *testing.T) {
	home := t.TempDir()
	for _, kv := range []string{"state=fixed", "nonce=x", "code_challenge_method=plain", "redirect_uri=http://evil", "scope="} {
		_, err := load(t, home, map[string]string{"provider": "google", "extra-auth-params": "hd=example.com," + kv}, nil)
		if err == nil || !strings.Contains(err.Error(), "cannot set") {
			t.Errorf("%s: err %v, want cannot set", kv, err)
		}
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
	a.AuthParams = map[string]string{"audience": "a&resource=b"}
	b.AuthParams = map[string]string{"audience": "a", "resource": "b"}
	if a.Key() == b.Key() {
		t.Error("auth param values containing & must not collide with separate params")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
