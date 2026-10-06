package store

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeSecretTool returns a secretTool backed by a shell script that exits with
// code for every command, printing stdout and nothing on stderr.
func fakeSecretTool(t *testing.T, stdout string, code int) *secretTool {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret-tool")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + stdout + "'\nexit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return &secretTool{path: path, service: "test"}
}

func TestSecretToolExitCodes(t *testing.T) {
	if _, err := fakeSecretTool(t, "", 1).Load("k"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load with empty lookup exiting 1: %v, want ErrNotFound", err)
	}
	if _, err := fakeSecretTool(t, "", 2).Load("k"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Load exiting 2: %v, want a failure", err)
	}
	if err := fakeSecretTool(t, "", 1).Save("k", []byte("v")); err == nil {
		t.Error("Save exiting 1 without stderr reported success")
	}
	if err := fakeSecretTool(t, "", 2).Save("k", []byte("v")); err == nil {
		t.Error("Save exiting 2 without stderr reported success")
	}
	if err := fakeSecretTool(t, "", 1).Delete("k"); err == nil {
		t.Error("Delete exiting 1 without stderr reported success")
	}
	if err := fakeSecretTool(t, "", 0).Delete("k"); err != nil {
		t.Errorf("Delete exiting 0: %v", err)
	}
	if got, err := fakeSecretTool(t, "secret", 0).Load("k"); err != nil || string(got) != "secret" {
		t.Errorf("Load = %q, %v; want secret", got, err)
	}
}
