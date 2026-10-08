// Package flatpak provides an interface to the Flatpak package manager
package flatpak

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/outputtail"
)

const (
	// readTimeout bounds read-only flatpak commands.
	readTimeout = 30 * time.Second
	// mutationTimeout bounds state-changing flatpak commands, which may
	// download large application images and therefore need a far larger budget.
	mutationTimeout = 30 * time.Minute
	// waitDelay bounds how long Wait blocks after the process group has been
	// signalled. flatpak's helpers (download workers, ostree pulls) inherit
	// the stdout/stderr pipes, so a straggler could otherwise hold Wait open
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
	if len(args) > 0 && stateChangingCommands[args[0]] {
		return outputtail.New(commandOutputTailLimit), outputtail.New(commandOutputTailLimit), true
	}
	return &bytes.Buffer{}, &bytes.Buffer{}, false
}

// Error represents a Flatpak-related error. Err, when non-nil, carries the
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

// NotFoundError is returned when Flatpak is not installed
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

// Kind distinguishes the two shapes of installed Flatpak ref. It exists
// because `flatpak list` reports them through mutually exclusive filters:
// `--app` never returns a runtime, and `--runtime` never returns an
// application. Anything shipped as a runtime extension — the MangoHud Vulkan
// layer, for one — is therefore invisible to an inventory that only ever
// passes `--app`.
type Kind string

const (
	// KindApplication is an installed application (`app/…` ref).
	KindApplication Kind = "app"
	// KindRuntime is an installed runtime, SDK, or runtime extension
	// (`runtime/…` ref).
	KindRuntime Kind = "runtime"
)

// listFlag returns the `flatpak list` filter that reports this kind.
func (k Kind) listFlag() string {
	if k == KindRuntime {
		return "--runtime"
	}
	return "--app"
}

// Application represents an installed Flatpak ref. Kind records whether that
// ref is an application or a runtime; the field is named for the struct's
// original application-only use, which every existing caller still has.
type Application struct {
	Name          string `json:"name"`
	ApplicationID string `json:"application"`
	Version       string `json:"version"`
	// Branch is the installed ref's branch ("stable" for most applications,
	// a Platform version such as "25.08" for runtime extensions). A
	// runtime extension can be installed in several branches at once, and
	// then only a branch-qualified ref ("ID//BRANCH") names one of them.
	Branch       string `json:"branch"`
	Installation string `json:"installation"` // "user" or "system"
	Kind         Kind   `json:"kind"`         // "app" or "runtime"
}

// stateChangingCommands are commands that modify system state
var stateChangingCommands = map[string]bool{
	"install":   true,
	"uninstall": true,
	"remove":    true,
	"update":    true,
}

// commandTimeout returns the timeout class for a flatpak invocation: the
// mutation timeout for state-changing commands, the read timeout otherwise.
func commandTimeout(args []string) time.Duration {
	if len(args) > 0 && stateChangingCommands[args[0]] {
		return mutationTimeout
	}
	return readTimeout
}

// runFlatpakCommand executes a flatpak command and returns the output
func runFlatpakCommand(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout(args))
	defer cancel()

	return runFlatpakCommandCtx(ctx, args...)
}

// runFlatpakCommandCtx is the single dry-run gate for flatpak: it applies the
// skip (before any exec.Cmd exists) and otherwise runs the command under ctx.
// runFlatpakCommand supplies its own timeout context; context-taking exported
// entry points such as Update supply the caller's context already narrowed to
// the mutation budget, so cancellation propagates without a second gate that
// could drift out of sync.
func runFlatpakCommandCtx(ctx context.Context, args ...string) (string, error) {
	if len(args) > 0 && stateChangingCommands[args[0]] && dryrun.Enabled() {
		msg := fmt.Sprintf("[DRY-RUN] Would execute: flatpak %s", strings.Join(args, " "))
		log.Println(msg)
		return msg, nil
	}

	return runFlatpakCommandAt(ctx, "flatpak", args...)
}

// runFlatpakCommandAt runs exe with args under ctx. Read-only commands return
// full stdout for parsers; state-changing commands discard successful output
// and retain only bounded stdout/stderr tails for failure diagnostics. The
// executable and context are parameters so tests can drive a fake script and
// control the deadline; the sole production caller
// (runFlatpakCommandCtx) always passes "flatpak".
//
// The command runs in its own process group and cancellation signals the
// whole group, so flatpak's helper processes (download workers, ostree pulls)
// die with it rather than being orphaned. cmd.Run still reaps the child.
func runFlatpakCommandAt(ctx context.Context, exe string, args ...string) (string, error) {
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
		if boundedOutput && diagnosticText == "" {
			diagnosticText = stdout.String()
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", &Error{Message: fmt.Sprintf("Flatpak command failed: %s", diagnosticText), Err: err}
		}
		// exec.ErrNotFound covers a bare name missing from $PATH;
		// fs.ErrNotExist covers an explicit path that does not exist.
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return "", &NotFoundError{Message: "Flatpak not found. Please install Flatpak first."}
		}
		return "", &Error{Message: err.Error(), Err: err}
	}

	if boundedOutput {
		return "", nil
	}
	return stdout.String(), nil
}

// IsInstalled checks if Flatpak is installed and accessible
func IsInstalled() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "flatpak", "--version")
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

// ListUserApplications returns all user-installed Flatpak applications
func ListUserApplications() ([]Application, error) {
	return listRefs("--user", KindApplication)
}

// ListSystemApplications returns all system-installed Flatpak applications
func ListSystemApplications() ([]Application, error) {
	return listRefs("--system", KindApplication)
}

// ListUserRuntimes returns every user-installed runtime, SDK, and runtime
// extension. It is a separate query rather than a widened application listing
// because `flatpak list --app` excludes them entirely.
func ListUserRuntimes() ([]Application, error) {
	return listRefs("--user", KindRuntime)
}

// ListSystemRuntimes returns every system-installed runtime, SDK, and runtime
// extension.
func ListSystemRuntimes() ([]Application, error) {
	return listRefs("--system", KindRuntime)
}

// listRefs lists installed refs of one kind for a given installation type
func listRefs(installFlag string, kind Kind) ([]Application, error) {
	// Use columns format for structured output
	output, err := runFlatpakCommand("list", installFlag, kind.listFlag(), "--columns=name,application,version,branch")
	if err != nil {
		return nil, err
	}

	return parseRefList(output, installFlag, kind)
}

// parseRefList parses the tabular output from flatpak list. The kind is
// stamped from the filter the listing was requested with rather than read back
// out of the ref column, so a row that falls through to the whitespace
// fallback below — where the ref may not have been captured at all — is still
// classified correctly.
func parseRefList(output string, installFlag string, kind Kind) ([]Application, error) {
	var apps []Application
	lines := strings.Split(strings.TrimSpace(output), "\n")

	installation := "system"
	if installFlag == "--user" {
		installation = "user"
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Split by tab (flatpak uses tabs as column separators)
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			// Try splitting by multiple spaces for systems that might use spaces
			fields = strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
		}

		app := Application{
			Installation: installation,
			Kind:         kind,
		}

		if len(fields) >= 1 {
			app.Name = strings.TrimSpace(fields[0])
		}
		if len(fields) >= 2 {
			app.ApplicationID = strings.TrimSpace(fields[1])
		}
		if len(fields) >= 3 {
			app.Version = strings.TrimSpace(fields[2])
		}
		if len(fields) >= 4 {
			app.Branch = strings.TrimSpace(fields[3])
		}
		apps = append(apps, app)
	}

	return apps, nil
}

// Install installs a Flatpak application
func Install(appID string, user bool) error {
	args := []string{"install", "-y"}
	if user {
		args = append(args, "--user")
	} else {
		args = append(args, "--system")
	}
	args = append(args, appID)

	_, err := runFlatpakCommand(args...)
	return err
}

// InstallFromRemote installs appID from a specific remote into the user or
// system scope: `flatpak install -y [--user|--system] <remote> <appID>`. The
// remote is the origin flatpak pulls from (typically "flathub"); pass "" for
// the default remote. Like Install it enforces dryrun and runs through the
// shared runner, so it inherits the process-group and timeout handling.
func InstallFromRemote(appID, remote string, user bool) error {
	args := []string{"install", "-y"}
	if user {
		args = append(args, "--user")
	} else {
		args = append(args, "--system")
	}
	if remote != "" {
		args = append(args, remote)
	}
	args = append(args, appID)

	_, err := runFlatpakCommand(args...)
	return err
}

// Uninstall removes a Flatpak application
func Uninstall(appID string, user bool) error {
	args := []string{"uninstall", "-y"}
	if user {
		args = append(args, "--user")
	} else {
		args = append(args, "--system")
	}
	args = append(args, appID)

	_, err := runFlatpakCommand(args...)
	return err
}

// AppRuntime returns the runtime ref ("ID/ARCH/BRANCH") the installed
// application appID runs on. `flatpak info` without a scope flag searches
// every installation, so a copy in either scope answers.
func AppRuntime(appID string) (string, error) {
	output, err := runFlatpakCommand("info", "--show-runtime", appID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// RemoteAppRuntime returns the runtime ref ("ID/ARCH/BRANCH") the application
// appID published on remote runs on, read from that remote as configured in
// the user or system installation. It is the answer AppRuntime would give
// once appID is installed from there.
func RemoteAppRuntime(remote, appID string, user bool) (string, error) {
	args := []string{"remote-info"}
	if user {
		args = append(args, "--user")
	} else {
		args = append(args, "--system")
	}
	args = append(args, "--app", "--show-runtime", remote, appID)

	output, err := runFlatpakCommand(args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// RefBranch returns the branch of a full ref: "ID/ARCH/BRANCH", as
// `--show-runtime` prints it, or "KIND/ID/ARCH/BRANCH".
func RefBranch(ref string) (string, error) {
	parts := strings.Split(strings.TrimSpace(ref), "/")
	if (len(parts) != 3 && len(parts) != 4) || slices.Contains(parts, "") {
		return "", fmt.Errorf("unrecognized Flatpak ref %q", ref)
	}
	return parts[len(parts)-1], nil
}

// Update updates a Flatpak application, or all applications when appID is
// empty. It runs under ctx so a caller that started the command as one phase
// of a larger run — Update All, for example — can cancel the whole run and
// have this command stop with it, instead of the command ignoring the parent
// deadline and running to its own 30-minute budget. The mutation budget is
// still applied on top, so a lone caller stays bounded by whichever deadline
// is nearer.
func Update(ctx context.Context, appID string, user bool) error {
	args := []string{"update", "-y"}
	if user {
		args = append(args, "--user")
	} else {
		args = append(args, "--system")
	}
	if appID != "" {
		args = append(args, appID)
	}

	// context.WithTimeout takes whichever deadline is nearer: the run's own
	// deadline (if any) or the mutation budget. A parent that is already
	// cancelled makes the command fail immediately rather than start a fresh
	// 30-minute run.
	runCtx, cancel := context.WithTimeout(ctx, mutationTimeout)
	defer cancel()

	_, err := runFlatpakCommandCtx(runCtx, args...)
	return err
}

// UpdateInfo represents an available Flatpak update.
type UpdateInfo struct {
	Name          string `json:"name"`
	ApplicationID string `json:"application"`
	NewVersion    string `json:"new_version"`
	Installation  string `json:"installation"` // "user" or "system"
}

// installationFlag returns the scope flag every per-installation query passes.
func installationFlag(user bool) string {
	if user {
		return "--user"
	}
	return "--system"
}

// updateRemoteListArgs lists the scope's remotes with their options. flatpak
// already omits disabled remotes unless --show-disabled is given; the options
// column lets parseUpdateRemotes check that rather than depend on it.
func updateRemoteListArgs(user bool) []string {
	return []string{"remotes", installationFlag(user), "--columns=name,options"}
}

// parseUpdateRemotes returns the remotes an update query covers, in listed
// order: every remote except a disabled one, which is exactly the set
// flatpak's own remote-less `remote-ls` visits.
func parseUpdateRemotes(output string) []string {
	var remotes []string
	for _, line := range strings.Split(output, "\n") {
		name, options, _ := strings.Cut(strings.TrimSpace(line), "\t")
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(strings.Split(options, ","), "disabled") {
			continue
		}
		remotes = append(remotes, name)
	}
	return remotes
}

// originListArgs lists the origin remote of every installed application in
// the scope. "--app" matches the update query's own filter: runtimes never
// appear in the update inventory, so a remote that serves only runtimes —
// the usual leftover when an application is uninstalled without
// `--unused` — cannot hide an update the inventory would list.
func originListArgs(user bool) []string {
	return []string{"list", installationFlag(user), "--app", "--columns=origin"}
}

// parseOrigins returns the set of remotes named in `flatpak list
// --columns=origin` output.
func parseOrigins(output string) map[string]bool {
	origins := make(map[string]bool)
	for _, line := range strings.Split(output, "\n") {
		if origin := strings.TrimSpace(line); origin != "" {
			origins[origin] = true
		}
	}
	return origins
}

// updateListArgs builds the flatpak argument list used to query one remote's
// available updates. "--app" restricts the query to applications so runtimes
// never appear as updates.
func updateListArgs(user bool, remote string) []string {
	return []string{"remote-ls", "--updates", "--app", "--columns=name,application,version", installationFlag(user), remote}
}

// RemoteError reports a remote whose update query failed while installed
// applications in its installation still come from it, so updates for those
// applications are unknown.
type RemoteError struct {
	Remote       string
	Installation string // "user" or "system"
	Err          error
}

func (e *RemoteError) Error() string {
	return fmt.Sprintf("Flatpak remote %q in the %s installation could not be checked: %v", e.Remote, e.Installation, e.Err)
}

// Unwrap exposes the failed query's cause to errors.Is/errors.As.
func (e *RemoteError) Unwrap() error {
	return e.Err
}

// readCtx runs a read-only flatpak query under ctx, bounded by the read
// timeout.
func readCtx(ctx context.Context, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return runFlatpakCommandCtx(runCtx, args...)
}

// ListUpdates returns available updates for Flatpak applications in one
// installation. It queries each enabled remote separately, because a single
// remote-less `remote-ls --updates` fails outright when any one remote is
// unreachable — a leftover remote from an uninstalled application then hid
// every other remote's updates (issue #471). A remote whose query fails is
// ignored, with a log line, when no installed application in the
// installation comes from it; otherwise it is reported as a *RemoteError.
// When the returned error is non-nil the slice still holds the updates the
// healthy remotes reported, for callers that can use a partial inventory.
//
// The caller's ctx governs every query so a cancellation on the mutation or
// reconciliation path aborts the network call instead of leaving an orphaned
// flatpak download running; the read timeout still bounds each healthy call.
func ListUpdates(ctx context.Context, user bool) ([]UpdateInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	remotesOutput, err := readCtx(ctx, updateRemoteListArgs(user)...)
	if err != nil {
		return nil, err
	}

	installation := "system"
	if user {
		installation = "user"
	}
	var updates []UpdateInfo
	var failed []*RemoteError
	for _, remote := range parseUpdateRemotes(remotesOutput) {
		output, err := readCtx(ctx, updateListArgs(user, remote)...)
		if err != nil {
			// A cancelled run and a missing binary are not one remote's
			// failure; only the remote's own error may be judged below.
			var notFound *NotFoundError
			if ctx.Err() != nil || errors.As(err, &notFound) {
				return nil, err
			}
			failed = append(failed, &RemoteError{Remote: remote, Installation: installation, Err: err})
			continue
		}
		remoteUpdates, _ := parseUpdateList(output, user)
		updates = append(updates, remoteUpdates...)
	}
	if len(failed) == 0 {
		return updates, nil
	}

	originsOutput, err := readCtx(ctx, originListArgs(user)...)
	if err != nil {
		// Without the origins no failed remote can be shown unused.
		errs := []error{err}
		for _, remoteErr := range failed {
			errs = append(errs, remoteErr)
		}
		return updates, errors.Join(errs...)
	}
	origins := parseOrigins(originsOutput)
	var errs []error
	for _, remoteErr := range failed {
		if !origins[remoteErr.Remote] {
			log.Printf("flatpak: ignoring %s remote %q, which no installed application uses: %v", installation, remoteErr.Remote, remoteErr.Err)
			continue
		}
		errs = append(errs, remoteErr)
	}
	return updates, errors.Join(errs...)
}

// parseUpdateList parses the tabular output from the flatpak update query
func parseUpdateList(output string, user bool) ([]UpdateInfo, error) {
	var updates []UpdateInfo
	lines := strings.Split(strings.TrimSpace(output), "\n")

	installation := "system"
	if user {
		installation = "user"
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Split by tab (flatpak uses tabs as column separators)
		fields := strings.Split(line, "\t")
		if len(fields) < 3 {
			// Try splitting by multiple spaces for systems that might use spaces
			fields = strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
		}

		update := UpdateInfo{
			Installation: installation,
		}

		if len(fields) >= 1 {
			update.Name = strings.TrimSpace(fields[0])
		}
		if len(fields) >= 2 {
			update.ApplicationID = strings.TrimSpace(fields[1])
		}
		if len(fields) >= 3 {
			update.NewVersion = strings.TrimSpace(fields[2])
		}

		updates = append(updates, update)
	}

	return updates, nil
}

// UninstallUnused removes unused Flatpak runtimes and extensions in both the
// user and system installation scopes. Flatpak defaults to the system
// installation when no scope flag is given, so running without a scope would
// leave unused user runtimes behind while reporting success. Each scope is run
// independently; output is combined and an error from either scope is reported
// (via errors.Join) rather than masking the other scope's result.
func UninstallUnused() (string, error) {
	var (
		out  bytes.Buffer
		errs []error
	)
	for _, flag := range []string{"--user", "--system"} {
		scopeOut, scopeErr := runFlatpakCommand("uninstall", "--unused", "-y", flag)
		if scopeOut != "" {
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(scopeOut)
		}
		if scopeErr != nil {
			errs = append(errs, fmt.Errorf("%s scope: %w", flag, scopeErr))
		}
	}

	return out.String(), errors.Join(errs...)
}

// RemoveAllUser uninstalls every user-scope Flatpak application. It is
// Powerwash's Flatpak step (internal/powerwash) — the entire point is
// removing everything, so it takes no application ID, unlike Uninstall.
func RemoveAllUser() error {
	_, err := runFlatpakCommand("uninstall", "--user", "--all", "-y")
	return err
}

// HasUserRefs reports whether the user installation holds any application or
// runtime — exactly what RemoveAllUser would uninstall. It is read-only.
// Powerwash consults it first because `flatpak uninstall --user --all -y`
// exits 0 with nothing to remove, which on its own reads as a removal.
func HasUserRefs() (bool, error) {
	apps, err := ListUserApplications()
	if err != nil {
		return false, err
	}
	if len(apps) > 0 {
		return true, nil
	}
	runtimes, err := ListUserRuntimes()
	if err != nil {
		return false, err
	}
	return len(runtimes) > 0, nil
}
