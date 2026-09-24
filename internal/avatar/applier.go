package avatar

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// BusctlCommand is the program an avatar dispatch goes through. It is named
// here rather than spelled at the call site for the same reason
// internal/pkexec owns the escalation program name: one owner means a future
// reader can find every external command the avatar pipeline runs.
//
// busctl is deliberately not an escalation route. AccountsService exposes
// SetIconFile to the *unprivileged* caller and authorizes it with PolicyKit
// action org.freedesktop.accounts.change-own-user-data, which the desktop
// session's own authentication agent — not pkexec — brokers. That is what
// makes an avatar change need no privileged helper, no data/*.policy entry,
// and no journal record: the account being modified is the one that asked.
const BusctlCommand = "busctl"

// The D-Bus coordinates of the one call this package makes. busctl's grammar
// is `call SERVICE OBJECT INTERFACE METHOD [SIGNATURE ARG...]`, so the object
// path is a single argument built from the UID and nothing else.
const (
	accountsServiceName   = "org.freedesktop.Accounts"
	accountsUserInterface = "org.freedesktop.Accounts.User"
	accountsUserPathBase  = "/org/freedesktop/Accounts/User"
	setIconFileMethod     = "SetIconFile"
)

// The two files AccountsService and GDM read when the bus route is not
// available. AccountsService prefers ~/.face.icon; ~/.face is the legacy
// GDM face file, written as well because a session that never starts
// AccountsService (or a display manager reading the home directory directly)
// only looks at that one.
const (
	faceIconFileName = ".face.icon"
	faceFileName     = ".face"
)

// faceFileMode is the mode both face files are written with. They are inside
// the owning user's home and are read by the display manager's greeter, which
// is not the same process, so they cannot be 0600.
const faceFileMode = 0o644

// accountsIconDir is where AccountsService keeps the icon SetIconFile copied
// in, one file per user name. It is a variable only so tests can point it at
// a temporary directory.
var accountsIconDir = "/var/lib/AccountsService/icons"

// CurrentPicture returns the file the account's picture is currently read
// from, or "" when none exists: AccountsService's own copy first, because
// that is what the running session shows, then the two face files a
// fallback dispatch writes. It only stats files, so a page may call it
// without a subprocess or a network round trip.
func CurrentPicture(username, home string) string {
	candidates := []string{
		filepath.Join(accountsIconDir, username),
		filepath.Join(home, faceIconFileName),
		filepath.Join(home, faceFileName),
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// Request is one avatar dispatch.
//
// UID and ID are carried as separate facts rather than one being derived from
// the other. The UID selects the AccountsService object path and is an
// integer, so no caller-supplied string is ever interpolated into that path;
// the ID is the catalog identifier the user picked and appears only in the
// dry-run log line. Nothing in this file derives a path, a URL, or a D-Bus
// argument from ID — see LookupPath for why an identifier is not a filename.
type Request struct {
	// UID is the account whose avatar changes.
	UID int
	// ID is the catalog identifier of the chosen avatar, used for logging.
	ID string
	// IconPath is the transcoded PNG on disk. AccountsService is told to read
	// it over the bus, and the face-file fallback copies its bytes.
	IconPath string
}

// Route identifies which mechanism actually set the avatar. It is meaningful
// only when Dispatch returned a nil error.
type Route string

const (
	// RouteBusctl means AccountsService accepted SetIconFile: the change is
	// live for the running session.
	RouteBusctl Route = "busctl"
	// RouteFaceFile means the bus route failed or was unreachable and the
	// icon was written to the face files instead. AccountsService picks those
	// up at the next session start, not immediately — a caller rendering this
	// outcome must not present it as a live change.
	RouteFaceFile Route = "face-file"
	// RouteDryRun means nothing was touched: neither the bus nor the disk.
	RouteDryRun Route = "dry-run"
)

// ApplierFunc sets the avatar described by req and reports the route that took
// effect. It is the seam the picker calls, so a test can assert what the GUI
// asks for without a live AccountsService, and so the fallback decision is
// made in one tested place rather than in a GTK page builder (ADR-0007).
type ApplierFunc func(ctx context.Context, req Request) (Route, error)

// Applier is the production ApplierFunc: AccountsService over busctl first,
// the face files only when that route fails.
//
// The zero value is usable and is equivalent to NewApplier.
type Applier struct {
	// run executes one argv whose argv[0] is the program name, returning a
	// non-nil error when the call failed. nil means runBusctl. It is a field
	// so no test spawns busctl or reaches a real AccountsService.
	run func(ctx context.Context, argv []string) error
	// homeDir resolves the directory the face-file fallback writes into. nil
	// means os.UserHomeDir, so tests point it at t.TempDir instead of the
	// developer's own home.
	homeDir func() (string, error)
}

// NewApplier returns the production applier.
func NewApplier() *Applier {
	return &Applier{run: runBusctl, homeDir: os.UserHomeDir}
}

// Dispatch sets the avatar described by req.
//
// The dry-run gate comes first and returns before any process is constructed
// or any file is opened, so a preview touches neither the bus nor the disk.
//
// A live call goes to AccountsService first. Only a failure there falls back
// to the face files, and a canceled context is not a failure of the bus: a
// caller that has already given up does not get bytes written into its home
// directory for an action it abandoned.
func (a *Applier) Dispatch(ctx context.Context, req Request) (Route, error) {
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set avatar to %s", req.ID)
		return RouteDryRun, nil
	}

	busErr := a.runner()(ctx, SetIconFileArgs(req.UID, req.IconPath))
	if busErr == nil {
		return RouteBusctl, nil
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", fmt.Errorf("setting avatar %s: %w", req.ID, errors.Join(busErr, ctxErr))
	}

	log.Printf("avatar: AccountsService did not accept SetIconFile for uid %d (%v); "+
		"falling back to the face files, which take effect at the next session start", req.UID, busErr)

	if err := a.writeFaceFiles(req); err != nil {
		return "", fmt.Errorf("setting avatar %s: AccountsService failed (%v) and the face-file fallback failed: %w",
			req.ID, busErr, err)
	}
	return RouteFaceFile, nil
}

// SetIconFileArgs returns the argv for the one AccountsService call an avatar
// dispatch makes, with the program name as argv[0].
//
// The object path is composed from the integer UID. It is never built from a
// caller-supplied string, and no element of argv is passed through a shell, so
// there is no argument an authenticated caller could shape into a different
// D-Bus object or method.
func SetIconFileArgs(uid int, iconPath string) []string {
	return []string{
		BusctlCommand,
		"call",
		accountsServiceName,
		accountsUserPathBase + strconv.Itoa(uid),
		accountsUserInterface,
		setIconFileMethod,
		"s",
		iconPath,
	}
}

// runner returns the command runner to use.
func (a *Applier) runner() func(ctx context.Context, argv []string) error {
	if a.run != nil {
		return a.run
	}
	return runBusctl
}

// home returns the directory the face-file fallback writes into.
func (a *Applier) home() (string, error) {
	if a.homeDir != nil {
		return a.homeDir()
	}
	return os.UserHomeDir()
}

// writeFaceFiles copies the transcoded icon into both face files.
//
// Both are attempted even when the first fails, because they serve different
// readers: reporting success after writing only ~/.face.icon would leave a
// display manager that reads ~/.face showing the old picture.
func (a *Applier) writeFaceFiles(req Request) error {
	home, err := a.home()
	if err != nil {
		return fmt.Errorf("resolving the home directory: %w", err)
	}

	icon, err := os.ReadFile(req.IconPath)
	if err != nil {
		return fmt.Errorf("reading the transcoded avatar: %w", err)
	}

	failures := make([]error, 0, 2)
	for _, name := range []string{faceIconFileName, faceFileName} {
		path := filepath.Join(home, name)
		if err := os.WriteFile(path, icon, faceFileMode); err != nil {
			failures = append(failures, fmt.Errorf("writing %s: %w", path, err))
		}
	}
	return errors.Join(failures...)
}

// runBusctl executes one busctl argv and returns an error carrying the
// command's own diagnostic when it fails.
//
// busctl reports a rejected call on stderr with its exit status, and that text
// is the only place the PolicyKit denial or the missing-service message
// appears. Discarding it would leave the log line saying only "exit status 1".
func runBusctl(ctx context.Context, argv []string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if message := strings.TrimSpace(string(output)); message != "" {
			return fmt.Errorf("%s: %w: %s", argv[0], err, message)
		}
		return fmt.Errorf("%s: %w", argv[0], err)
	}
	return nil
}
