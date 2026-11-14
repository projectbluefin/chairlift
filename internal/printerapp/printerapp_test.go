package printerapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestFamiliesCoversEveryDriverFamily(t *testing.T) {
	wantIDs := []string{"ghostscript", "ps", "hplip", "gutenprint"}
	got := Families()
	if len(got) != len(wantIDs) {
		t.Fatalf("Families() returned %d families, want %d", len(got), len(wantIDs))
	}
	seen := map[string]bool{}
	for _, f := range got {
		if seen[f.ID] {
			t.Errorf("duplicate family ID %q", f.ID)
		}
		seen[f.ID] = true
		// Built-in families run their own repository's moving :stable tag;
		// Enable verifies and pins this reference before installing the unit.
		if want := "ghcr.io/projectbluefin/" + f.ID + "-printer-app:stable"; f.Image != want {
			t.Errorf("family %q image = %q, want %q", f.ID, f.Image, want)
		}
	}
	for _, id := range wantIDs {
		if !seen[id] {
			t.Errorf("Families() is missing the %q family", id)
		}
	}
}

func TestEveryAppRendersAStartableUnit(t *testing.T) {
	apps := []App{
		Select(Families()[0]),
		{Family: Families()[1], Name: "office-printer"},
		{Family: Families()[2], Name: "photo-printer"},
	}

	for _, app := range apps {
		unit := RenderUnit(app)

		for _, required := range []string{
			"[Container]",
			"Image=" + app.Family.Image,
			"ContainerName=" + app.ContainerName(),
			"WantedBy=default.target",
			"PublishPort=127.0.0.1:" + strconv.Itoa(app.Port()) + ":" + strconv.Itoa(app.Port()),
			"Environment=PORT=" + strconv.Itoa(app.Port()),
			"UserNS=keep-id:uid=65532,gid=65532",
		} {
			if !strings.Contains(unit, required) {
				t.Errorf("%s unit is missing %q:\n%s", app.UnitName(), required, unit)
			}
		}

		// ADR-0020: PAPPL's web admin is unauthenticated, so the unit never
		// joins the host network and never publishes on anything but loopback.
		if strings.Contains(unit, "Network=host") {
			t.Errorf("%s unit uses host networking:\n%s", app.UnitName(), unit)
		}
		for _, line := range strings.Split(unit, "\n") {
			if strings.HasPrefix(line, "PublishPort=") && !strings.HasPrefix(line, "PublishPort=127.0.0.1:") {
				t.Errorf("%s unit publishes beyond loopback: %q", app.UnitName(), line)
			}
		}
	}
}

func TestPrinterStateFollowsXDGDataHome(t *testing.T) {
	for _, name := range []string{"default", "relative", "custom"} {
		t.Run(name, func(t *testing.T) {
			home, _ := stubUnitDir(t)
			dataHome := filepath.Join(home, ".local", "share")
			volumeHome := "%h/.local/share"
			switch name {
			case "relative":
				t.Setenv("XDG_DATA_HOME", "relative/data")
			case "custom":
				dataHome = filepath.Join(home, "data with spaces and 100%")
				t.Setenv("XDG_DATA_HOME", dataHome)
				volumeHome = strings.ReplaceAll(dataHome, "%", "%%")
			}
			app := Select(Families()[0])
			if err := Enable(context.Background(), app); err != nil {
				t.Fatal(err)
			}
			wantDir := filepath.Join(dataHome, "chairlift", "printers", "ghostscript", "ghostscript")
			info, err := os.Stat(wantDir)
			if err != nil || !info.IsDir() {
				t.Fatalf("printer data directory %s: %v", wantDir, err)
			}
			unit, err := os.ReadFile(filepath.Join(home, app.UnitName()))
			if err != nil {
				t.Fatal(err)
			}
			wantVolume := fmt.Sprintf("Volume=%q\n", volumeHome+"/chairlift/printers/ghostscript/ghostscript:/var/lib/ghostscript-printer-app:z")
			if !strings.Contains(string(unit), wantVolume) {
				t.Fatalf("unit does not mount the created data directory: %s", unit)
			}
			if _, err := os.Stat(filepath.Join(home, "printer-workspaces")); !os.IsNotExist(err) {
				t.Fatalf("legacy home directory was created: %v", err)
			}
		})
	}
}

func TestHostVolumeDirNeedsHomeOnlyWithoutXDGDataHome(t *testing.T) {
	home, _ := stubUnitDir(t)
	noHome := errors.New("home unavailable")
	userHomeDir = func() (string, error) { return "", noHome }
	app := Select(Families()[0])
	if _, err := app.HostVolumeDir(); !errors.Is(err, noHome) {
		t.Fatalf("missing HOME error = %v, want %v", err, noHome)
	}
	t.Setenv("XDG_DATA_HOME", home)
	want := filepath.Join(home, "chairlift", "printers", "ghostscript", "ghostscript")
	if got, err := app.HostVolumeDir(); err != nil || got != want {
		t.Fatalf("HostVolumeDir = %q, %v; want %q without HOME", got, err, want)
	}
}

func TestUniqueNamePortAndVolumePerApp(t *testing.T) {
	// The core acceptance: each rendered unit has a unique name, port, and
	// volume, so one logical device is never claimed or advertised twice.
	apps := []App{
		{Family: Families()[0], Name: "front-office"},
		{Family: Families()[0], Name: "back-office"},
		{Family: Families()[1], Name: "office-printer"},
		{Family: Families()[2], Name: "photo-printer"},
		{Family: Families()[2], Name: "label-printer"},
	}

	names, ports, volumes := map[string]bool{}, map[int]bool{}, map[string]bool{}
	for _, app := range apps {
		names[app.UnitName()] = true
		ports[app.Port()] = true
		volumes[app.Volume()] = true
	}

	if len(names) != len(apps) {
		t.Errorf("unit names are not unique: %d distinct for %d apps\n%v", len(names), len(apps), names)
	}
	if len(ports) != len(apps) {
		t.Errorf("ports are not unique: %d distinct for %d apps\n%v", len(ports), len(apps), ports)
	}
	if len(volumes) != len(apps) {
		t.Errorf("volumes are not unique: %d distinct for %d apps\n%v", len(volumes), len(apps), volumes)
	}

	// Same identity always maps to the same port, so a printer keeps one
	// address across restarts.
	if Families()[0].ID != "" {
		a := Select(Families()[0])
		if a.Port() != Select(Families()[0]).Port() {
			t.Error("the same app rendered a different port on a second call")
		}
	}
}

func TestPostScriptFamilyKeepsItsContractedPortNamesAndVolume(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/data")
	var ps App
	others := []App{}
	for _, f := range Families() {
		if f.ID == "ps" {
			ps = Select(f)
		} else {
			others = append(others, Select(f))
		}
	}
	if ps.Family.ID == "" {
		t.Fatal("Families() has no PostScript family")
	}
	if got := ps.Port(); got != 18020 {
		t.Errorf("PostScript Port = %d, want 18020", got)
	}
	if got := ps.UnitName(); got != "chairlift-printer-ps.container" {
		t.Errorf("PostScript UnitName = %q", got)
	}
	if got := ps.ContainerName(); got != "chairlift-printer-ps" {
		t.Errorf("PostScript ContainerName = %q", got)
	}
	if got, want := ps.Volume(), "/data/chairlift/printers/ps/ps:/var/lib/ps-printer-app:z"; got != want {
		t.Errorf("PostScript Volume = %q, want %q", got, want)
	}
	if len(others) != 3 {
		t.Fatalf("expected three other families, got %d", len(others))
	}
	for _, other := range others {
		if other.Port() == ps.Port() || other.UnitName() == ps.UnitName() || other.Volume() == ps.Volume() {
			t.Errorf("PostScript collides with %q: port %d/%d unit %q/%q", other.Family.ID, ps.Port(), other.Port(), ps.UnitName(), other.UnitName())
		}
	}
}

func TestUnitNameDoesNotCollideAndServiceMatches(t *testing.T) {
	app := Select(Families()[0])

	if !strings.HasPrefix(app.UnitName(), "chairlift-") {
		t.Errorf("UnitName = %q, want a chairlift- prefix", app.UnitName())
	}
	// Select(f) produces default unit matching design doc: chairlift-printer-<family>.container
	wantDefaultUnit := "chairlift-printer-" + app.Family.ID + ".container"
	if app.UnitName() != wantDefaultUnit {
		t.Errorf("UnitName = %q, want %q", app.UnitName(), wantDefaultUnit)
	}
	if app.ServiceName() != strings.TrimSuffix(app.UnitName(), ".container")+".service" {
		t.Errorf("ServiceName = %q does not match UnitName %q", app.ServiceName(), app.UnitName())
	}
	if app.ContainerName() != strings.TrimSuffix(app.UnitName(), ".container") {
		t.Errorf("ContainerName = %q does not match UnitName %q", app.ContainerName(), app.UnitName())
	}
}

func TestSanitizeStableAcrossSeparators(t *testing.T) {
	// "Office  Printer" (two spaces) and "office-printer" must land on the
	// same unit, so a re-typed name does not spawn a second owner.
	a := App{Family: Families()[0], Name: "Office  Printer"}
	b := App{Family: Families()[0], Name: "office-printer"}
	if a.UnitName() != b.UnitName() {
		t.Errorf("sanitize is not stable across separator runs: %q vs %q", a.UnitName(), b.UnitName())
	}
}

// stubUnitDir points the package at a temporary quadlet directory and records
// the systemctl calls it makes.
func stubUnitDir(t *testing.T) (dir string, calls *[]string) {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	previousDir := unitDir
	previousHomeDir := userHomeDir
	previousCosign := runCosign
	runCosign = func(context.Context, ...string) ([]byte, error) {
		return []byte(`[{"critical":{"image":{"docker-manifest-digest":"sha256:` + strings.Repeat("a", 64) + `"}}}]`), nil
	}
	previousSystemctl := runSystemctl
	previousSystemctlOutput := runSystemctlOutput
	t.Cleanup(func() {
		runCosign = previousCosign
		unitDir = previousDir
		userHomeDir = previousHomeDir
		runSystemctl = previousSystemctl
		runSystemctlOutput = previousSystemctlOutput
		dryrun.Set(false)
	})

	unitDir = func() (string, error) { return tmp, nil }
	userHomeDir = func() (string, error) { return tmp, nil }

	recorded := []string{}
	runSystemctl = func(_ context.Context, args ...string) error {
		recorded = append(recorded, strings.Join(args, " "))
		return nil
	}
	runSystemctlOutput = func(_ context.Context, args ...string) (string, error) {
		recorded = append(recorded, strings.Join(args, " "))
		return "", nil
	}
	return tmp, &recorded
}

func TestEnableWritesVerifiedDigestAndStartsWithoutAutoUpdate(t *testing.T) {
	dir, calls := stubUnitDir(t)
	app := Select(Families()[0])

	if IsEnabled(app) {
		t.Fatal("IsEnabled reported true before Enable")
	}

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, app.UnitName()))
	if err != nil {
		t.Fatalf("reading written unit: %v", err)
	}
	if !strings.Contains(string(data), "Image=ghcr.io/projectbluefin/ghostscript-printer-app@sha256:"+strings.Repeat("a", 64)) || strings.Contains(string(data), "AutoUpdate=") || strings.Contains(string(data), ":stable") {
		t.Errorf("written unit is not pinned without auto-update:\n%s", data)
	}

	want := []string{"daemon-reload", "start " + app.ServiceName()}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}

	if !IsEnabled(app) {
		t.Error("IsEnabled reported false after Enable")
	}
}

// A failed reload or start rolls a fresh install back.
func TestEnableRollsBackAFreshUnitOnFailure(t *testing.T) {
	for _, failing := range []string{"start", "daemon-reload"} {
		t.Run(failing, func(t *testing.T) {
			dir, calls := stubUnitDir(t)
			runSystemctl = func(_ context.Context, args ...string) error {
				*calls = append(*calls, strings.Join(args, " "))
				if args[0] == failing {
					return errors.New("refused")
				}
				return nil
			}

			app := Select(Families()[0])
			if err := Enable(context.Background(), app); err == nil {
				t.Fatalf("Enable returned no error when %s failed", failing)
			}

			if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
				t.Error("a failed Enable left its quadlet on disk")
			}
			n := len(*calls)
			if n < 2 || (*calls)[n-2] != "stop "+app.ServiceName() || (*calls)[n-1] != "daemon-reload" {
				t.Errorf("systemctl calls = %v, want a stop then daemon-reload rollback", *calls)
			}
		})
	}
}

func TestEnableKeepsAPreexistingUnitWhenStartFails(t *testing.T) {
	dir, _ := stubUnitDir(t)
	app := Select(Families()[0])

	unitPath := filepath.Join(dir, app.UnitName())
	if err := os.WriteFile(unitPath, []byte("preexisting"), 0o644); err != nil {
		t.Fatal(err)
	}

	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "start" {
			return errors.New("unit not found")
		}
		return nil
	}

	if err := Enable(context.Background(), app); err == nil {
		t.Fatal("Enable returned no error when the service failed to start")
	}

	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		t.Error("Enable removed a preexisting unit when start failed")
	}
}

func TestDisableRemovesTheUnit(t *testing.T) {
	dir, calls := stubUnitDir(t)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	*calls = nil

	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Error("Disable left the quadlet on disk")
	}
	want := []string{"stop " + app.ServiceName(), "daemon-reload"}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}
}

func TestDisableLeavesTheStateVolumeUntouched(t *testing.T) {
	home, _ := stubUnitDir(t)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	state := filepath.Join(home, ".local", "share", "chairlift", "printers", "ghostscript", "ghostscript", "printer.conf")
	if err := os.WriteFile(state, []byte("configured printer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if got, err := os.ReadFile(state); err != nil || string(got) != "configured printer" {
		t.Fatalf("Disable changed printer state: %q, %v", got, err)
	}
}

func TestDisableSucceedsWhenTheServiceIsAlreadyDown(t *testing.T) {
	dir, _ := stubUnitDir(t)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "stop" {
			return errors.New("unit is not loaded")
		}
		return nil
	}
	runSystemctlOutput = func(_ context.Context, args ...string) (string, error) {
		if args[0] == "is-active" {
			return "inactive", nil
		}
		return "", nil
	}

	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Error("Disable left the quadlet on disk after a failed stop")
	}
}

func TestDisablePreservesTheUnitWhenStopFailsAndServiceRemainsActive(t *testing.T) {
	dir, _ := stubUnitDir(t)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	stopErr := errors.New("refused to stop")
	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "stop" {
			return stopErr
		}
		return nil
	}
	runSystemctlOutput = func(_ context.Context, args ...string) (string, error) {
		if args[0] == "is-active" {
			return "active", nil
		}
		return "", nil
	}

	if err := Disable(context.Background(), app); err == nil {
		t.Fatal("Disable succeeded when stop failed and service was active")
	}
	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); os.IsNotExist(err) {
		t.Error("Disable removed the quadlet while service was still active")
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	dir, calls := stubUnitDir(t)
	dryrun.Set(true)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Error("dry-run wrote a quadlet")
	}
	if len(*calls) != 0 {
		t.Errorf("dry-run ran systemctl: %v", *calls)
	}
}

func TestRenderUnitsIsOrderIndependent(t *testing.T) {
	apps := []App{
		{Family: Families()[0], Name: "a"},
		{Family: Families()[1], Name: "b"},
		{Family: Families()[2], Name: "c"},
	}
	reversed := make([]App, len(apps))
	for i, a := range apps {
		reversed[len(apps)-1-i] = a
	}

	if RenderUnits(apps) != RenderUnits(reversed) {
		t.Error("RenderUnits output depends on input order")
	}
}

func TestApplyOverridesReplacesTheImage(t *testing.T) {
	original := Families()[0]
	t.Cleanup(func() {
		for i := range families {
			if families[i].ID == original.ID {
				families[i] = original
				return
			}
		}
	})

	const mirror = "mirror.example.internal/ghostscript-printer-app@sha256:82487bd81925b824f16d79a50b4237230d00429fca7761454299a8a4393368cc"
	if err := ApplyOverrides(map[string]string{"ghostscript": mirror}); err != nil {
		t.Fatalf("ApplyOverrides: %v", err)
	}

	if got := Select(Families()[0]).Family.Image; got != mirror {
		t.Errorf("image = %q, want the override", got)
	}
	if unit := RenderUnit(Select(Families()[0])); !strings.Contains(unit, "Image="+mirror+"\n") {
		t.Errorf("rendered unit does not run the override:\n%s", unit)
	}

	// An override for one family leaves the others alone. Look hplip up by ID:
	// its position in Families() shifts whenever a family is added.
	for _, f := range Families() {
		if f.ID == "hplip" && f.Image != "ghcr.io/projectbluefin/hplip-printer-app:stable" {
			t.Errorf("overriding ghostscript disturbed the hplip image: %q", f.Image)
		}
	}
}

func TestApplyOverridesRejectsBadInput(t *testing.T) {
	tests := []struct {
		name   string
		images map[string]string
		want   string
	}{
		{
			name:   "unknown family",
			images: map[string]string{"brother": "example.com/x:1"},
			// A typo'd family ID must surface, not silently do nothing while the
			// site believes its mirror is in use.
			want: `unknown family "brother"`,
		},
		{
			name:   "empty image",
			images: map[string]string{"hplip": ""},
			want:   "empty image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := Families()[1].Image

			err := ApplyOverrides(tt.images)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ApplyOverrides unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ApplyOverrides error = %v, want it to mention %q", err, tt.want)
			}
			if got := Families()[1].Image; got != before {
				t.Errorf("a rejected override changed the hplip image to %q", got)
			}
		})
	}
}

func TestDefaultUnitDirReturnsPathUnderUserConfig(t *testing.T) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("cannot determine UserConfigDir: %v", err)
	}
	got, err := defaultUnitDir()
	if err != nil {
		t.Fatalf("defaultUnitDir() error: %v", err)
	}
	want := filepath.Join(configDir, "containers", "systemd")
	if got != want {
		t.Errorf("defaultUnitDir() = %q, want %q", got, want)
	}
}

// A Disable that arrives while Refresh is verifying must wait for Refresh to
// finish, so Refresh cannot rewrite and restart a unit Disable just removed.
func TestDisableWaitsForAnInFlightRefreshOfTheSameApp(t *testing.T) {
	dir, calls := stubUnitDir(t)
	app := Select(Families()[0])
	if err := os.WriteFile(filepath.Join(dir, app.UnitName()), []byte("Image="+app.Family.Image+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	verifying := make(chan struct{})
	release := make(chan struct{})
	verify := runCosign
	runCosign = func(ctx context.Context, args ...string) ([]byte, error) {
		close(verifying)
		<-release
		return verify(ctx, args...)
	}
	waiting := make(chan struct{}, 2)
	lockWaitHook = func(App) { waiting <- struct{}{} }
	t.Cleanup(func() { lockWaitHook = nil })

	refreshed := make(chan error, 1)
	go func() { refreshed <- Refresh(context.Background(), app) }()
	<-waiting
	<-verifying

	disabled := make(chan error, 1)
	go func() { disabled <- Disable(context.Background(), app) }()
	<-waiting
	select {
	case err := <-disabled:
		t.Fatalf("Disable returned %v while Refresh held the app", err)
	default:
	}
	close(release)

	if err := <-refreshed; err != nil {
		t.Fatalf("Refresh = %v", err)
	}
	if err := <-disabled; err != nil {
		t.Fatalf("Disable = %v", err)
	}
	if IsEnabled(app) {
		t.Fatal("unit survived a Disable issued during Refresh")
	}
	want := []string{"daemon-reload", "restart " + app.ServiceName(), "stop " + app.ServiceName(), "daemon-reload"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
}

func TestWebAdminURLDerivesFromAppPort(t *testing.T) {
	for _, f := range Families() {
		app := Select(f)
		want := fmt.Sprintf("http://localhost:%d/", app.Port())
		if got := app.WebAdminURL(); got != want {
			t.Errorf("app %s WebAdminURL = %q, want %q", f.ID, got, want)
		}
		unit := RenderUnit(app)
		wantPublish := fmt.Sprintf("PublishPort=127.0.0.1:%d:%d", app.Port(), app.Port())
		if !strings.Contains(unit, wantPublish) {
			t.Errorf("app %s rendered unit missing %q", f.ID, wantPublish)
		}
	}
}

func TestRenderUnitWithDevicePath(t *testing.T) {
	prevStat := statDeviceNode
	t.Cleanup(func() { statDeviceNode = prevStat })

	// By default or when statDeviceNode is false (node missing/stale), AddDevice= is omitted
	statDeviceNode = func(path string) bool { return false }
	appWithoutDevice := Select(Families()[0])
	unitWithoutDevice := RenderUnit(appWithoutDevice)
	if strings.Contains(unitWithoutDevice, "AddDevice=") {
		t.Errorf("unit without DevicePath contains AddDevice: %s", unitWithoutDevice)
	}

	appWithDevice := Select(Families()[0])
	appWithDevice.DevicePath = "/dev/bus/usb/001/008"
	unitWithMissingDevice := RenderUnit(appWithDevice)
	if strings.Contains(unitWithMissingDevice, "AddDevice=") {
		t.Errorf("unit with missing DevicePath must omit AddDevice to prevent container failure: %s", unitWithMissingDevice)
	}

	// When node exists and is a valid device, AddDevice= is rendered
	statDeviceNode = func(path string) bool { return path == "/dev/bus/usb/001/008" }
	unitWithDevice := RenderUnit(appWithDevice)
	wantLine := "AddDevice=/dev/bus/usb/001/008\n"
	if !strings.Contains(unitWithDevice, wantLine) {
		t.Errorf("unit with present DevicePath missing %q: %s", wantLine, unitWithDevice)
	}
}

func TestValidateDeviceConflicts(t *testing.T) {
	apps := []App{
		{Family: Families()[0], Name: "app1", DevicePath: "/dev/bus/usb/001/002"},
		{Family: Families()[1], Name: "app2", DevicePath: "/dev/bus/usb/001/003"},
		{Family: Families()[2], Name: "app3", DevicePath: ""},
	}
	if err := ValidateDeviceConflicts(apps); err != nil {
		t.Fatalf("ValidateDeviceConflicts failed on distinct devices: %v", err)
	}

	conflicting := []App{
		{Family: Families()[0], Name: "app1", DevicePath: "/dev/bus/usb/001/002"},
		{Family: Families()[1], Name: "app2", DevicePath: "/dev/bus/usb/001/002"},
	}
	if err := ValidateDeviceConflicts(conflicting); err == nil {
		t.Fatal("ValidateDeviceConflicts allowed duplicate DevicePath")
	}
}

func TestScanUSBDevices(t *testing.T) {
	sysfs := t.TempDir()
	prevRoot := usbSysfsRoot
	usbSysfsRoot = sysfs
	t.Cleanup(func() { usbSysfsRoot = prevRoot })

	// Device 1: Hub (not printer)
	hubDir := filepath.Join(sysfs, "1-1")
	if err := os.MkdirAll(filepath.Join(hubDir, "1-1:1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(hubDir, "busnum"), []byte("1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(hubDir, "devnum"), []byte("2\n"), 0o644)
	_ = os.WriteFile(filepath.Join(hubDir, "manufacturer"), []byte("Generic\n"), 0o644)
	_ = os.WriteFile(filepath.Join(hubDir, "product"), []byte("USB Hub\n"), 0o644)
	_ = os.WriteFile(filepath.Join(hubDir, "bDeviceClass"), []byte("09\n"), 0o644)
	_ = os.WriteFile(filepath.Join(hubDir, "1-1:1.0", "bInterfaceClass"), []byte("09\n"), 0o644)

	// Device 2: HP LaserJet Printer (interface class 07)
	printerDir := filepath.Join(sysfs, "1-2")
	if err := os.MkdirAll(filepath.Join(printerDir, "1-2:1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(printerDir, "busnum"), []byte("1\n"), 0o644)
	_ = os.WriteFile(filepath.Join(printerDir, "devnum"), []byte("8\n"), 0o644)
	_ = os.WriteFile(filepath.Join(printerDir, "manufacturer"), []byte("HP\n"), 0o644)
	_ = os.WriteFile(filepath.Join(printerDir, "product"), []byte("LaserJet M1212nf\n"), 0o644)
	_ = os.WriteFile(filepath.Join(printerDir, "bDeviceClass"), []byte("00\n"), 0o644)
	_ = os.WriteFile(filepath.Join(printerDir, "1-2:1.0", "bInterfaceClass"), []byte("07\n"), 0o644)

	devices, err := ScanUSBDevices()
	if err != nil {
		t.Fatalf("ScanUSBDevices() error = %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("ScanUSBDevices() returned %d devices, want 2", len(devices))
	}

	d1 := devices[0]
	if d1.DevicePath != "/dev/bus/usb/001/002" || d1.IsPrinter {
		t.Errorf("device 1 = %+v; want hub not printer", d1)
	}

	d2 := devices[1]
	if d2.DevicePath != "/dev/bus/usb/001/008" || !d2.IsPrinter || d2.Manufacturer != "HP" || d2.Product != "LaserJet M1212nf" {
		t.Errorf("device 2 = %+v; want HP LaserJet printer", d2)
	}
}
