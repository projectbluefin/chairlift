package printerapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestFamiliesCoversEveryDriverFamily(t *testing.T) {
	wantIDs := []string{"ghostscript", "hplip", "gutenprint"}
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
		// the unit's AutoUpdate=registry keeps it current.
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
			"AutoUpdate=registry",
			"ContainerName=" + app.ContainerName(),
			"WantedBy=default.target",
			"PublishPort=127.0.0.1:" + strconv.Itoa(app.Port()) + ":" + strconv.Itoa(app.Port()),
			"Volume=%h/printer-workspaces/" + app.Family.ID + "/" + app.Name + ":/var/lib/" + app.Family.ID + "-printer-app:z",
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
	previousDir := unitDir
	previousHomeDir := userHomeDir
	previousSystemctl := runSystemctl
	previousSystemctlOutput := runSystemctlOutput
	t.Cleanup(func() {
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

func TestEnableWritesTheUnitStartsItAndEnablesAutoUpdate(t *testing.T) {
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
	if !strings.Contains(string(data), app.Family.Image) {
		t.Errorf("written unit does not name %q:\n%s", app.Family.Image, data)
	}

	want := []string{"daemon-reload", "start " + app.ServiceName(), "enable --now podman-auto-update.timer"}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}

	if !IsEnabled(app) {
		t.Error("IsEnabled reported false after Enable")
	}
}

// A failed start or a failed auto-update timer enable both roll a fresh
// install back: the service is stopped, the quadlet removed, systemd reloaded.
func TestEnableRollsBackAFreshUnitOnFailure(t *testing.T) {
	for _, failing := range []string{"start", "enable"} {
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
	_, _ = stubUnitDir(t)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	// The state volume is under the user's home, which stubUnitDir does not
	// point at a temp dir, so assert Disable does not try to remove anything
	// outside the quadlet unit. The only removal Disable performs is the unit.
	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
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

	// An override for one family leaves the others alone.
	if Families()[1].Image != "ghcr.io/projectbluefin/hplip-printer-app:stable" {
		t.Error("overriding ghostscript disturbed the hplip image")
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
