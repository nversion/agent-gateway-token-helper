package login

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nversion/agent-gateway-token-helper/internal/oidctest"
)

func params(p *oidctest.Provider, out *bytes.Buffer) Params {
	return Params{
		AuthURL:      p.BaseURL() + "/authorize",
		TokenURL:     p.BaseURL() + "/token",
		ClientID:     p.ClientID(),
		Scopes:       []string{"openid", "email"},
		RedirectHost: "localhost",
		CallbackPath: "/callback",
		OpenBrowser: func(u string) error {
			go func() {
				resp, err := http.Get(u)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
			return nil
		},
		Out:     out,
		Timeout: 10 * time.Second,
	}
}

func TestRun(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	var out bytes.Buffer
	prm := params(p, &out)
	prm.AuthParams = map[string]string{"prompt": "consent"}

	res, err := Run(context.Background(), prm)
	if err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if res.Token.Extra("id_token") == nil || res.Token.RefreshToken == "" {
		t.Fatalf("missing tokens: %+v", res.Token)
	}
	q := p.LastAuthorizeParams()
	if q.Get("nonce") != res.Nonce || q.Get("prompt") != "consent" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorize params: %v", q)
	}
	if !strings.Contains(out.String(), URLPrefix) {
		t.Fatalf("output has no URL line: %q", out.String())
	}
}

func TestRunNoCallbackPath(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	var out bytes.Buffer
	prm := params(p, &out)
	prm.CallbackPath = ""
	if _, err := Run(context.Background(), prm); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ru, err := url.Parse(p.LastAuthorizeParams().Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	if ru.Path != "" || ru.Hostname() != "localhost" {
		t.Fatalf("redirect_uri = %s, want http://localhost:<port>", ru)
	}
}

func TestRunProviderError(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	var out bytes.Buffer
	prm := params(p, &out)
	prm.Scopes = []string{"email"} // the fake rejects requests without openid
	_, err := Run(context.Background(), prm)
	if err == nil || !strings.Contains(err.Error(), "openid") {
		t.Fatalf("error %v, want authorization error about openid", err)
	}
}

func TestRunTimeout(t *testing.T) {
	p := oidctest.Start(t, oidctest.Options{})
	var out bytes.Buffer
	prm := params(p, &out)
	prm.OpenBrowser = nil
	prm.Timeout = 50 * time.Millisecond
	if _, err := Run(context.Background(), prm); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("error %v, want timeout", err)
	}
}

func TestListenPortSearch(t *testing.T) {
	busy, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	port := busy.Addr().(*net.TCPAddr).Port

	ls, got, err := listen("127.0.0.1", port, 5)
	if err != nil {
		t.Skipf("no free port near %d: %v", port, err)
	}
	for _, l := range ls {
		_ = l.Close()
	}
	if got == port || got > port+4 {
		t.Fatalf("got port %d, want one in %d-%d", got, port+1, port+4)
	}

	if _, _, err := listen("127.0.0.1", port, 1); err == nil {
		t.Fatal("expected an error for a busy fixed port")
	}
}
