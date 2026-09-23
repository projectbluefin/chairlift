// Package homebrew provides an interface to the Homebrew package manager
package homebrew

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/outputtail"
)

const (
	// readTimeout bounds read-only brew commands (listing, searching, info).
	readTimeout = 30 * time.Second
	// mutationTimeout bounds state-changing brew commands, which may download
	// and build packages and therefore need a far larger budget.
	mutationTimeout = 30 * time.Minute
	// waitDelay bounds how long Wait blocks after the process group has been
	// signalled. brew's helpers (git, curl, download workers) inherit the
	// stdout/stderr pipes, so a straggler could otherwise hold Wait open
	// forever even though the command itself is gone.
	waitDelay = 5 * time.Second
	// commandOutputTailLimit bounds retained mutation output used only for
	// diagnostics after a command fails. Successful mutation output is discarded.
	commandOutputTailLimit = 64 * 1024
)

type commandOutputWriter interface {
	Write([]byte) (int, error)
	String() string
}

func commandOutputWriters(args []string) (commandOutputWriter, commandOutputWriter, bool) {
	if isStateChanging(args) {
		return outputtail.New(commandOutputTailLimit), outputtail.New(commandOutputTailLimit), true
	}
	return &bytes.Buffer{}, &bytes.Buffer{}, false
}

// Error represents a Homebrew-related error. Err, when non-nil, carries the
// underlying cause (for example context.DeadlineExceeded or
// context.Canceled) so callers can classify it with errors.Is.
type Error struct {
	Message string
	Err     error
}

func (e *Error) Error() string {
	return e.Message
}

// Unwrap exposes the underlying cause to errors.Is/errors.As.
func (e *Error) Unwrap() error {
	return e.Err
}

// NotFoundError is returned when Homebrew is not installed
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

// Package represents an installed Homebrew package
type Package struct {
	Name               string   `json:"name"`
	Version            string   `json:"version"`
	InstalledOnRequest bool     `json:"installed_on_request"`
	Pinned             bool     `json:"pinned"`
	Outdated           bool     `json:"outdated"`
	Dependencies       []string `json:"dependencies,omitempty"`
}

// SearchResult represents a search result
type SearchResult struct {
	Name string
	Kind PackageKind
}

// PackageKind identifies which Homebrew install namespace a search result
// belongs to. Formulae and casks can share a name, so the kind must travel
// with the result all the way to the install command.
type PackageKind string

const (
	Formula PackageKind = "formula"
	Cask    PackageKind = "cask"
)

func (k PackageKind) DisplayName() string {
	if k == Cask {
		return "Cask"
	}
	return "Formula"
}

// stateChangingCommands are commands that modify system state
var stateChangingCommands = map[string]bool{
	"install":   true,
	"uninstall": true,
	"remove":    true,
	"upgrade":   true,
	"update":    true,
	"pin":       true,
	"unpin":     true,
	"bundle":    true,
	"cleanup":   true,
	"trust":     true,
	// tap clones a repository and changes which packages brew will
	// install from. Leaving it out would both run it for real under
	// --dry-run and bound a fresh clone by the 30-second read timeout.
	"tap": true,
}

// isStateChanging reports whether a brew invocation modifies system state
// (requiring mutationTimeout, bounded diagnostic output, and dry-run skipping)
// or is read-only (using readTimeout, full output, and active under dry-run).
func isStateChanging(args []string) bool {
	if len(args) == 0 {
		return false
	}
	cmd := args[0]
	if cmd == "bundle" {
		// Reclassify brew bundle subcommands: check and list are read-only,
		// while install and dump remain state-changing.
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			switch arg {
			case "check", "list":
				return false
			default:
				// install, dump, and any unrecognized subcommand are
				// treated as state-changing.
				return true
			}
		}
		// A bare "brew bundle" or flags without a subcommand defaults to install
		return true
	}
	return stateChangingCommands[cmd]
}

// commandTimeout returns the timeout class for a brew invocation: the
// mutation timeout for state-changing commands, the read timeout otherwise.
func commandTimeout(args []string) time.Duration {
	if isStateChanging(args) {
		return mutationTimeout
	}
	return readTimeout
}

// runBrewCommand executes a brew command and returns the output
func runBrewCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout(args))
	defer cancel()

	return runBrewCommandCtx(ctx, args...)
}

// runBrewCommandCtx is the single dry-run gate for brew: it applies the skip
// (before any exec.Cmd exists) and otherwise runs the command under ctx.
// runBrewCommand supplies its own timeout context; context-taking exported
// entry points such as Update supply the caller's context already narrowed to
// the mutation budget, so cancellation propagates without a second gate that
// could drift out of sync.
//
// The executable comes from brewExecutable, the same resolution IsInstalled
// reports through, so visibility and execution cannot name different binaries.
func runBrewCommandCtx(ctx context.Context, args ...string) (string, error) {
	if isStateChanging(args) && dryrun.Enabled() {
		msg := fmt.Sprintf("[DRY-RUN] Would execute: brew %s", strings.Join(args, " "))
		log.Println(msg)
		return msg, nil
	}

	return runBrewCommandAt(ctx, brewExecutable(), args...)
}

// runBrewCommandAt runs exe with args under ctx. Read-only commands return
// full stdout for parsers; state-changing commands discard successful output
// and retain only bounded stdout/stderr tails for failure diagnostics. The
// executable and context are parameters so tests can drive a fake script and
// control the deadline; the sole production caller (runBrewCommandCtx) passes
// the path brewExecutable resolved, so the binary that runs is the one
// ExecutablePath reported as installed.
//
// The command runs in its own process group and cancellation signals the
// whole group, so brew's helper processes (git, curl, download workers) die
// with it rather than being orphaned. cmd.Run still reaps the child.
func runBrewCommandAt(ctx context.Context, exe string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay

	stdout, stderr, boundedOutput := commandOutputWriters(args)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	if err != nil {
		display := exe
		if len(args) > 0 {
			display += " " + strings.Join(args, " ")
		}
		// Classify the context outcome first: the process was killed by our
		// own Cancel func, so the raw error would otherwise read
		// "signal: killed".
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return "", &Error{
				Message: fmt.Sprintf("Command '%s' timed out", display),
				Err:     context.DeadlineExceeded,
			}
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			return "", &Error{
				Message: fmt.Sprintf("Command '%s' was canceled", display),
				Err:     context.Canceled,
			}
		}
		stderrText := stderr.String()
		diagnosticText := stderrText
		if boundedOutput {
			// A state-changing command splits its diagnosis across both
			// streams — `brew bundle install` replays a failing entry's own
			// installer output on stdout and prints its summary on stderr —
			// so keeping only the non-empty one dropped the root cause
			// whenever the other stream also had content.
			diagnosticText = joinCommandStreams(stdout.String(), stderrText)
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// The message below is distilled to one line for the UI, so the
			// full retained output is logged here: it is the evidence a bug
			// report needs, and nothing else preserves it.
			// Only for state-changing commands: a read-only `brew search`
			// exits 1 with "No formulae or casks found" when one namespace
			// has no matches, and searchKind treats that as an empty result,
			// not a failure worth a log line.
			if boundedOutput {
				if trimmed := strings.TrimSpace(diagnosticText); trimmed != "" {
					log.Printf("Command '%s' failed:\n%s", display, trimmed)
				}
			}
			// Only a state-changing command is distilled to one line: its
			// full output is in the log above and its stdout replay is what
			// made the message unreadable. A read-only command keeps its
			// whole stderr in the error, as before, since nothing else
			// preserves it and callers such as searchKind match on it.
			message := diagnosticText
			tapMessage := stderrText
			if boundedOutput {
				message = summarizeDiagnostic(diagnosticText)
				tapMessage = summarizeDiagnostic(stderrText)
			}
			if isUntrustedTapMessage(stderrText) {
				return "", &UntrustedTapError{Message: fmt.Sprintf("Brew command failed: %s", tapMessage)}
			}
			return "", &Error{Message: fmt.Sprintf("Brew command failed: %s", message), Err: err}
		}
		// exec.ErrNotFound covers a bare name missing from $PATH;
		// fs.ErrNotExist covers an explicit path that does not exist.
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return "", &NotFoundError{Message: "Homebrew not found. Please install Homebrew first."}
		}
		return "", &Error{Message: err.Error(), Err: err}
	}

	if boundedOutput {
		return "", nil
	}
	return stdout.String(), nil
}

// IsInstalled checks if Homebrew is installed and accessible.
//
// It asks ExecutablePath where brew is instead of assuming $PATH, so a host
// whose Homebrew is only reachable at the Linuxbrew install path — reached by
// a direct binary launch, which bypasses data/chairlift-wrapper.sh and the
// `brew shellenv` it runs — is reported as installed exactly when
// runBrewCommandCtx would find a binary to run (issue #207). An absent
// Homebrew short-circuits rather than exec'ing a command that cannot resolve.
func IsInstalled() bool {
	exe := ExecutablePath()
	if exe == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, "--version")
	err := cmd.Run()
	return err == nil
}

var (
	installedOnce   sync.Once
	installedResult bool
)

// IsInstalledCached returns a cached result of IsInstalled, running the check at most once.
func IsInstalledCached() bool {
	installedOnce.Do(func() {
		installedResult = IsInstalled()
	})
	return installedResult
}

// ListInstalledFormulae returns all installed formulae
func ListInstalledFormulae() ([]Package, error) {
	output, err := runBrewCommand("info", "--installed", "--json=v2", "--formula")
	if err != nil {
		return nil, err
	}

	return parsePackagesJSON(output, true)
}

// ListInstalledCasks returns all installed casks
func ListInstalledCasks() ([]Package, error) {
	output, err := runBrewCommand("info", "--installed", "--json=v2", "--cask")
	if err != nil {
		return nil, err
	}

	return parsePackagesJSON(output, false)
}

// parsePackagesJSON parses the JSON output from brew info
func parsePackagesJSON(jsonData string, isFormula bool) ([]Package, error) {
	var data struct {
		Formulae []struct {
			Name     string `json:"name"`
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
			Installed []struct {
				Version            string `json:"version"`
				InstalledOnRequest bool   `json:"installed_on_request"`
			} `json:"installed"`
			Pinned   bool `json:"pinned"`
			Outdated bool `json:"outdated"`
		} `json:"formulae"`
		Casks []struct {
			Token     string `json:"token"`
			Version   string `json:"version"`
			Installed string `json:"installed"`
			Outdated  bool   `json:"outdated"`
		} `json:"casks"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, &Error{Message: fmt.Sprintf("Failed to parse JSON: %v", err)}
	}

	var packages []Package

	if isFormula {
		for _, f := range data.Formulae {
			if len(f.Installed) == 0 {
				continue
			}
			packages = append(packages, Package{
				Name:               f.Name,
				Version:            f.Installed[0].Version,
				InstalledOnRequest: f.Installed[0].InstalledOnRequest,
				Pinned:             f.Pinned,
				Outdated:           f.Outdated,
			})
		}
	} else {
		for _, c := range data.Casks {
			packages = append(packages, Package{
				Name:     c.Token,
				Version:  c.Installed,
				Outdated: c.Outdated,
			})
		}
	}

	return packages, nil
}

// ListOutdated returns all outdated packages
func ListOutdated() ([]Package, error) {
	output, err := runBrewCommand("outdated", "--json=v2")
	if err != nil {
		return nil, err
	}

	var data struct {
		Formulae []struct {
			Name              string   `json:"name"`
			InstalledVersions []string `json:"installed_versions"`
			CurrentVersion    string   `json:"current_version"`
			Pinned            bool     `json:"pinned"`
		} `json:"formulae"`
		Casks []struct {
			Name              string   `json:"name"`
			InstalledVersions []string `json:"installed_versions"`
			CurrentVersion    string   `json:"current_version"`
		} `json:"casks"`
	}

	if err := json.Unmarshal([]byte(output), &data); err != nil {
		return nil, &Error{Message: fmt.Sprintf("Failed to parse JSON: %v", err)}
	}

	var packages []Package
	for _, f := range data.Formulae {
		packages = append(packages, Package{
			Name:     f.Name,
			Version:  strings.Join(f.InstalledVersions, ", "),
			Outdated: true,
			Pinned:   f.Pinned,
		})
	}
	for _, c := range data.Casks {
		packages = append(packages, Package{
			Name:     c.Name,
			Version:  strings.Join(c.InstalledVersions, ", "),
			Outdated: true,
		})
	}

	return packages, nil
}

// Search searches both Homebrew namespaces and returns typed results.
func Search(query string) ([]SearchResult, error) {
	return searchWith(runBrewCommand, query)
}

func searchWith(run func(...string) (string, error), query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	formulae, err := searchKind(run, query, "--formula", Formula)
	if err != nil {
		return nil, err
	}
	casks, err := searchKind(run, query, "--cask", Cask)
	if err != nil {
		return nil, err
	}
	return append(formulae, casks...), nil
}

func searchKind(run func(...string) (string, error), query, flag string, kind PackageKind) ([]SearchResult, error) {
	output, err := run("search", flag, query)
	if err != nil {
		// Homebrew exits 1 when one namespace has no matches, even if the
		// other namespace may have results. Treat only that documented
		// no-result diagnostic as an empty category.
		if strings.Contains(err.Error(), "No formulae or casks found") {
			return nil, nil
		}
		return nil, err
	}

	return parseSearchOutput(output, kind), nil
}

func parseSearchOutput(output string, kind PackageKind) []SearchResult {
	var results []SearchResult
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "==>") {
			results = append(results, SearchResult{Name: line, Kind: kind})
		}
	}
	return results
}

// Tap adds a third-party tap. It is required before installing a package by
// its qualified user/tap/name: brew refuses the shorthand for a tap that is
// not already present, rather than silently trusting it.
func Tap(name string) error {
	_, err := runBrewCommand("tap", name)
	return err
}

// Install installs a package
func Install(name string, isCask bool) error {
	args := []string{"install"}
	if isCask {
		args = append(args, "--cask")
	}
	args = append(args, name)

	_, err := runBrewCommand(args...)
	return err
}

// Uninstall removes a package
func Uninstall(name string, isCask bool) error {
	args := []string{"uninstall"}
	if isCask {
		args = append(args, "--cask")
	}
	args = append(args, name)

	_, err := runBrewCommand(args...)
	return err
}

// Upgrade upgrades one package or all packages. The caller's context also
// bounds the mutation so Update All cancellation stops an in-flight upgrade.
func Upgrade(ctx context.Context, name string) error {
	args := []string{"upgrade"}
	if name != "" {
		args = append(args, name)
	}

	runCtx, cancel := context.WithTimeout(ctx, mutationTimeout)
	defer cancel()
	_, err := runBrewCommandCtx(runCtx, args...)
	return err
}

// Update updates Homebrew itself. It runs under ctx so an enclosing run's
// cancellation — Update All, for example — propagates into the command and
// stops it, instead of the command ignoring the parent deadline and running to
// its own 30-minute budget. The mutation budget is still applied on top, so a
// lone caller stays bounded by whichever deadline is nearer.
func Update(ctx context.Context) error {
	runCtx, cancel := context.WithTimeout(ctx, mutationTimeout)
	defer cancel()

	_, err := runBrewCommandCtx(runCtx, "update")
	return err
}

// Pin pins a package
func Pin(name string) error {
	_, err := runBrewCommand("pin", name)
	return err
}

// Unpin unpins a package
func Unpin(name string) error {
	_, err := runBrewCommand("unpin", name)
	return err
}

// BundleDump dumps installed packages to a Brewfile
func BundleDump(path string, force bool) error {
	args := []string{"bundle", "dump"}
	if path != "" {
		args = append(args, "--file="+path)
	}
	if force {
		args = append(args, "--force")
	}

	_, err := runBrewCommand(args...)
	return err
}

// BundleInstall installs packages from a Brewfile
func BundleInstall(path string) error {
	args := []string{"bundle", "install"}
	if path != "" {
		args = append(args, "--file="+path)
	}

	_, err := runBrewCommand(args...)
	return err
}

// BundleStatus represents the multi-state status of a Brewfile bundle.
type BundleStatus string

const (
	BundleInstalled       BundleStatus = "installed"
	BundleUpdateAvailable BundleStatus = "update_available"
	BundleNotInstalled    BundleStatus = "not_installed"
	BundleIndeterminate   BundleStatus = "indeterminate"
)

// DisplayName returns a human-readable representation of the bundle status.
func (s BundleStatus) DisplayName() string {
	switch s {
	case BundleInstalled:
		return "Installed"
	case BundleUpdateAvailable:
		return "Update Available"
	case BundleNotInstalled:
		return "Not Installed"
	default:
		return "Indeterminate"
	}
}

type exitCoder interface {
	ExitCode() int
}

func exitCode(err error) int {
	var ec exitCoder
	if errors.As(err, &ec) {
		return ec.ExitCode()
	}
	return -1
}

func isMalformedManifestError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, term := range []string{
		"syntax error",
		"undefined method",
		"loaderror",
		"malformed",
		"failed to parse",
		"cannot load such file",
	} {
		if strings.Contains(msg, term) {
			return true
		}
	}
	return false
}

// BundleCheck checks the multi-state status of a Brewfile bundle using a two-pass check:
// 1. Pass 1: `brew bundle check --file=<path>` -> exit 0: Installed.
// 2. Pass 2: If exit != 0, re-run with `--no-upgrade` -> exit 0: Update Available; exit 1: Not Installed.
// 3. Other errors (timeout, malformed manifest) -> Indeterminate/Error.
func BundleCheck(path string) (BundleStatus, error) {
	return bundleCheckWith(runBrewCommand, path)
}

func bundleCheckWith(run func(...string) (string, error), path string) (BundleStatus, error) {
	pass1Args := []string{"bundle", "check"}
	if path != "" {
		pass1Args = append(pass1Args, "--file="+path)
	}

	_, err1 := run(pass1Args...)
	if err1 == nil {
		return BundleInstalled, nil
	}

	if errors.Is(err1, context.DeadlineExceeded) || errors.Is(err1, context.Canceled) {
		return BundleIndeterminate, err1
	}
	if isMalformedManifestError(err1) {
		return BundleIndeterminate, err1
	}

	code1 := exitCode(err1)
	if code1 != 1 {
		return BundleIndeterminate, err1
	}

	pass2Args := []string{"bundle", "check", "--no-upgrade"}
	if path != "" {
		pass2Args = append(pass2Args, "--file="+path)
	}

	_, err2 := run(pass2Args...)
	if err2 == nil {
		return BundleUpdateAvailable, nil
	}

	if errors.Is(err2, context.DeadlineExceeded) || errors.Is(err2, context.Canceled) {
		return BundleIndeterminate, err2
	}
	if isMalformedManifestError(err2) {
		return BundleIndeterminate, err2
	}

	code2 := exitCode(err2)
	if code2 == 1 {
		return BundleNotInstalled, nil
	}

	return BundleIndeterminate, err2
}

// Cleanup removes old versions, outdated downloads, and clears cache
func Cleanup() (string, error) {
	return runBrewCommand("cleanup")
}
