package avatar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// pngPayload stands in for a transcoded avatar. It is never decoded here; the
// applier moves bytes and never inspects them, so the only property the tests
// need is that the exact bytes arrive in the face files.
var pngPayload = []byte("\x89PNG\r\n\x1a\n transcoded avatar bytes")

// The picker takes the seam rather than the concrete applier, so Dispatch must
// keep matching ApplierFunc.
var _ ApplierFunc = NewApplier().Dispatch

// recordingRunner stands in for busctl. Production never reaches it in a
// test, so no test spawns a process or talks to a real AccountsService.
type recordingRunner struct {
	calls [][]string
	err   error
}

func (r *recordingRunner) run(_ context.Context, argv []string) error {
	r.calls = append(r.calls, append([]string(nil), argv...))
	return r.err
}

// testApplier returns an Applier whose bus seam is the given runner and whose
// home directory is a temporary directory, so nothing a test does can reach
// the developer's own home.
func testApplier(t *testing.T, runner func(context.Context, []string) error) (*Applier, string) {
	t.Helper()
	home := t.TempDir()
	return &Applier{run: runner, homeDir: func() (string, error) { return home, nil }}, home
}

// writeIcon writes the payload the applier is asked to dispatch.
func writeIcon(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "avatar.png")
	if err := os.WriteFile(path, pngPayload, 0o600); err != nil {
		t.Fatalf("seeding the transcoded avatar: %v", err)
	}
	return path
}

// TestSetIconFileArgsNamesTheAccountsServiceCall pins the whole argv, not a
// substring of it: the address of the call is the entire contract with
// AccountsService, and a busctl invocation with a wrong object path or a
// missing signature does not fail loudly — it reaches the bus and is refused,
// which is indistinguishable from the account simply not accepting the icon.
func TestSetIconFileArgsNamesTheAccountsServiceCall(t *testing.T) {
	const iconPath = "/run/user/1000/chairlift/avatar.png"

	got := SetIconFileArgs(1000, iconPath)
	want := []string{
		"busctl",
		"call",
		"org.freedesktop.Accounts",
		"/org/freedesktop/Accounts/User1000",
		"org.freedesktop.Accounts.User",
		"SetIconFile",
		"s",
		iconPath,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SetIconFileArgs(1000, %q) =\n  %q\nwant\n  %q", iconPath, got, want)
	}
}

// TestSetIconFileArgsComposesTheObjectPathFromTheUID covers the half of the
// call the caller could otherwise influence. The object path is assembled
// from an int, so the set of objects an authenticated caller can address is
// exactly the set of uids it can name — and no element of argv is a string a
// shell or a D-Bus path parser would re-split.
func TestSetIconFileArgsComposesTheObjectPathFromTheUID(t *testing.T) {
	for _, uid := range []int{0, 1000, 65534} {
		argv := SetIconFileArgs(uid, "/tmp/avatar.png")

		want := "/org/freedesktop/Accounts/User" + strconv.Itoa(uid)
		if argv[3] != want {
			t.Errorf("uid %d: object path is %q, want %q", uid, argv[3], want)
		}
		if argv[0] != BusctlCommand {
			t.Errorf("uid %d: argv[0] is %q, want %q", uid, argv[0], BusctlCommand)
		}
		for i, arg := range argv {
			if strings.ContainsAny(arg, " \t\n") {
				t.Errorf("uid %d: argv[%d] = %q carries whitespace; nothing here is shell-split", uid, i, arg)
			}
		}
	}
}

// TestDispatchUnderDryRunTouchesNeitherBusNorDisk is the preview guarantee:
// the log line the issue names is emitted, and the runner is never called and
// the home directory is never written, so a screenshot run cannot invent an
// avatar.
func TestDispatchUnderDryRunTouchesNeitherBusNorDisk(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	applier, home := testApplier(t, func(context.Context, []string) error {
		t.Error("the bus was used under --dry-run; a preview must not reach AccountsService")
		return nil
	})

	route, err := applier.Dispatch(context.Background(), Request{
		UID: 1000, ID: "dakotaraptor", IconPath: writeIcon(t),
	})
	if err != nil {
		t.Fatalf("Dispatch under dry-run returned %v", err)
	}
	if route != RouteDryRun {
		t.Errorf("Dispatch under dry-run reported route %q, want %q", route, RouteDryRun)
	}
	if want := "[DRY-RUN] would set avatar to dakotaraptor"; !strings.Contains(logged.String(), want) {
		t.Errorf("dry-run log is missing %q; got:\n%s", want, logged.String())
	}
	assertDirectoryEmpty(t, home)
}

// TestZeroValueApplierStaysSafeUnderDryRun covers the documented zero value.
// Only the preview path is reachable without a runner and a home directory, so
// only that path is asserted; what matters is that an Applier built by struct
// literal does not nil-panic before the gate it is supposed to honor.
func TestZeroValueApplierStaysSafeUnderDryRun(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	var applier Applier
	route, err := applier.Dispatch(context.Background(), Request{UID: 1000, ID: "bob"})
	if err != nil {
		t.Fatalf("a zero-value Applier returned %v under dry-run", err)
	}
	if route != RouteDryRun {
		t.Errorf("a zero-value Applier reported route %q, want %q", route, RouteDryRun)
	}
}

// TestDispatchUsesTheBusWhenAccountsServiceAnswers is the happy path: the bus
// route is taken, and the face files are left alone — writing them as well
// would put two sources of truth in the user's home for one change.
func TestDispatchUsesTheBusWhenAccountsServiceAnswers(t *testing.T) {
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	runner := &recordingRunner{}
	applier, home := testApplier(t, runner.run)
	iconPath := writeIcon(t)

	route, err := applier.Dispatch(context.Background(), Request{UID: 1000, ID: "bob", IconPath: iconPath})
	if err != nil {
		t.Fatalf("Dispatch returned %v", err)
	}
	if route != RouteBusctl {
		t.Errorf("Dispatch reported route %q, want %q", route, RouteBusctl)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("the bus was called %d times, want exactly 1", len(runner.calls))
	}
	if want := SetIconFileArgs(1000, iconPath); !reflect.DeepEqual(runner.calls[0], want) {
		t.Errorf("the bus was called with\n  %q\nwant\n  %q", runner.calls[0], want)
	}
	assertDirectoryEmpty(t, home)
}

// TestDispatchFallsBackToTheFaceFilesWhenTheBusFails covers the fallback the
// issue asks for: a refused or unreachable bus writes both files, with the
// transcoded bytes and a mode the display manager's greeter can read.
func TestDispatchFallsBackToTheFaceFilesWhenTheBusFails(t *testing.T) {
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	runner := &recordingRunner{err: fmt.Errorf("busctl: %w: Call denied", errors.New("exit status 1"))}
	applier, home := testApplier(t, runner.run)

	route, err := applier.Dispatch(context.Background(), Request{
		UID: 1000, ID: "dakotaraptor", IconPath: writeIcon(t),
	})
	if err != nil {
		t.Fatalf("a failed bus call with a working fallback must not be reported as an error; got %v", err)
	}
	if route != RouteFaceFile {
		t.Errorf("Dispatch reported route %q, want %q", route, RouteFaceFile)
	}

	for _, name := range []string{faceIconFileName, faceFileName} {
		path := filepath.Join(home, name)
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading the fallback file %s: %v", name, readErr)
		}
		if !bytes.Equal(got, pngPayload) {
			t.Errorf("%s holds %q, want the transcoded payload %q", name, got, pngPayload)
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat %s: %v", name, statErr)
		}
		if perm := info.Mode().Perm(); perm != faceFileMode {
			t.Errorf("%s has mode %o, want %o: the greeter is not the owning process", name, perm, faceFileMode)
		}
	}
}

// TestDispatchFallsBackWhenBusctlIsAbsent covers the other half of "fails or
// is unreachable": a host with no busctl at all reaches the same fallback
// rather than surfacing a start error to the user.
func TestDispatchFallsBackWhenBusctlIsAbsent(t *testing.T) {
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	runner := &recordingRunner{err: fmt.Errorf("%s: %w", BusctlCommand, errors.New("executable file not found in $PATH"))}
	applier, home := testApplier(t, runner.run)

	route, err := applier.Dispatch(context.Background(), Request{UID: 1000, ID: "katharina", IconPath: writeIcon(t)})
	if err != nil {
		t.Fatalf("Dispatch returned %v with an absent busctl; the face files are the fallback for exactly this", err)
	}
	if route != RouteFaceFile {
		t.Errorf("Dispatch reported route %q, want %q", route, RouteFaceFile)
	}
	if _, statErr := os.Stat(filepath.Join(home, faceIconFileName)); statErr != nil {
		t.Errorf("the fallback did not write %s: %v", faceIconFileName, statErr)
	}
}

// TestDispatchDoesNotFallBackAfterTheContextIsCanceled is the one failure that
// must not reach the fallback. A caller that canceled asked for the action to
// stop; reinterpreting that as "the bus is down" and writing into the user's
// home would perform work for an action that was abandoned, and the face files
// are not undone by a later pick that also succeeds over the bus.
func TestDispatchDoesNotFallBackAfterTheContextIsCanceled(t *testing.T) {
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := &recordingRunner{err: context.Canceled}
	applier, home := testApplier(t, runner.run)

	route, err := applier.Dispatch(ctx, Request{UID: 1000, ID: "utahraptor", IconPath: writeIcon(t)})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dispatch after cancellation returned %v, want an error wrapping context.Canceled", err)
	}
	if route != "" {
		t.Errorf("Dispatch after cancellation reported route %q; a failed dispatch names no route", route)
	}
	assertDirectoryEmpty(t, home)
}

// TestDispatchReportsTheBusFailureWhenTheFallbackAlsoFails keeps both causes
// visible. Reporting only the write error would hide the reason the fallback
// ran at all, and the bus diagnostic is the only place the PolicyKit denial
// appears.
func TestDispatchReportsTheBusFailureWhenTheFallbackAlsoFails(t *testing.T) {
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	busErr := errors.New("Call denied by policy")
	applier, _ := testApplier(t, (&recordingRunner{err: busErr}).run)

	_, err := applier.Dispatch(context.Background(), Request{
		UID: 1000, ID: "bob", IconPath: filepath.Join(t.TempDir(), "missing.png"),
	})
	if err == nil {
		t.Fatal("Dispatch reported success although neither route could have set an avatar")
	}
	if !strings.Contains(err.Error(), busErr.Error()) {
		t.Errorf("error %q does not name the bus failure %q", err, busErr)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error %q does not wrap the unreadable icon's cause", err)
	}
}

// TestDispatchWritesTheOtherFaceFileWhenOneWriteFails holds the "both are
// attempted" decision. The two files serve different readers, so a failure on
// one must not abandon the other and silently leave that reader on the old
// picture.
func TestDispatchWritesTheOtherFaceFileWhenOneWriteFails(t *testing.T) {
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	applier, home := testApplier(t, (&recordingRunner{err: errors.New("Call denied")}).run)

	// A directory where ~/.face belongs makes that one write fail.
	if err := os.Mkdir(filepath.Join(home, faceFileName), 0o755); err != nil {
		t.Fatalf("seeding the obstructed face path: %v", err)
	}

	_, err := applier.Dispatch(context.Background(), Request{UID: 1000, ID: "bob", IconPath: writeIcon(t)})
	if err == nil {
		t.Fatal("Dispatch reported success although one face file could not be written")
	}
	if _, statErr := os.Stat(filepath.Join(home, faceIconFileName)); statErr != nil {
		t.Errorf("%s was not written because %s failed: %v", faceIconFileName, faceFileName, statErr)
	}
}

// TestApplierNeverInvokesAPrivilegedHelper is the issue's verification
// requirement, asserted against the source rather than promised in prose.
//
// An avatar is a change to the caller's own account, authorized by
// AccountsService through org.freedesktop.accounts.change-own-user-data. It
// must never acquire a privileged route: routing it through pkexec would
// broaden a helper's accepted command surface, and mkdir-ing a data/*.policy
// action for a picture is exactly the kind of escalation the repository's
// privilege-boundary invariant exists to prevent.
//
// The scan reads string literals from the AST, the way
// internal/installcheck's privilege gates do, so the word "pkexec" in a
// comment explaining why it is not used does not register as a use.
func TestApplierNeverInvokesAPrivilegedHelper(t *testing.T) {
	forbidden := []string{
		"pkexec",
		"chairlift-ublue-helper",
		"chairlift-updex-helper",
		"helperexec",
		"internal/ublue",
		"internal/updex",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++

		file, parseErr := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", name, parseErr)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, unquoteErr := strconv.Unquote(literal.Value)
			if unquoteErr != nil {
				return true
			}
			for _, marker := range forbidden {
				if strings.Contains(value, marker) {
					t.Errorf("%s spells %q in the literal %s; an avatar dispatch is unprivileged and "+
						"must not reach a helper", name, marker, literal.Value)
				}
			}
			return true
		})
	}

	if scanned == 0 {
		t.Fatal("no non-test Go file was scanned; the gate would pass for the wrong reason")
	}
}

// assertDirectoryEmpty proves a route touched no disk at all, rather than
// merely not writing the two names this package knows about.
func assertDirectoryEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("%s is not empty after a dispatch that should have written nothing: %v", dir, names)
	}
}

// The page shows whatever the running session shows: AccountsService's copy
// wins over the face files, ~/.face.icon over the legacy ~/.face, and a
// directory where a file should be is not a picture.
func TestCurrentPicturePrefersAccountsServiceThenFaceFiles(t *testing.T) {
	icons := t.TempDir()
	home := t.TempDir()
	saved := accountsIconDir
	accountsIconDir = icons
	t.Cleanup(func() { accountsIconDir = saved })

	if got := CurrentPicture("ada", home); got != "" {
		t.Fatalf("CurrentPicture with no files = %q, want empty", got)
	}
	if err := os.Mkdir(filepath.Join(icons, "ada"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := CurrentPicture("ada", home); got != "" {
		t.Fatalf("CurrentPicture returned the directory %q", got)
	}

	steps := []string{
		filepath.Join(home, faceFileName),
		filepath.Join(home, faceIconFileName),
		filepath.Join(icons, "bob"),
	}
	for _, path := range steps {
		if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
		user := "ada"
		if strings.HasSuffix(path, "bob") {
			user = "bob"
		}
		if got := CurrentPicture(user, home); got != path {
			t.Errorf("CurrentPicture(%q) = %q, want %q", user, got, path)
		}
	}
}
