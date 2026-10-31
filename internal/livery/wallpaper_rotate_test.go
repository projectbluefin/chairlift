package livery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// wallpaperRotationState wires a fake gsettings layer (the given ChairLift
// wallpaper keys), a fixture wallpaper tree, and a live GNOME background
// schema, so RotateWallpaper sees an available native background.
func wallpaperRotationState(t *testing.T, values, files map[string]string) *fakeCommands {
	t.Helper()
	f := newFakeCommands(t)
	var b strings.Builder
	for key, value := range values {
		b.WriteString(SchemaID + " " + key + " " + value + "\n")
	}
	f.reply["gsettings list-recursively "+SchemaID] = b.String()
	// GNOME owns the background schema, so NativeBackgroundAvailable is true.
	f.reply["gsettings get org.gnome.desktop.background picture-uri"] = "''"
	withWallpaperRoot(t, files)
	return f
}

func TestRotateWallpaperIsNoOpWhenDisabled(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{KeyWallpaperRotate: "false", KeyWallpaperChannel: "bluefin", KeyWallpaperImage: "bluefin/aurora.png"},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	if err := RotateWallpaper(context.Background()); err != nil {
		t.Fatalf("disabled rotation should be a no-op: %v", err)
	}
	if f.sawPrefix("gsettings set org.gnome.desktop.background") {
		t.Fatal("disabled rotation must not change the background")
	}
}

func TestRotateWallpaperAdvancesToNextImage(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{KeyWallpaperRotate: "true", KeyWallpaperChannel: "bluefin", KeyWallpaperImage: "bluefin/aurora.png"},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	if err := RotateWallpaper(context.Background()); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}
	if !f.sawPrefix("gsettings set org.gnome.desktop.background picture-uri ") {
		t.Fatal("rotation should apply the next wallpaper to the background schema")
	}
	if !f.sawPrefix("gsettings set " + SchemaID + " " + KeyWallpaperImage) {
		t.Fatal("rotation should persist the new image id")
	}
}

func TestRotateWallpaperIsNoOpWithoutNativeBackground(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{KeyWallpaperRotate: "true", KeyWallpaperChannel: "bluefin", KeyWallpaperImage: "bluefin/aurora.png"},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	// A missing schema makes NativeBackgroundAvailable false, so there is
	// nothing to rotate. The fake returns the gsettings error text alongside a
	// nonzero exit so isMissingSchema recognises it.
	f.reply["gsettings get org.gnome.desktop.background picture-uri"] = "No such schema"
	f.fail["gsettings get org.gnome.desktop.background picture-uri"] = errors.New("gsettings: not installed")
	if err := RotateWallpaper(context.Background()); err != nil {
		t.Fatalf("rotation without a native background should be a no-op: %v", err)
	}
	if f.sawPrefix("gsettings set org.gnome.desktop.background") {
		t.Fatal("rotation must not touch the background when the schema is absent")
	}
}

// TestRotateWallpaperSurfacesAProbeFailure pins that a transient GNOME
// background probe failure (a hung or broken settings tool) is returned from
// RotateWallpaper rather than swallowed as a no-op, so the unit exits
// non-zero instead of silently hiding the feature (#230: detection failures
// are errors, not "unsupported").
func TestRotateWallpaperSurfacesAProbeFailure(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{KeyWallpaperRotate: "true", KeyWallpaperChannel: "bluefin", KeyWallpaperImage: "bluefin/aurora.png"},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	// A probe failure that is not a missing schema: the tool exits nonzero
	// with output that is neither "No such schema" nor "not installed", so
	// gsettingsGet returns a wrapped error and NativeBackgroundAvailable
	// surfaces it rather than reporting a genuine false.
	f.reply["gsettings get org.gnome.desktop.background picture-uri"] = "internal error"
	f.fail["gsettings get org.gnome.desktop.background picture-uri"] = errors.New("gsettings: connection hung")
	if _, err := NativeBackgroundAvailable(context.Background()); err == nil {
		t.Fatal("a transient probe failure must be returned, not swallowed as unavailable")
	}
	if err := RotateWallpaper(context.Background()); err == nil {
		t.Fatal("rotation must return a probe failure so the unit exits non-zero")
	}
	if f.sawPrefix("gsettings set org.gnome.desktop.background") {
		t.Fatal("rotation must not touch the background when the probe failed")
	}
}

// TestNativeBackgroundUnavailableOffGNOME pins that a non-GNOME session never
// gets a GNOME background write, even when the schema is installed (Plasma on
// Fedora-based images ships gsettings-desktop-schemas): selection reports
// unavailable and rotation leaves the background alone.
func TestNativeBackgroundUnavailableOffGNOME(t *testing.T) {
	for _, desktop := range []deskenv.Desktop{deskenv.KDE, deskenv.Unknown} {
		t.Run(desktop.String(), func(t *testing.T) {
			f := wallpaperRotationState(t,
				map[string]string{KeyWallpaperRotate: "true", KeyWallpaperChannel: "bluefin", KeyWallpaperImage: "bluefin/aurora.png"},
				map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
			)
			useDesktop(t, desktop)
			available, err := NativeBackgroundAvailable(context.Background())
			if available || err != nil {
				t.Fatalf("native background must be unavailable on %v even with the schema present", desktop)
			}
			if err := RotateWallpaper(context.Background()); err != nil {
				t.Fatalf("rotation off GNOME should be a no-op: %v", err)
			}
			if f.sawPrefix("gsettings set org.gnome.desktop.background") {
				t.Fatalf("rotation must not touch the GNOME background on %v", desktop)
			}
		})
	}
}

func TestConfigureWallpaperRotationRespectsDryRun(t *testing.T) {
	f := newFakeCommands(t)
	useTempDataHome(t)
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	wallpaperRotationState(t, map[string]string{KeyWallpaperRotate: "false"}, map[string]string{})
	if _, err := ConfigureWallpaperRotation(context.Background(), State{WallpaperRotate: true}); err != nil {
		t.Fatalf("dry-run configure should succeed: %v", err)
	}
	if f.sawPrefix("systemctl --user enable") {
		t.Fatal("dry-run configure must not enable the rotation unit")
	}
}

// TestRotateWallpaperIgnoresTheIconRotationToken pins the separate
// idempotence key: an icon pass that already stamped last-rotation-token for
// this session must not make the wallpaper pass skip, and the wallpaper pass
// records its own key rather than the icon one.
func TestRotateWallpaperIgnoresTheIconRotationToken(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{
			KeyWallpaperRotate: "true", KeyWallpaperChannel: "'bluefin'", KeyWallpaperImage: "'bluefin/aurora.png'",
			KeyRotationToken: "'session:12345'",
		},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	f.reply["systemctl --user show -p ActiveEnterTimestampMonotonic"] = "12345\n"

	if err := RotateWallpaper(context.Background()); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}
	if !f.sawPrefix("gsettings set org.gnome.desktop.background picture-uri ") {
		t.Fatalf("the icon pass's token suppressed the wallpaper pass: %v", f.calls)
	}
	if !f.sawPrefix(setCall(KeyWallpaperRotationToken, "session:12345")) {
		t.Errorf("wallpaper pass did not record its own token: %v", f.calls)
	}
	if f.sawPrefix("gsettings set " + SchemaID + " " + KeyRotationToken + " ") {
		t.Errorf("wallpaper pass overwrote the icon rotation token: %v", f.calls)
	}
}

// TestRotateWallpaperRunsOncePerSession is the wallpaper pass's own
// idempotence: a matching wallpaper token makes a second pass a no-op.
func TestRotateWallpaperRunsOncePerSession(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{
			KeyWallpaperRotate: "true", KeyWallpaperChannel: "'bluefin'", KeyWallpaperImage: "'bluefin/aurora.png'",
			KeyWallpaperRotationToken: "'session:12345'",
		},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	f.reply["systemctl --user show -p ActiveEnterTimestampMonotonic"] = "12345\n"

	if err := RotateWallpaper(context.Background()); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}
	if f.sawPrefix("gsettings set org.gnome.desktop.background") {
		t.Errorf("a second pass in the same session advanced the wallpaper: %v", f.calls)
	}
}

// wallpaperUnitFixture points the unit directory and executable at test
// values, and returns the service and timer paths.
func wallpaperUnitFixture(t *testing.T) (servicePath, timerPath string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GSETTINGS_SCHEMA_DIR", "")
	original := executablePath
	executablePath = func() (string, error) { return "/usr/bin/chairlift", nil }
	t.Cleanup(func() { executablePath = original })
	var err error
	if servicePath, err = wallpaperUnitPath(); err != nil {
		t.Fatal(err)
	}
	if timerPath, err = wallpaperTimerPath(); err != nil {
		t.Fatal(err)
	}
	return servicePath, timerPath
}

func TestWallpaperRotationInstallWritesUnitsRunningTheFlag(t *testing.T) {
	f := newFakeCommands(t)
	servicePath, timerPath := wallpaperUnitFixture(t)

	if err := InstallWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("InstallWallpaperRotation: %v", err)
	}
	service, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}
	if want := `ExecStart="/usr/bin/chairlift" ` + WallpaperRotateFlag; !strings.Contains(string(service), want) {
		t.Errorf("service does not run %q:\n%s", want, service)
	}
	// The service is enabled under graphical-session.target, so each login
	// runs it with that session's token; no timer is written, because a
	// timer can fire before the session and fall back to the boot token.
	if !f.sawPrefix("systemctl --user enable " + WallpaperServiceName) {
		t.Errorf("service not enabled: %v", f.calls)
	}
	if _, err := os.Stat(timerPath); !os.IsNotExist(err) {
		t.Errorf("a timer was written: %v", err)
	}
	if f.sawPrefix("systemctl --user enable "+WallpaperTimerName) || f.sawPrefix("systemctl --user start") {
		t.Errorf("install must only enable the service: %v", f.calls)
	}
}

// TestWallpaperRotationInstallRestoresPriorUnitsOnFailure covers the rollback:
// a failed enable leaves a previously-installed service byte-for-byte as it
// was.
func TestWallpaperRotationInstallRestoresPriorUnitsOnFailure(t *testing.T) {
	f := newFakeCommands(t)
	servicePath, _ := wallpaperUnitFixture(t)
	prior := []byte("[Service]\nExecStart=/old/chairlift --rotate-wallpaper\n")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servicePath, prior, 0o644); err != nil {
		t.Fatal(err)
	}
	f.fail["systemctl --user enable"] = errors.New("exit status 1")

	if err := InstallWallpaperRotation(context.Background()); err == nil {
		t.Fatal("InstallWallpaperRotation reported success after enable failed")
	}
	got, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("prior service was not restored: %v", err)
	}
	if string(got) != string(prior) {
		t.Errorf("prior service not restored:\n got %q\nwant %q", got, prior)
	}
}

func TestRemoveWallpaperRotationDeletesUnits(t *testing.T) {
	f := newFakeCommands(t)
	servicePath, timerPath := wallpaperUnitFixture(t)
	if err := InstallWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("InstallWallpaperRotation: %v", err)
	}
	// A timer left by an earlier build is cleaned up alongside the service.
	if err := os.WriteFile(timerPath, []byte("[Timer]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("RemoveWallpaperRotation: %v", err)
	}
	for _, path := range []string{servicePath, timerPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still present after remove: %v", path, err)
		}
	}
	if !f.sawPrefix("systemctl --user disable " + WallpaperServiceName) {
		t.Errorf("service not disabled: %v", f.calls)
	}
	if !f.sawPrefix("systemctl --user disable " + WallpaperTimerName) {
		t.Errorf("leftover timer not disabled: %v", f.calls)
	}
}

// TestRemoveWallpaperRotationSkipsAbsentTimer pins that removal never asks
// systemd to disable a timer that was never installed.
func TestRemoveWallpaperRotationSkipsAbsentTimer(t *testing.T) {
	f := newFakeCommands(t)
	wallpaperUnitFixture(t)
	if err := InstallWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("InstallWallpaperRotation: %v", err)
	}
	if err := RemoveWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("RemoveWallpaperRotation: %v", err)
	}
	if f.sawPrefix("systemctl --user disable " + WallpaperTimerName) {
		t.Errorf("disabled a timer that does not exist: %v", f.calls)
	}
}

// TestRemoveWallpaperRotationRestoresUnitsOnFailure covers the inverse
// rollback: a failed disable puts both units back as they were.
func TestRemoveWallpaperRotationRestoresUnitsOnFailure(t *testing.T) {
	f := newFakeCommands(t)
	servicePath, timerPath := wallpaperUnitFixture(t)
	if err := InstallWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("InstallWallpaperRotation: %v", err)
	}
	if err := os.WriteFile(timerPath, []byte("[Timer]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, _ := os.ReadFile(servicePath)
	timer, _ := os.ReadFile(timerPath)
	f.fail["systemctl --user disable "+WallpaperServiceName] = errors.New("exit status 1")

	if err := RemoveWallpaperRotation(context.Background()); err == nil {
		t.Fatal("RemoveWallpaperRotation reported success after disable failed")
	}
	for path, want := range map[string][]byte{servicePath: service, timerPath: timer} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s not restored: %v", path, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s not restored byte-for-byte:\n got %q\nwant %q", path, got, want)
		}
	}
}
