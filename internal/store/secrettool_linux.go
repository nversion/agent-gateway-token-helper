package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/nversion/agent-gateway-token-helper/internal/config"
)

const (
	// probeTimeout bounds the availability check; without a D-Bus session
	// secret-tool can hang trying to autolaunch one.
	probeTimeout = 5 * time.Second
	// opTimeout leaves room for an unlock prompt from the keyring.
	opTimeout = 2 * time.Minute
)

// platformBackend uses libsecret's secret-tool when a Secret Service (GNOME
// Keyring, KWallet, KeePassXC) answers, and a 0600 file otherwise, e.g. on
// headless machines.
func platformBackend(dir string) Backend {
	path, err := exec.LookPath("secret-tool")
	if err != nil {
		return newFileBackend(dir, "file (secret-tool not installed)", nil)
	}
	s := &secretTool{path: path, service: config.AppName}
	if err := s.probe(); err != nil {
		return newFileBackend(dir, "file (Secret Service unavailable: "+err.Error()+")", nil)
	}
	return s
}

// secretTool shells out to secret-tool. Secrets are passed on stdin and read
// from stdout, never on the command line.
type secretTool struct {
	path    string
	service string
}

func (s *secretTool) Name() string {
	return "Secret Service via secret-tool (service " + s.service + ")"
}

func (s *secretTool) run(timeout time.Duration, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.path, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("secret-tool %s timed out", args[0])
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return nil, fmt.Errorf("secret-tool %s: %s", args[0], msg)
	}
	if _, ok := errors.AsType[*exec.ExitError](err); err != nil && !ok {
		return nil, err
	}
	// secret-tool exits 1 without output for lookups that match nothing.
	return stdout.Bytes(), nil
}

func (s *secretTool) attrs(key string) []string {
	return []string{"service", s.service, "key", key}
}

func (s *secretTool) probe() error {
	_, err := s.run(probeTimeout, nil, append([]string{"lookup"}, s.attrs("probe")...)...)
	return err
}

func (s *secretTool) Load(key string) ([]byte, error) {
	out, err := s.run(opTimeout, nil, append([]string{"lookup"}, s.attrs(key)...)...)
	if err != nil {
		return nil, err
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

func (s *secretTool) Save(key string, data []byte) error {
	args := append([]string{"store", "--label", s.service + " token"}, s.attrs(key)...)
	_, err := s.run(opTimeout, data, args...)
	return err
}

func (s *secretTool) Delete(key string) error {
	_, err := s.run(opTimeout, nil, append([]string{"clear"}, s.attrs(key)...)...)
	return err
}
