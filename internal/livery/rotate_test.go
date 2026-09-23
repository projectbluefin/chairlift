package livery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/deskenv"
)

// rotateState installs a fake gsettings layer holding the given key/value
// pairs, so Rotate's Load sees a state without a live dconf.
//
// The recorder is returned because what rotation *wrote* is the assertion:
// every selection change leaves a `gsettings set` behind.
func rotateState(t *testing.T, values map[string]string) *fakeCommands {
	t.Helper()
	f := newFakeCommands(t)
	var b strings.Builder
	for key, value := range values {
		b.WriteString(SchemaID + " " + key + " " + value + "\n")
	}
	f.reply["gsettings list-recursively "+SchemaID] = b.String()

	// The gaming probe reads the booted image's descriptor, which a test host
	// does not have; pin it so the panel default is the ordinary one.
	originalGaming := detectGaming
	detectGaming = func() bool { return false }
	t.Cleanup(func() { detectGaming = originalGaming })

	useTempDataHome(t)
	return f
}

// useLoopbackCNCFFetch answers the artwork fetch from a loopback server, so a
// dock rotation never leaves the machine.
func useLoopbackCNCFFetch(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<svg viewBox="0 0 24 24"><path d="M1 1h2v2H1z"/></svg>`))
	}))
	t.Cleanup(server.Close)

	original := Fetch
	Fetch = func(ctx context.Context, _ string) ([]byte, error) {
		return httpFetch(ctx, server.URL)
	}
	t.Cleanup(func() { Fetch = original })
}

// setCall is the recorded write of one settings key.
func setCall(key, value string) string {
	return "gsettings set " + SchemaID + " " + key + " \"" + value + "\""
}

// TestRotateAdvancesEnabledCatalogSections is the happy path: the one code
// path the systemd unit runs headlessly, with both sections rotating.
func TestRotateAdvancesEnabledCatalogSections(t *testing.T) {
	useLoopbackCNCFFetch(t)
	panelID := Foundations()[0].ID
	f := rotateState(t, map[string]string{
		KeyPanelEnabled: "true", KeyPanelRotate: "true", KeyPanelID: "'" + panelID + "'",
		KeyDockEnabled: "true", KeyDockRotate: "true", KeyDockID: "'" + DefaultCNCFID + "'",
	})

	if err := Rotate(context.Background()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if want := setCall(KeyPanelID, NextID(panelID)); !f.sawPrefix(want) {
		t.Errorf("panel selection was not advanced; wanted %q in %v", want, f.calls)
	}
	if want := setCall(KeyDockID, NextCNCFID(DefaultCNCFID)); !f.sawPrefix(want) {
		t.Errorf("dock selection was not advanced; wanted %q in %v", want, f.calls)
	}
}

// TestRotateLeavesACustomMarkAlone is the guard this package's doc comment
// promises: a section pinned to the user's own SVG is a choice, not a cycle,
// and NextID/NextCNCFID would map that sentinel onto the first catalog entry.
func TestRotateLeavesACustomMarkAlone(t *testing.T) {
	f := rotateState(t, map[string]string{
		KeyPanelEnabled: "true", KeyPanelRotate: "true",
		KeyPanelID: "'" + CustomID + "'", KeyPanelCustom: "'/home/user/mark.svg'",
		KeyDockEnabled: "true", KeyDockRotate: "true",
		KeyDockID: "'" + CustomID + "'", KeyDockCustom: "'/home/user/files.svg'",
	})

	if err := Rotate(context.Background()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	for _, key := range []string{KeyPanelID, KeyDockID} {
		if f.sawPrefix("gsettings set " + SchemaID + " " + key) {
			t.Errorf("rotation overwrote the custom %s selection: %v", key, f.calls)
		}
	}
}

// TestRotateSkipsASectionThatIsNotEnabled asserts rotation changes which mark
// is selected without deciding whether ChairLift manages that surface.
func TestRotateSkipsASectionThatIsNotEnabled(t *testing.T) {
	panelID := Foundations()[0].ID
	f := rotateState(t, map[string]string{
		KeyPanelEnabled: "false", KeyPanelRotate: "true", KeyPanelID: "'" + panelID + "'",
		KeyDockEnabled: "false", KeyDockRotate: "true", KeyDockID: "'" + DefaultCNCFID + "'",
	})

	if err := Rotate(context.Background()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	for _, key := range []string{KeyPanelID, KeyDockID} {
		if f.sawPrefix("gsettings set " + SchemaID + " " + key) {
			t.Errorf("rotation wrote %s for a disabled section: %v", key, f.calls)
		}
	}
}

// TestRotateDoesNothingWithBothSwitchesOff asserts the unit's pass is inert
// when no section rotates, rather than advancing anything by default.
func TestRotateDoesNothingWithBothSwitchesOff(t *testing.T) {
	f := rotateState(t, map[string]string{
		KeyPanelEnabled: "true", KeyPanelRotate: "false", KeyPanelID: "'" + Foundations()[0].ID + "'",
		KeyDockEnabled: "true", KeyDockRotate: "false", KeyDockID: "'" + DefaultCNCFID + "'",
	})

	if err := Rotate(context.Background()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if f.sawPrefix("gsettings set") {
		t.Errorf("rotation wrote with both switches off: %v", f.calls)
	}
}

// TestRotateRunsOncePerSession pins the idempotence key: a pass whose token
// matches the stored one does nothing, so restarting the unit or running the
// flag by hand cannot double-advance the selection.
func TestRotateRunsOncePerSession(t *testing.T) {
	panelID := Foundations()[0].ID
	f := rotateState(t, map[string]string{
		KeyPanelEnabled: "true", KeyPanelRotate: "true", KeyPanelID: "'" + panelID + "'",
		KeyRotationToken: "'session:12345'",
	})
	f.reply["systemctl --user show -p ActiveEnterTimestampMonotonic"] = "12345\n"

	if err := Rotate(context.Background()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if f.sawPrefix("gsettings set") {
		t.Errorf("a second pass in the same session advanced the selection: %v", f.calls)
	}
}

// TestRotateRetriesDockFetchWhileNetworkIsDown covers a login on a host whose
// connectivity arrives after the graphical session does. The unit cannot
// order against network-online.target from the user manager, and the token is
// recorded even for a failed pass, so without the retry the dock would simply
// not rotate that session and say so only in the journal.
func TestRotateRetriesDockFetchWhileNetworkIsDown(t *testing.T) {
	original := Fetch
	t.Cleanup(func() { Fetch = original })
	var attempts int
	Fetch = func(_ context.Context, _ string) ([]byte, error) {
		attempts++
		if attempts < 3 {
			return nil, fmt.Errorf("livery: reaching cncf/artwork: %w",
				&net.OpError{Op: "dial", Err: errors.New("network is unreachable")})
		}
		return []byte(`<svg viewBox="0 0 24 24"><path d="M1 1h2v2H1z"/></svg>`), nil
	}

	originalDelays := rotateRetryDelays
	rotateRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { rotateRetryDelays = originalDelays })

	f := rotateState(t, map[string]string{
		KeyDockEnabled: "true", KeyDockRotate: "true", KeyDockID: "'" + DefaultCNCFID + "'",
	})

	if err := Rotate(context.Background()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected the fetch to be retried until it succeeded, got %d attempts", attempts)
	}
	if want := setCall(KeyDockID, NextCNCFID(DefaultCNCFID)); !f.sawPrefix(want) {
		t.Errorf("dock selection was not advanced after the network came up; wanted %q in %v", want, f.calls)
	}
}

// TestRotateDoesNotRetryAPermanentFailure keeps the retry from turning a
// withdrawn mark into minutes of pointless waiting at every login.
func TestRotateDoesNotRetryAPermanentFailure(t *testing.T) {
	original := Fetch
	t.Cleanup(func() { Fetch = original })
	var attempts int
	Fetch = func(_ context.Context, _ string) ([]byte, error) {
		attempts++
		return nil, ErrIconNotFound
	}

	originalDelays := rotateRetryDelays
	rotateRetryDelays = []time.Duration{time.Minute}
	t.Cleanup(func() { rotateRetryDelays = originalDelays })

	rotateState(t, map[string]string{
		KeyDockEnabled: "true", KeyDockRotate: "true", KeyDockID: "'" + DefaultCNCFID + "'",
	})

	if err := Rotate(context.Background()); err == nil {
		t.Fatal("Rotate reported success for a mark the artwork repository does not publish")
	}
	if attempts != 1 {
		t.Errorf("a permanent failure was retried %d times", attempts)
	}
}

// TestRotationUnitQuotesAwkwardPaths pins the escaping the unit file needs.
//
// A unit file is neither shell nor free text: a space in ExecStart becomes a
// second argument, a `%` starts a specifier systemd expands, and both values
// here are runtime paths — os.Executable() and $GSETTINGS_SCHEMA_DIR — that a
// source build can perfectly well place under a directory containing either.
func TestRotationUnitQuotesAwkwardPaths(t *testing.T) {
	newFakeCommands(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GSETTINGS_SCHEMA_DIR", "/home/a b/100% done/schemas")

	original := executablePath
	executablePath = func() (string, error) { return "/home/a b/100% done/chairlift", nil }
	t.Cleanup(func() { executablePath = original })

	if err := InstallRotation(context.Background()); err != nil {
		t.Fatalf("InstallRotation: %v", err)
	}
	path, err := UnitPath()
	if err != nil {
		t.Fatalf("UnitPath: %v", err)
	}
	unit, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the unit: %v", err)
	}
	wantExec := `ExecStart="/home/a b/100%% done/chairlift" ` + RotateFlag
	if !strings.Contains(string(unit), wantExec) {
		t.Errorf("ExecStart is not quoted and percent-escaped:\n%s", unit)
	}
	wantEnv := `Environment="GSETTINGS_SCHEMA_DIR=/home/a b/100%% done/schemas"`
	if !strings.Contains(string(unit), wantEnv) {
		t.Errorf("Environment is not quoted and percent-escaped:\n%s", unit)
	}
}

// TestRotationUnitRefusesANewlineInAPath covers the one case quoting cannot
// carry: a unit directive ends at the newline, so a path containing one would
// write a further directive into the file rather than a path.
func TestRotationUnitRefusesANewlineInAPath(t *testing.T) {
	newFakeCommands(t)
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("GSETTINGS_SCHEMA_DIR", "")

	original := executablePath
	executablePath = func() (string, error) {
		return "/tmp/chairlift\nExecStartPost=/usr/bin/id", nil
	}
	t.Cleanup(func() { executablePath = original })

	if err := InstallRotation(context.Background()); err == nil {
		t.Fatal("InstallRotation accepted an executable path containing a newline")
	}
	path, err := UnitPath()
	if err != nil {
		t.Fatalf("UnitPath: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a rejected path still wrote a unit file")
	}
}

// TestRotateSkipsASurfaceTheSessionCannotResolve covers a user whose stored
// rotation state was written under GNOME and who then logs into Plasma. The
// panel surface has no Plasma equivalent, so the pass must skip it rather
// than turn every login into a non-zero exit from the rotation unit.
func TestRotateSkipsASurfaceTheSessionCannotResolve(t *testing.T) {
	useLoopbackCNCFFetch(t)
	panelID := Foundations()[0].ID
	f := rotateState(t, map[string]string{
		KeyPanelEnabled: "true", KeyPanelRotate: "true", KeyPanelID: "'" + panelID + "'",
		KeyDockEnabled: "true", KeyDockRotate: "true", KeyDockID: "'" + DefaultCNCFID + "'",
	})
	useDesktop(t, deskenv.KDE)

	if err := Rotate(context.Background()); err != nil {
		t.Fatalf("Rotate on KDE: %v", err)
	}
	if f.sawPrefix("gsettings set " + SchemaID + " " + KeyPanelID) {
		t.Errorf("rotation advanced the panel on a desktop that has no panel surface: %v", f.calls)
	}
	if want := setCall(KeyDockID, NextCNCFID(DefaultCNCFID)); !f.sawPrefix(want) {
		t.Errorf("the dock, which KDE does have, was not advanced; wanted %q in %v", want, f.calls)
	}
}
