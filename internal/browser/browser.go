// Package browser opens a URL in the user's default browser.
package browser

import (
	"fmt"
	"os/exec"
)

// Open starts the platform's URL handler for url and returns without waiting
// for the browser.
func Open(url string) error {
	name, args := command(url)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", name, err)
	}
	// Reap the process in the background; xdg-open in particular can stay
	// around until the browser exits.
	go func() { _ = cmd.Wait() }()
	return nil
}
