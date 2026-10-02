// Command fake-oidc runs the fake OIDC provider from internal/oidctest over
// plain HTTP on a loopback address, for trying agw-token or a harness
// integration without a real identity provider:
//
//	fake-oidc --addr 127.0.0.1:9999 &
//	agw-token --issuer http://127.0.0.1:9999 --client-id agw-test-client
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/nversion/agent-gateway-token-helper/internal/oidctest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9999", "loopback address to listen on")
	clientID := flag.String("client-id", oidctest.DefaultClientID, "accepted client ID")
	clientSecret := flag.String("client-secret", "", "required client secret (empty: public client)")
	subject := flag.String("subject", "alice", "sub claim of issued tokens")
	email := flag.String("email", "alice@example.com", "email claim of issued tokens")
	ttl := flag.Duration("ttl", time.Hour, "lifetime of issued tokens")
	rotate := flag.Bool("rotate-refresh-tokens", false, "issue a new refresh token on every refresh")
	entraTenant := flag.String("entra-tenant-id", "", "mimic Entra ID multi-tenant issuers with this tenant ID")
	flag.Parse()

	if err := run(*addr, oidctest.Options{
		ClientID:            *clientID,
		ClientSecret:        *clientSecret,
		User:                oidctest.User{Subject: *subject, Email: *email},
		TokenTTL:            *ttl,
		RotateRefreshTokens: *rotate,
		EntraTenantID:       *entraTenant,
		Logf:                log.Printf,
	}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "fake-oidc: %v\n", err)
		os.Exit(1)
	}
}

func run(addr string, opts oidctest.Options) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("refusing to listen on a non-loopback address: this provider signs tokens for anyone")
	}

	p, err := oidctest.New(opts)
	if err != nil {
		return err
	}
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	p.SetBaseURL("http://" + l.Addr().String())
	log.Printf("issuer: %s", p.Issuer())
	log.Printf("client id: %s", p.ClientID())
	srv := &http.Server{Handler: p.Handler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.Serve(l)
}
