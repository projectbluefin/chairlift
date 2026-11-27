// Package videoplay opens media in the user's default desktop handler.
//
// The setup wizard uses it to play its introduction video in the user's own
// player while the assistant keeps running. Like the URL opener in
// internal/views, this is a read-only desktop hand-off rather than a mutation,
// so it is not gated on dry-run mode: the launch is an aside that must never
// block or fail the surrounding flow.
package videoplay

import (
	"errors"
	"os/exec"

	"github.com/projectbluefin/chairlift/internal/launcher"
)

// ErrNoHandler reports that neither gio nor xdg-open is available, so the
// media cannot be handed to the desktop's default URI handler.
var ErrNoHandler = errors.New("videoplay: no default URI handler (gio or xdg-open)")

// Subprocess seams, overridable in tests like firstrun's runCommand: a test
// replaces the one piece it needs rather than guessing which seam a regression
// used.
var (
	lookPath = exec.LookPath
	newCmd   = exec.Command
)

// Launch opens uri in the default handler and returns once the process has
// started. The player's later unsuccessful exit is reported through onExit,
// which runs on the launcher's wait goroutine — UI callers marshal to their
// main thread before touching widgets. A process-start failure returns
// synchronously from Launch and does not call onExit.
func Launch(uri string, onExit func(error)) error {
	name, args := resolve(uri)
	if name == "" {
		return ErrNoHandler
	}
	return launcher.Start(newCmd(name, args...), onExit)
}

// resolve picks the default-handler invocation for uri. It prefers gio, the
// GIO scheme-aware dispatcher guaranteed on the GNOME/Bluefin surface, and
// falls back to xdg-open.
func resolve(uri string) (name string, args []string) {
	if _, err := lookPath("gio"); err == nil {
		return "gio", []string{"open", uri}
	}
	if _, err := lookPath("xdg-open"); err == nil {
		return "xdg-open", []string{uri}
	}
	return "", nil
}
