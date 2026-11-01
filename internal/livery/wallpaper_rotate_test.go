package livery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

const enableTimer = "systemctl --user enable --now " + WallpaperRotateTimer

// wallpaperUnits points the user unit directory at a temporary one and
// returns the service and timer paths the package will use.
func wallpaperUnits(t *testing.T) (service, timer string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	service, timer, err := wallpaperUnitPaths()
	if err != nil {
		t.Fatal(err)
	}
	return service, timer
}

func useDryRun(t *testing.T) {
	t.Helper()
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
}

func readUnit(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

func assertAbsent(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s exists (err %v); want it absent", path, err)
		}
	}
}

func TestWallpaperRotationEnableWritesTheTimerAndAServiceNamingTheChannel(t *testing.T) {
	f := newFakeCommands(t)
	servicePath, timerPath := wallpaperUnits(t)

	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatalf("EnableWallpaperRotation() = %v", err)
	}

	service := readUnit(t, servicePath)
	if !strings.Contains(service, " "+WallpaperRotateFlag+` "bluefin"`) || !strings.Contains(service, "Type=oneshot") {
		t.Fatalf("service = %q; want a oneshot running %s for bluefin", service, WallpaperRotateFlag)
	}
	if id, ok := wallpaperUnitChannel(service); !ok || id != "bluefin" {
		t.Fatalf("wallpaperUnitChannel() = %q, %v; want bluefin", id, ok)
	}
	timer := readUnit(t, timerPath)
	if !strings.Contains(timer, "OnCalendar=daily") || !strings.Contains(timer, "WantedBy=timers.target") {
		t.Fatalf("timer = %q; want a daily timer wanted by timers.target", timer)
	}
	if !containsLine(f.calls, "systemctl --user daemon-reload") || !containsLine(f.calls, enableTimer) {
		t.Fatalf("calls = %v; want daemon-reload and %q", f.calls, enableTimer)
	}
	if containsPrefix(f.calls, "gsettings set") {
		t.Fatalf("enabling rotation changed the wallpaper itself: %v", f.calls)
	}
}

func TestWallpaperRotationEnableTwiceLeavesTheSameUnits(t *testing.T) {
	newFakeCommands(t)
	servicePath, timerPath := wallpaperUnits(t)

	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	firstService, firstTimer := readUnit(t, servicePath), readUnit(t, timerPath)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatalf("second EnableWallpaperRotation() = %v", err)
	}

	if readUnit(t, servicePath) != firstService || readUnit(t, timerPath) != firstTimer {
		t.Fatal("enabling an enabled rotation changed its units")
	}
}

func TestWallpaperRotationEnableRefusesAChannelOutsideTheClosedSet(t *testing.T) {
	for _, id := range []string{"", "../evil", "a b", "x\nExecStartPre=/bin/true", "-flag"} {
		f := newFakeCommands(t)
		servicePath, timerPath := wallpaperUnits(t)

		err := EnableWallpaperRotation(context.Background(), id)

		if !errors.Is(err, ErrWallpaperChannelInvalid) {
			t.Fatalf("EnableWallpaperRotation(%q) = %v; want ErrWallpaperChannelInvalid", id, err)
		}
		if len(f.calls) != 0 {
			t.Fatalf("EnableWallpaperRotation(%q) ran %v", id, f.calls)
		}
		assertAbsent(t, servicePath, timerPath)
	}
}

func TestWallpaperRotationEnableUnderDryRunWritesAndRunsNothing(t *testing.T) {
	f := newFakeCommands(t)
	servicePath, timerPath := wallpaperUnits(t)
	useDryRun(t)

	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatalf("EnableWallpaperRotation() = %v", err)
	}

	if len(f.calls) != 0 {
		t.Fatalf("preview ran %v", f.calls)
	}
	assertAbsent(t, servicePath, timerPath)
}

func TestWallpaperRotationFailedEnableLeavesNoUnitBehind(t *testing.T) {
	for _, failed := range []string{"systemctl --user daemon-reload", enableTimer} {
		f := newFakeCommands(t)
		servicePath, timerPath := wallpaperUnits(t)
		f.fail[failed] = errors.New("user manager rejected request")

		if err := EnableWallpaperRotation(context.Background(), "bluefin"); err == nil {
			t.Fatalf("%s failed but enabling reported success", failed)
		}

		assertAbsent(t, servicePath, timerPath)
	}
}

func TestWallpaperRotationFailedEnableRestoresThePreviousChannel(t *testing.T) {
	newFakeCommands(t)
	servicePath, timerPath := wallpaperUnits(t)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	previousService, previousTimer := readUnit(t, servicePath), readUnit(t, timerPath)
	f := newFakeCommands(t)
	f.fail[enableTimer] = errors.New("user manager rejected request")

	if err := EnableWallpaperRotation(context.Background(), "bazzite"); err == nil {
		t.Fatal("a rejected enable reported success")
	}

	if readUnit(t, servicePath) != previousService || readUnit(t, timerPath) != previousTimer {
		t.Fatal("a failed switch to another channel did not restore the rotation that was on")
	}
}

func TestWallpaperRotationDisableWithNothingInstalledRunsNothing(t *testing.T) {
	f := newFakeCommands(t)
	wallpaperUnits(t)

	if err := DisableWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("DisableWallpaperRotation() = %v", err)
	}

	if len(f.calls) != 0 {
		t.Fatalf("disabling an absent rotation ran %v", f.calls)
	}
}

func TestWallpaperRotationDisableStopsTheTimerAndRemovesBothUnits(t *testing.T) {
	newFakeCommands(t)
	servicePath, timerPath := wallpaperUnits(t)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	f := newFakeCommands(t)

	if err := DisableWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("DisableWallpaperRotation() = %v", err)
	}

	assertAbsent(t, servicePath, timerPath)
	if !containsLine(f.calls, "systemctl --user disable --now "+WallpaperRotateTimer) || !containsLine(f.calls, "systemctl --user daemon-reload") {
		t.Fatalf("calls = %v; want the timer disabled and units reloaded", f.calls)
	}
	if err := DisableWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("second DisableWallpaperRotation() = %v", err)
	}
}

func TestWallpaperRotationFailedStopKeepsRotationOn(t *testing.T) {
	newFakeCommands(t)
	servicePath, timerPath := wallpaperUnits(t)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	f := newFakeCommands(t)
	f.fail["systemctl --user disable"] = errors.New("user manager unreachable")

	if err := DisableWallpaperRotation(context.Background()); err == nil {
		t.Fatal("a failed stop reported rotation as off")
	}

	readUnit(t, servicePath)
	readUnit(t, timerPath)
}

func TestWallpaperRotationFailedReloadAfterDisableRestoresAndReenables(t *testing.T) {
	newFakeCommands(t)
	servicePath, timerPath := wallpaperUnits(t)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	f := newFakeCommands(t)
	f.fail["systemctl --user daemon-reload"] = errors.New("user manager unreachable")

	if err := DisableWallpaperRotation(context.Background()); err == nil {
		t.Fatal("a failed reload reported rotation as off")
	}

	if id, ok := wallpaperUnitChannel(readUnit(t, servicePath)); !ok || id != "bluefin" {
		t.Fatalf("restored service names %q, %v; want bluefin", id, ok)
	}
	readUnit(t, timerPath)
	if !containsLine(f.calls, enableTimer) {
		t.Fatalf("calls = %v; want the timer re-enabled after restoring it", f.calls)
	}
}

func TestWallpaperRotationDisableUnderDryRunKeepsTheUnits(t *testing.T) {
	newFakeCommands(t)
	servicePath, timerPath := wallpaperUnits(t)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	f := newFakeCommands(t)
	useDryRun(t)

	if err := DisableWallpaperRotation(context.Background()); err != nil {
		t.Fatalf("DisableWallpaperRotation() = %v", err)
	}

	if len(f.calls) != 0 {
		t.Fatalf("preview ran %v", f.calls)
	}
	readUnit(t, servicePath)
	readUnit(t, timerPath)
}

func TestObserveWallpaperRotationIsOffWithoutRunningAnythingWhenNoTimerExists(t *testing.T) {
	f := newFakeCommands(t)
	wallpaperUnits(t)

	state, err := ObserveWallpaperRotation(context.Background())

	if err != nil || state.On {
		t.Fatalf("ObserveWallpaperRotation() = %+v, %v; want off", state, err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("observing an absent rotation ran %v", f.calls)
	}
}

func TestObserveWallpaperRotationReportsTheRealSchedule(t *testing.T) {
	newFakeCommands(t)
	wallpaperUnits(t)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	f := newFakeCommands(t)
	f.reply["systemctl --user show --timestamp=unix -p UnitFileState"] = "UnitFileState=enabled\nActiveState=active\nNextElapseUSecRealtime=@1762041600\n"
	f.reply["systemctl --user show --timestamp=unix -p Result"] = "Result=success\n"

	state, err := ObserveWallpaperRotation(context.Background())

	if err != nil {
		t.Fatalf("ObserveWallpaperRotation() = %v", err)
	}
	want := WallpaperRotation{On: true, ChannelID: "bluefin", Scheduled: true, Next: time.Unix(1762041600, 0)}
	if state != want {
		t.Fatalf("state = %+v; want %+v", state, want)
	}
}

func TestObserveWallpaperRotationReportsAStoppedTimerAndAFailedPass(t *testing.T) {
	newFakeCommands(t)
	wallpaperUnits(t)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	f := newFakeCommands(t)
	f.reply["systemctl --user show --timestamp=unix -p UnitFileState"] = "UnitFileState=disabled\nActiveState=inactive\nNextElapseUSecRealtime=\n"
	f.reply["systemctl --user show --timestamp=unix -p Result"] = "Result=exit-code\n"

	state, err := ObserveWallpaperRotation(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if !state.On || state.Scheduled || !state.LastRunFailed || !state.Next.IsZero() {
		t.Fatalf("state = %+v; want on, not scheduled, last run failed, no next time", state)
	}
}

func TestObserveWallpaperRotationReturnsAFailedReadAsAnError(t *testing.T) {
	newFakeCommands(t)
	wallpaperUnits(t)
	if err := EnableWallpaperRotation(context.Background(), "bluefin"); err != nil {
		t.Fatal(err)
	}
	f := newFakeCommands(t)
	f.fail["systemctl --user show"] = errors.New("user manager unreachable")

	if _, err := ObserveWallpaperRotation(context.Background()); err == nil {
		t.Fatal("a failed read was reported as a state")
	}
}

func TestNextWallpaperAdvancesWrapsAndStartsAtTheFirstPicture(t *testing.T) {
	channel := WallpaperChannel{ID: "c", Wallpapers: []Wallpaper{
		{Name: "A", File: "/b/c/a.png"}, {Name: "B", File: "/b/c/b.png"}, {Name: "C", File: "/b/c/c.png"},
	}}
	for current, want := range map[string]string{
		"file:///b/c/a.png":     "B",
		"file:///b/c/c.png":     "A",
		"file:///elsewhere.png": "A",
		"":                      "A",
	} {
		got, ok := NextWallpaper(channel, current)
		if !ok || got.Name != want {
			t.Errorf("NextWallpaper(%q) = %q, %v; want %q", current, got.Name, ok, want)
		}
	}
	if _, ok := NextWallpaper(WallpaperChannel{}, ""); ok {
		t.Error("an empty channel offered a picture")
	}
}

// statefulBackground makes the fake gsettings keep what is written to GNOME's
// background schema, so a pass reads the current picture, writes the next,
// and confirms it by reading it back.
func statefulBackground(t *testing.T, currentFile string) map[string]string {
	t.Helper()
	values := map[string]string{pictureOptionsKey: "zoom"}
	if currentFile != "" {
		values[pictureURIKey] = BackgroundURI(currentFile)
		values[pictureURIDarkKey] = BackgroundURI(currentFile)
	}
	inner := runCommand
	runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		out, err := inner(ctx, name, args...)
		if name != "gsettings" || len(args) < 3 || args[1] != NativeBackgroundSchema || err != nil {
			return out, err
		}
		switch args[0] {
		case "get":
			return "'" + values[args[2]] + "'\n", nil
		case "set":
			values[args[2]] = strings.Trim(args[3], `"'`)
		}
		return "", nil
	}
	t.Cleanup(func() { runCommand = inner })
	return values
}

func useWallpaperDataDirs(t *testing.T, dirs ...string) {
	t.Helper()
	original := wallpaperDataDirs
	wallpaperDataDirs = func() []string { return dirs }
	t.Cleanup(func() { wallpaperDataDirs = original })
}

func TestRotateWallpaperMovesToTheNextPictureOfTheChannel(t *testing.T) {
	dataDir := installedChannels(t)
	useWallpaperDataDirs(t, dataDir)
	january := filepath.Join(dataDir, "backgrounds", "bluefin", "01-bluefin.xml")
	february := filepath.Join(dataDir, "backgrounds", "bluefin", "02-bluefin.png")
	for current, want := range map[string]string{january: february, february: january, "": january} {
		newFakeCommands(t)
		values := statefulBackground(t, current)

		if err := RotateWallpaper(context.Background(), "bluefin"); err != nil {
			t.Fatalf("RotateWallpaper() from %q = %v", current, err)
		}

		for _, key := range []string{pictureURIKey, pictureURIDarkKey} {
			if values[key] != BackgroundURI(want) {
				t.Fatalf("from %q, %s = %q; want %q", current, key, values[key], BackgroundURI(want))
			}
		}
	}
}

func TestRotateWallpaperReportsAnUninstalledChannelWithoutWriting(t *testing.T) {
	dataDir := installedChannels(t)
	useWallpaperDataDirs(t, dataDir)
	f := newFakeCommands(t)
	statefulBackground(t, "")

	err := RotateWallpaper(context.Background(), "framework")

	if !errors.Is(err, ErrWallpaperChannelMissing) {
		t.Fatalf("RotateWallpaper() = %v; want ErrWallpaperChannelMissing", err)
	}
	if containsPrefix(f.calls, "gsettings set") {
		t.Fatalf("a missing channel still wrote: %v", f.calls)
	}
}

func TestRotateWallpaperOffGNOMEReadsAndWritesNothing(t *testing.T) {
	useDesktop(t, deskenv.KDE)
	useWallpaperDataDirs(t, installedChannels(t))
	f := newFakeCommands(t)

	if err := RotateWallpaper(context.Background(), "bluefin"); err != nil {
		t.Fatalf("RotateWallpaper() = %v; want a quiet skip", err)
	}

	if len(f.calls) != 0 {
		t.Fatalf("a pass on another desktop ran %v", f.calls)
	}
}

func TestRotateWallpaperFailsWhenTheDesktopCannotBeRead(t *testing.T) {
	useWallpaperDataDirs(t, installedChannels(t))
	f := newFakeCommands(t)
	f.reply["gsettings get org.gnome.desktop.background"] = "dconf is unreachable"
	f.fail["gsettings get org.gnome.desktop.background"] = errors.New("exit status 1")

	if err := RotateWallpaper(context.Background(), "bluefin"); err == nil {
		t.Fatal("a failed desktop read was reported as a rotation")
	}
	if containsPrefix(f.calls, "gsettings set") {
		t.Fatalf("a failed read still wrote: %v", f.calls)
	}
}

func TestRotateWallpaperUnderDryRunWritesNothing(t *testing.T) {
	dataDir := installedChannels(t)
	useWallpaperDataDirs(t, dataDir)
	f := newFakeCommands(t)
	statefulBackground(t, filepath.Join(dataDir, "backgrounds", "bluefin", "01-bluefin.xml"))
	useDryRun(t)

	if err := RotateWallpaper(context.Background(), "bluefin"); err != nil {
		t.Fatalf("RotateWallpaper() = %v", err)
	}

	if containsPrefix(f.calls, "gsettings set") {
		t.Fatalf("preview wrote: %v", f.calls)
	}
}

func TestRotateWallpaperRefusesAChannelOutsideTheClosedSet(t *testing.T) {
	f := newFakeCommands(t)

	if err := RotateWallpaper(context.Background(), "../bluefin"); !errors.Is(err, ErrWallpaperChannelInvalid) {
		t.Fatalf("RotateWallpaper() = %v; want ErrWallpaperChannelInvalid", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("an invalid channel ran %v", f.calls)
	}
}
