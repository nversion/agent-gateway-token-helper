// Package login runs the OAuth 2.0 authorization code flow with PKCE for a
// native app (RFC 8252): it listens on a loopback port, sends the user's
// browser to the provider and exchanges the code that comes back.
package login

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// URLPrefix starts the line that shows the authorization URL. Tests look for
// it to play the browser.
const URLPrefix = "Open this URL: "

// Params configures a login.
type Params struct {
	AuthURL      string
	TokenURL     string
	AuthStyle    oauth2.AuthStyle
	ClientID     string
	ClientSecret string
	Scopes       []string
	// AuthParams are added to the authorization request.
	AuthParams map[string]string

	// RedirectHost is localhost, 127.0.0.1 or [::1].
	RedirectHost string
	// RedirectPort 0 picks a free port.
	RedirectPort int
	// PortSearch is how many ports from RedirectPort to try.
	PortSearch int
	// CallbackPath is the redirect URI's path; empty means none, as MSAL
	// sends it.
	CallbackPath string

	// OpenBrowser opens the authorization URL; nil only prints it.
	OpenBrowser func(url string) error
	// Out receives instructions for the user (stderr).
	Out     io.Writer
	Timeout time.Duration
}

// Result is the outcome of a successful login.
type Result struct {
	Token *oauth2.Token
	// Nonce is the nonce sent in the authorization request, which the ID
	// token must echo.
	Nonce string
}

type callback struct {
	code string
	err  error
}

// Run performs the login. ctx may carry an *http.Client under
// oauth2.HTTPClient for the token request.
func Run(ctx context.Context, p Params) (*Result, error) {
	listeners, port, err := listen(p.RedirectHost, p.RedirectPort, p.PortSearch)
	if err != nil {
		return nil, err
	}

	conf := &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     oauth2.Endpoint{AuthURL: p.AuthURL, TokenURL: p.TokenURL, AuthStyle: p.AuthStyle},
		RedirectURL:  "http://" + p.RedirectHost + ":" + strconv.Itoa(port) + p.CallbackPath,
		Scopes:       p.Scopes,
	}
	state, nonce, verifier := rand.Text(), rand.Text(), oauth2.GenerateVerifier()
	opts := []oauth2.AuthCodeOption{
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	}
	for k, v := range p.AuthParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}
	authURL := conf.AuthCodeURL(state, opts...)

	results := make(chan callback, 1)
	var once sync.Once
	path := p.CallbackPath
	if path == "" {
		path = "/"
	}
	// Match the path literally rather than via ServeMux, which would treat
	// it as a routing pattern.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("state") != state {
			// Not our redirect (or a forged one); keep waiting.
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		var cb callback
		if e := q.Get("error"); e != "" {
			cb.err = fmt.Errorf("authorization failed: %s: %s", e, q.Get("error_description"))
		} else if cb.code = q.Get("code"); cb.code == "" {
			cb.err = errors.New("authorization response has no code")
		}
		writePage(w, cb.err)
		once.Do(func() { results <- cb })
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	for _, l := range listeners {
		go func() { _ = srv.Serve(l) }()
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	if p.OpenBrowser != nil {
		_, _ = fmt.Fprintln(p.Out, "Opening your browser to sign in. If it doesn't open, use the URL below.")
		if err := p.OpenBrowser(authURL); err != nil {
			_, _ = fmt.Fprintf(p.Out, "Could not open a browser: %v\n", err)
		}
	}
	_, _ = fmt.Fprintln(p.Out, URLPrefix+authURL)

	waitCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	var cb callback
	select {
	case cb = <-results:
	case <-waitCtx.Done():
		return nil, fmt.Errorf("waiting for the browser sign-in: %w", waitCtx.Err())
	}
	if cb.err != nil {
		return nil, cb.err
	}

	tok, err := conf.Exchange(ctx, cb.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("exchanging authorization code: %w", err)
	}
	return &Result{Token: tok, Nonce: nonce}, nil
}

// listen binds the redirect listener(s). For "localhost" it binds both
// 127.0.0.1 and [::1] on the same port, since browsers may resolve localhost
// to either.
func listen(host string, port, search int) ([]net.Listener, int, error) {
	var lastErr error
	for i := range max(search, 1) {
		try := port
		if port != 0 {
			try = port + i
		}
		ls, actual, err := listenOn(host, try)
		if err == nil {
			return ls, actual, nil
		}
		lastErr = err
	}
	if search > 1 {
		return nil, 0, fmt.Errorf("no free port for the login redirect in %d-%d: %w", port, port+search-1, lastErr)
	}
	return nil, 0, fmt.Errorf("listening for the login redirect: %w", lastErr)
}

func listenOn(host string, port int) ([]net.Listener, int, error) {
	switch host {
	case "127.0.0.1":
		l, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return nil, 0, err
		}
		return []net.Listener{l}, l.Addr().(*net.TCPAddr).Port, nil
	case "[::1]":
		l, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(port)))
		if err != nil {
			return nil, 0, err
		}
		return []net.Listener{l}, l.Addr().(*net.TCPAddr).Port, nil
	case "localhost":
		l4, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			return nil, 0, err
		}
		actual := l4.Addr().(*net.TCPAddr).Port
		l6, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(actual)))
		if err == nil {
			return []net.Listener{l4, l6}, actual, nil
		}
		// No IPv6 loopback at all is fine; the port being taken on ::1 by
		// someone else is not, since the browser might end up there.
		if probe, perr := net.Listen("tcp6", "[::1]:0"); perr == nil {
			_ = probe.Close()
			_ = l4.Close()
			return nil, 0, fmt.Errorf("port %d is in use on [::1]: %w", actual, err)
		}
		return []net.Listener{l4}, actual, nil
	default:
		return nil, 0, fmt.Errorf("unsupported redirect host %q", host)
	}
}

func writePage(w http.ResponseWriter, err error) {
	title, msg := "Signed in", "You are signed in. You can close this tab and return to your application."
	status := http.StatusOK
	if err != nil {
		title, msg, status = "Sign-in failed", err.Error(), http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html><head><meta charset="utf-8"><title>agw-token: %[1]s</title>
<style>body{font-family:system-ui,sans-serif;max-width:32rem;margin:4rem auto;padding:0 1rem;color:#222}
@media(prefers-color-scheme:dark){body{background:#111;color:#ddd}}</style></head>
<body><h1>%[1]s</h1><p>%[2]s</p></body></html>
`, html.EscapeString(title), html.EscapeString(msg))
}
