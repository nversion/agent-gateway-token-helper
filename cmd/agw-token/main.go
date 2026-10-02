// Command agw-token prints an OIDC ID token for authenticating to an agent
// gateway, signing in through the browser when needed. It is meant to be
// called by AI harnesses, in the spirit of `gcloud auth print-identity-token`.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/nversion/agent-gateway-token-helper/internal/browser"
	"github.com/nversion/agent-gateway-token-helper/internal/config"
	"github.com/nversion/agent-gateway-token-helper/internal/idtoken"
	"github.com/nversion/agent-gateway-token-helper/internal/store"
	"github.com/nversion/agent-gateway-token-helper/internal/tokens"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = ""

var flagHelp = map[string]string{
	"provider":          "provider preset: generic, google (gcloud's client) or entra (Azure CLI's client) (default generic)",
	"issuer":            "OIDC issuer URL; may contain {tenant}",
	"tenant":            "Entra tenant ID or domain for {tenant} in the issuer (entra default organizations)",
	"client-id":         "OAuth client ID",
	"client-secret":     "OAuth client secret, for providers that require one for native apps",
	"scopes":            "space- or comma-separated scopes (default \"openid email profile offline_access\")",
	"extra-auth-params": "extra authorization request parameters, as key=value,key=value; added to the preset's, an empty value (prompt=) removes one",
	"min-valid":         "refresh when the token expires within this duration (default 5m)",
	"redirect-host":     "redirect URI host: localhost, 127.0.0.1 or [::1] (default localhost)",
	"redirect-port":     "redirect URI port; 0 picks a free one, N+ tries N and the next 99 (default 0)",
	"callback-path":     "redirect URI path (default /callback)",
	"no-browser":        "print the sign-in URL instead of opening a browser",
	"login-timeout":     "how long to wait for the browser sign-in (default 5m)",
	"store":             "token storage: auto (platform secret store) or file (default auto)",
	"output":            "token output: raw, json, headers ({\"Authorization\":\"Bearer ...\"} for MCP headersHelper) or decoded (header and payload as JSON, for debugging) (default raw)",
}

const usageText = `Usage: agw-token [command] [flags]

Prints an OIDC ID token on stdout, signing in through the browser on first
use and refreshing it afterwards.

Commands:
  token    print the ID token (default)
  login    sign in again, ignoring stored tokens, and print the ID token
  logout   delete the stored tokens
  status   show who is signed in and until when (never prints the token)
  version  print the version

Every flag can also be set with an AGW_* environment variable (AGW_CLIENT_ID
for --client-id) or in a profile in config.json in the config directory.
Precedence: flag, environment, profile, provider preset.

Flags:
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	cmd := "token"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "token", "login", "logout", "status":
	case "version":
		_, _ = fmt.Fprintln(stdout, buildVersion())
		return 0
	case "help":
		args = []string{"-h"}
	default:
		_, _ = fmt.Fprintf(stderr, "agw-token: unknown command %q\n", cmd)
		return 2
	}

	fs := flag.NewFlagSet("agw-token", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprint(stderr, usageText)
		fs.PrintDefaults()
	}
	fs.String("profile", "", "profile in config.json (default \"default\")")
	for _, k := range config.Keys {
		if k == "no-browser" {
			fs.Bool(k, false, flagHelp[k])
		} else {
			fs.String(k, "", flagHelp[k])
		}
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "agw-token: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	flags := map[string]string{}
	fs.Visit(func(f *flag.Flag) { flags[f.Name] = f.Value.String() })

	cfg, err := config.Load(config.Sources{Flags: flags, Getenv: getenv})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agw-token: %v\n", err)
		return 2
	}
	st, err := store.Open(cfg.Store, cfg.Dirs.State)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agw-token: %v\n", err)
		return 1
	}
	m := &tokens.Manager{Config: cfg, Store: st, OpenBrowser: browser.Open, Stderr: stderr}

	switch cmd {
	case "logout":
		err = m.Logout()
	case "status":
		return status(m, stdout, stderr)
	default:
		var e *store.Entry
		if e, err = m.Token(ctx, cmd == "login"); err == nil {
			err = printToken(stdout, cfg.Output, e)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "agw-token: %v\n", err)
		return 1
	}
	return 0
}

func printToken(w io.Writer, output string, e *store.Entry) error {
	switch output {
	case "decoded":
		header, payload, err := idtoken.Decode(e.IDToken)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]json.RawMessage{"header": header, "payload": payload})
	case "headers":
		return json.NewEncoder(w).Encode(map[string]string{"Authorization": "Bearer " + e.IDToken})
	case "json":
		return json.NewEncoder(w).Encode(map[string]any{
			"id_token":   e.IDToken,
			"expires_at": e.Expiry.UTC().Format(time.RFC3339),
			"issuer":     e.Issuer,
			"subject":    e.Subject,
			"email":      e.Email,
		})
	}
	_, err := fmt.Fprintln(w, e.IDToken)
	return err
}

func status(m *tokens.Manager, stdout, stderr io.Writer) int {
	cfg := m.Config
	e, err := m.Stored()
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		_, _ = fmt.Fprintf(stderr, "agw-token: %v\n", err)
		return 1
	}

	info := map[string]any{
		"profile":   cfg.Profile,
		"provider":  cfg.Provider,
		"issuer":    cfg.Issuer,
		"client_id": cfg.ClientID,
		"store":     m.Store.Name(),
		"logged_in": e != nil,
	}
	if e != nil {
		info["subject"] = e.Subject
		info["email"] = e.Email
		info["expires_at"] = e.Expiry.UTC().Format(time.RFC3339)
		info["expired"] = !e.Expiry.After(time.Now())
		info["has_refresh_token"] = e.RefreshToken != ""
	}

	if cfg.Output == "json" {
		_ = json.NewEncoder(stdout).Encode(info)
	} else {
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		row := func(k string, v any) { _, _ = fmt.Fprintf(tw, "%s:\t%v\n", k, v) }
		row("Profile", cfg.Profile)
		row("Provider", cfg.Provider)
		row("Issuer", cfg.Issuer)
		row("Client ID", cfg.ClientID)
		row("Store", m.Store.Name())
		if e == nil {
			row("Logged in", "no")
		} else {
			row("Logged in", "yes")
			row("Subject", e.Subject)
			if e.Email != "" {
				row("Email", e.Email)
			}
			left := time.Until(e.Expiry).Round(time.Second)
			if left > 0 {
				row("ID token expires", fmt.Sprintf("%s (in %s)", e.Expiry.Local().Format(time.RFC3339), left))
			} else {
				row("ID token expires", fmt.Sprintf("%s (expired)", e.Expiry.Local().Format(time.RFC3339)))
			}
			row("Refresh token", e.RefreshToken != "")
		}
		_ = tw.Flush()
	}
	if e == nil {
		return 1
	}
	return 0
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "dev"
}
