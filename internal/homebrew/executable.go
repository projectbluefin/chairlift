package homebrew

import (
	"os"
	"os/exec"
)

// linuxbrewExecutable is Homebrew's supported install location on Linux — the
// path data/chairlift-wrapper.sh:5-8 evaluates `brew shellenv` from before it
// exec's the application. A launch through that wrapper therefore finds `brew`
// on $PATH and a direct binary launch does not, even though the same Homebrew
// is installed. ChairLift resolves the path itself so the two launch routes
// agree (issue #207).
//
// It is a variable rather than a constant for the same reason lookPath is
// one: a test that observes the fallback executing points it at a temporary
// stand-in, and a test that observes "nothing resolves" points it at a path
// that does not exist. Production code never writes it.
var linuxbrewExecutable = "/home/linuxbrew/.linuxbrew/bin/brew"

// lookPath and statFile are the two non-blocking probes resolution is built
// from, and the injection seams that keep this file testable without
// installing Homebrew — the same role internal/troubleshoot's own lookPath
// seam plays for its detection. Tests replace them and restore them with
// t.Cleanup.
var (
	lookPath = exec.LookPath
	statFile = os.Stat
)

// FallbackExecutable returns Homebrew's supported install location on Linux:
// "/home/linuxbrew/.linuxbrew/bin/brew".
func FallbackExecutable() string {
	return linuxbrewExecutable
}

// ResolveExecutable resolves the Homebrew executable ChairLift should use: the
// `brew` $PATH resolves through lookPath, or the Linuxbrew fallback path when
// a regular file exists at that location, or "" when this host has no Homebrew.
func ResolveExecutable(lookPath func(string) (string, error), stat func(string) (os.FileInfo, error)) string {
	if path, err := lookPath("brew"); err == nil && path != "" {
		return path
	}

	// The wrapper tests the fallback with `[ -f "$BREW_PATH" ]`, and so does
	// this: a regular file is what the wrapper would have put on $PATH, so
	// presence here has to mean the same thing it means there.
	if info, err := stat(linuxbrewExecutable); err == nil && info.Mode().IsRegular() {
		return linuxbrewExecutable
	}

	return ""
}

// ExecutablePath returns the Homebrew executable ChairLift runs: the `brew`
// $PATH resolves, or the Linuxbrew install path when $PATH has no `brew`, or
// "" when this host has no Homebrew at all.
//
// It is the one resolution both halves of ChairLift read, which is what keeps
// them from diverging. Visibility — IsInstalled, and through it every view
// that hides or disables a Homebrew affordance — and execution —
// runBrewCommandCtx, and through it every brew command ChairLift issues — call
// this function, so a host whose Homebrew is reachable only at the fallback
// path is both reported as installed and actually driven, instead of one of
// the two. Order matters in the other direction as well: a `brew` on $PATH
// wins, so a user who customised their $PATH keeps the Homebrew they chose.
func ExecutablePath() string {
	return ResolveExecutable(lookPath, statFile)
}

// brewExecutable is ExecutablePath with the not-found case kept as the bare
// command name.
//
// Exec'ing the bare name rather than failing early preserves the existing
// classification for a host with no Homebrew: a name missing from $PATH is
// exec.ErrNotFound, which runBrewCommandAt turns into *NotFoundError and its
// "Please install Homebrew first" message. There is no path to run in that
// case, so nothing here can diverge from what ExecutablePath reported — both
// agree the host has no Homebrew.
func brewExecutable() string {
	if path := ExecutablePath(); path != "" {
		return path
	}
	return "brew"
}
