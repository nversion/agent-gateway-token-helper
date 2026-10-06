//go:build integration

// Package integration runs the built agw-token binary against the fake OIDC
// provider, playing the browser by following the URL it prints.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nversion/agent-gateway-token-helper/internal/config"
	"github.com/nversion/agent-gateway-token-helper/internal/login"
	"github.com/nversion/agent-gateway-token-helper/internal/oidctest"
	"github.com/nversion/agent-gateway-token-helper/internal/store"
)

var (
	binary string
	// binaryEntra is built with entra as the build-time default provider.
	binaryEntra string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agw-token-it-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	build := func(subdir string, args ...string) string {
		out := filepath.Join(dir, subdir, "agw-token"+exeSuffix())
		args = append(append([]string{"build", "-o", out}, args...), "github.com/nversion/agent-gateway-token-helper/cmd/agw-token")
		cmd := exec.Command("go", args...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "building agw-token:", err)
			os.Exit(1)
		}
		return out
	}
	binary = build("plain")
	binaryEntra = build("entra", "-ldflags", "-X main.defaultProvider=entra")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// installAs copies the agw-token binary into a fresh directory under name,
// returning its path.
func installAs(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name+exeSuffix())
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

type result struct {
	stdout, stderr string
	code           int
	// logins counts how often the binary asked for a browser sign-in.
	logins int
}

// cli is one isolated agw-token installation: its own AGW_HOME and base flags.
type cli struct {
	t    *testing.T
	home string
	args []string
	// bin overrides the binary to run.
	bin string
}

func newCLI(t *testing.T, p *oidctest.Provider, extra ...string) *cli {
	t.Helper()
	args := []string{"--issuer", p.Issuer(), "--client-id", p.ClientID()}
	return &cli{t: t, home: t.TempDir(), args: append(args, extra...)}
}

func (c *cli) start(cmdArgs ...string) func() result {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	args := append(cmdArgs, c.args...)
	args = append(args, "--no-browser", "--store", "file", "--login-timeout", "20s")
	bin := binary
	if c.bin != "" {
		bin = c.bin
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(cleanEnv(), "AGW_HOME="+c.home)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		c.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		c.t.Fatal(err)
	}

	var stderr bytes.Buffer
	logins := 0
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		sc := bufio.NewScanner(stderrPipe)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			line := sc.Text()
			stderr.WriteString(line + "\n")
			if u, ok := strings.CutPrefix(line, login.URLPrefix); ok {
				logins++
				go playBrowser(u)
			}
		}
		_, _ = io.Copy(io.Discard, stderrPipe)
	}()

	return func() result {
		defer cancel()
		<-scanned
		err := cmd.Wait()
		r := result{stdout: stdout.String(), stderr: stderr.String(), logins: logins}
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			r.code = exitErr.ExitCode()
		} else if err != nil {
			c.t.Fatalf("running agw-token: %v", err)
		}
		return r
	}
}

func (c *cli) run(cmdArgs ...string) result {
	c.t.Helper()
	return c.start(cmdArgs...)()
}

// mustToken runs agw-token and checks it printed exactly one token.
func (c *cli) mustToken(cmdArgs ...string) (string, result) {
	c.t.Helper()
	r := c.run(cmdArgs...)
	if r.code != 0 {
		c.t.Fatalf("agw-token exited %d\nstderr:\n%s", r.code, r.stderr)
	}
	tok := strings.TrimSuffix(r.stdout, "\n")
	if strings.Count(tok, ".") != 2 || strings.ContainsAny(tok, " \n") {
		c.t.Fatalf("stdout is not exactly one JWT: %q", r.stdout)
	}
	return tok, r
}

func (c *cli) stored() *store.Entry {
	c.t.Helper()
	files, err := filepath.Glob(filepath.Join(c.home, "tokens", "*.json"))
	if err != nil || len(files) != 1 {
		c.t.Fatalf("want one stored token file, got %v (%v)", files, err)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		c.t.Fatal(err)
	}
	var e store.Entry
	if err := json.Unmarshal(data, &e); err != nil {
		c.t.Fatal(err)
	}
	return &e
}

// playBrowser follows the authorization URL; the fake provider redirects
// straight to agw-token's loopback callback.
func playBrowser(u string) {
	resp, err := http.Get(u)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "AGW_") {
			env = append(env, kv)
		}
	}
	return env
}

func TestFirstRunThenCached(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	c := newCLI(t, p)

	tok, r := c.mustToken()
	if r.logins != 1 {
		t.Fatalf("first run: %d logins, want 1", r.logins)
	}
	e := c.stored()
	if e.IDToken != tok || e.RefreshToken == "" || e.Subject != "alice" {
		t.Fatalf("stored entry %+v", e)
	}

	before := p.Counters()
	tok2, r := c.mustToken()
	if tok2 != tok || r.logins != 0 {
		t.Fatalf("second run: same token %v, logins %d", tok2 == tok, r.logins)
	}
	if after := p.Counters(); after.CodeExchange != before.CodeExchange || after.Refresh != before.Refresh {
		t.Fatalf("second run hit the token endpoint: %+v -> %+v", before, after)
	}
}

func TestRefreshNearExpiry(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{TokenTTL: time.Minute, RotateRefreshTokens: true})
	c := newCLI(t, p, "--min-valid", "5m")

	first, _ := c.mustToken()
	firstRT := c.stored().RefreshToken
	second, r := c.mustToken()
	if r.logins != 0 {
		t.Fatalf("refresh run asked for a login:\n%s", r.stderr)
	}
	if cnt := p.Counters(); cnt.Refresh != 1 {
		t.Fatalf("refresh count %d, want 1", cnt.Refresh)
	}
	e := c.stored()
	if second == first || e.IDToken != second {
		t.Fatal("refreshed token not returned and stored")
	}
	if e.RefreshToken == firstRT {
		t.Fatal("rotated refresh token not persisted")
	}
	if e.Subject != "alice" {
		t.Fatalf("subject changed to %q", e.Subject)
	}
}

func TestRefreshFailureFallsBackToBrowser(t *testing.T) {
	for _, tt := range []struct {
		name    string
		breakIt func(*oidctest.Provider)
	}{
		{"revoked", (*oidctest.Provider).RevokeRefreshTokens},
		{"no id_token on refresh", func(p *oidctest.Provider) { p.SetOmitIDTokenOnRefresh(true) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := oidctest.Start(t, oidctest.Options{TokenTTL: time.Minute})
			c := newCLI(t, p, "--min-valid", "5m")
			c.mustToken()
			tt.breakIt(p)
			_, r := c.mustToken()
			if r.logins != 1 || !strings.Contains(r.stderr, "signing in again") {
				t.Fatalf("expected a browser login after the failed refresh; logins %d\n%s", r.logins, r.stderr)
			}
		})
	}
}

func TestConcurrentInvocationsLoginOnce(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	c := newCLI(t, p)

	var wg sync.WaitGroup
	results := make([]result, 3)
	for i := range results {
		wait := c.start()
		wg.Go(func() { results[i] = wait() })
	}
	wg.Wait()

	if cnt := p.Counters(); cnt.Authorize != 1 {
		t.Fatalf("%d authorizations, want 1", cnt.Authorize)
	}
	for i, r := range results {
		if r.code != 0 {
			t.Fatalf("invocation %d exited %d:\n%s", i, r.code, r.stderr)
		}
		if r.stdout != results[0].stdout {
			t.Fatalf("invocation %d printed a different token", i)
		}
	}
}

func TestLogoutAndStatus(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	c := newCLI(t, p)
	tok, _ := c.mustToken()

	r := c.run("status")
	if r.code != 0 || !strings.Contains(r.stdout, "alice") || strings.Contains(r.stdout, tok) {
		t.Fatalf("status exited %d, stdout:\n%s", r.code, r.stdout)
	}

	if r := c.run("logout"); r.code != 0 {
		t.Fatalf("logout exited %d:\n%s", r.code, r.stderr)
	}
	r = c.run("status", "--output", "json")
	var info map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &info); err != nil {
		t.Fatalf("status json: %v\n%s", err, r.stdout)
	}
	if r.code != 1 || info["logged_in"] != false {
		t.Fatalf("status after logout exited %d: %v", r.code, info)
	}
}

func TestForceLogin(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	c := newCLI(t, p)
	c.mustToken()
	if _, r := c.mustToken("login"); r.logins != 1 {
		t.Fatalf("login command: %d logins, want 1", r.logins)
	}
}

func TestJSONOutput(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	c := newCLI(t, p, "--output", "json")
	r := c.run()
	var out struct {
		IDToken   string    `json:"id_token"`
		ExpiresAt time.Time `json:"expires_at"`
		Subject   string    `json:"subject"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("json output: %v\n%s", err, r.stdout)
	}
	if out.IDToken == "" || out.Subject != "alice" || time.Until(out.ExpiresAt) < 30*time.Minute {
		t.Fatalf("unexpected json output %+v", out)
	}
}

func TestHeadersOutput(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	c := newCLI(t, p)
	tok, _ := c.mustToken()
	r := c.run("--output", "headers")
	var headers map[string]string
	if err := json.Unmarshal([]byte(r.stdout), &headers); err != nil {
		t.Fatalf("headers output: %v\n%s", err, r.stdout)
	}
	if len(headers) != 1 || headers["Authorization"] != "Bearer "+tok {
		t.Fatalf("headers %v, want only Authorization: Bearer <cached token>", headers)
	}
}

func TestDecodedOutput(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	c := newCLI(t, p)
	c.mustToken()
	r := c.run("--output", "decoded")
	var out struct {
		Header  map[string]any `json:"header"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
		t.Fatalf("decoded output: %v\n%s", err, r.stdout)
	}
	if out.Header["alg"] != "RS256" || out.Payload["sub"] != "alice" || out.Payload["aud"] != p.ClientID() {
		t.Fatalf("decoded output %+v", out)
	}
}

func TestRejectsInvalidIDToken(t *testing.T) {
	for _, override := range []map[string]any{
		{"aud": "someone-else"},
		{"iss": "http://127.0.0.1:1"},
		{"nonce": "replayed"},
		{"exp": time.Now().Add(-time.Hour).Unix()},
	} {
		t.Run(fmt.Sprint(override), func(t *testing.T) {
			p := oidctest.Start(t, oidctest.Options{})
			p.SetClaimOverrides(override)
			r := newCLI(t, p).run()
			if r.code == 0 || r.stdout != "" {
				t.Fatalf("exit %d, stdout %q; want failure with empty stdout", r.code, r.stdout)
			}
		})
	}
}

func TestConfigErrorsExit2(t *testing.T) {
	c := &cli{t: t, home: t.TempDir()}
	r := c.run()
	if r.code != 2 || !strings.Contains(r.stderr, "--issuer") {
		t.Fatalf("exit %d, stderr:\n%s", r.code, r.stderr)
	}
}

func TestEntraPreset(t *testing.T) {
	const tenantID = "72f988bf-0000-0000-0000-000000000000"
	p := oidctest.Start(t, oidctest.Options{
		ClientID:      config.Presets["entra"]["client-id"],
		EntraTenantID: tenantID,
	})
	// Only the issuer is pointed at the fake; client ID, scopes and redirect
	// come from the preset.
	c := &cli{t: t, home: t.TempDir(), args: []string{"--provider", "entra", "--issuer", p.Issuer()}}
	c.mustToken()

	e := c.stored()
	if want := p.BaseURL() + "/" + tenantID + "/v2.0"; e.Issuer != want {
		t.Fatalf("issuer %q, want %q", e.Issuer, want)
	}
	q := p.LastAuthorizeParams()
	ru, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	if ru.Hostname() != "localhost" || ru.Path != "" {
		t.Fatalf("redirect_uri %s, want http://localhost:<port> like MSAL", ru)
	}
	if !strings.Contains(q.Get("scope"), "offline_access") {
		t.Fatalf("scope %q lacks offline_access", q.Get("scope"))
	}
	if q.Get("prompt") != "select_account" {
		t.Fatalf("prompt %q, want select_account like az login", q.Get("prompt"))
	}
}

func TestGooglePreset(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{
		ClientID:     config.Presets["google"]["client-id"],
		ClientSecret: config.Presets["google"]["client-secret"],
	})
	c := &cli{t: t, home: t.TempDir(), args: []string{"--provider", "google", "--issuer", p.Issuer()}}
	c.mustToken()

	q := p.LastAuthorizeParams()
	if q.Get("access_type") != "offline" || q.Get("prompt") != "select_account consent" {
		t.Fatalf("authorize params %v lack access_type=offline&prompt=select_account+consent", q)
	}
	if strings.Contains(q.Get("scope"), "offline_access") {
		t.Fatalf("scope %q must not contain offline_access for Google", q.Get("scope"))
	}
	ru, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(ru.Port())
	if ru.Path != "/" || port < 8085 || port > 8184 {
		t.Fatalf("redirect_uri %s, want http://localhost:<8085-8184>/ like gcloud", ru)
	}
}

// statusProvider runs status and returns the provider it reports. Status
// exits 1 when nobody is signed in, which is fine here.
func (c *cli) statusProvider() string {
	c.t.Helper()
	r := c.run("status", "--output", "json")
	var info struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &info); err != nil {
		c.t.Fatalf("status exited %d, stdout %q, stderr:\n%s", r.code, r.stdout, r.stderr)
	}
	return info.Provider
}

func TestProviderFromExecutableName(t *testing.T) {
	bin := installAs(t, "agw-token-google")
	c := &cli{t: t, home: t.TempDir(), bin: bin}
	if got := c.statusProvider(); got != "google" {
		t.Fatalf("provider %q, want google from the name", got)
	}

	// A profile named after the binary, next to it, wins over the preset.
	writeConfig(t, filepath.Join(filepath.Dir(bin), config.FileName), `{"profiles": {"google": {"provider": "entra"}}}`)
	if got := c.statusProvider(); got != "entra" {
		t.Fatalf("provider %q, want entra from the profile", got)
	}

	// And the command line wins over both.
	c.args = []string{"--provider", "generic", "--issuer", "https://idp.example", "--client-id", "x"}
	if got := c.statusProvider(); got != "generic" {
		t.Fatalf("provider %q, want generic from the flag", got)
	}
}

func TestBuildDefaultProvider(t *testing.T) {
	c := &cli{t: t, home: t.TempDir(), bin: binaryEntra}
	if got := c.statusProvider(); got != "entra" {
		t.Fatalf("provider %q, want the build-time entra", got)
	}
	c.args = []string{"--provider", "google"}
	if got := c.statusProvider(); got != "google" {
		t.Fatalf("provider %q, want google from the flag", got)
	}
	r := c.run("version")
	if !strings.Contains(r.stdout, "default provider entra") {
		t.Fatalf("version %q does not mention the default provider", r.stdout)
	}
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNoAccountPicker(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{ClientID: config.Presets["entra"]["client-id"], EntraTenantID: "t1"})
	c := &cli{t: t, home: t.TempDir(), args: []string{"--provider", "entra", "--issuer", p.Issuer(), "--no-account-picker"}}
	c.mustToken()
	if q := p.LastAuthorizeParams(); q.Has("prompt") || q.Has("login_hint") {
		t.Fatalf("authorize params %v, want no prompt", q)
	}
}
