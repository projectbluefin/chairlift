package livery

import (
	"errors"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
)

type damaskScope = struct {
	name string
	list func() ([]flatpak.Application, error)
}

func listing(apps []flatpak.Application, err error) func() ([]flatpak.Application, error) {
	return func() ([]flatpak.Application, error) { return apps, err }
}

// useDamaskFakes replaces both scopes and both mutations, returning counters
// for the mutations so a test can prove which ran.
func useDamaskFakes(t *testing.T, user, system func() ([]flatpak.Application, error)) (installs, launches *int) {
	t.Helper()
	scopes, install, launch := damaskScopes, installDamask, launchDamask
	t.Cleanup(func() {
		damaskScopes, installDamask, launchDamask = scopes, install, launch
		dryrun.Set(false)
	})
	damaskScopes = []damaskScope{{name: "user", list: user}, {name: "system", list: system}}
	installs, launches = new(int), new(int)
	installDamask = func() error { *installs++; return nil }
	launchDamask = func() error { *launches++; return nil }
	return installs, launches
}

var withDamask = []flatpak.Application{{ApplicationID: "org.other.App"}, {ApplicationID: DamaskID}}

func TestDamaskInstalledProbesBothScopes(t *testing.T) {
	listErr := &flatpak.Error{Message: "list failed"}
	missing := &flatpak.NotFoundError{Message: "flatpak not found"}
	tests := []struct {
		name          string
		user, system  func() ([]flatpak.Application, error)
		wantInstalled bool
		wantErr       bool
	}{
		{name: "installed for the user", user: listing(withDamask, nil), system: listing(nil, nil), wantInstalled: true},
		{name: "installed system-wide", user: listing(nil, nil), system: listing(withDamask, nil), wantInstalled: true},
		{name: "absent from both scopes", user: listing(nil, nil), system: listing(nil, nil)},
		{name: "a failed query is unknown, not absent", user: listing(nil, listErr), system: listing(nil, nil), wantErr: true},
		{name: "a failed query still finds Damask in the other scope", user: listing(nil, listErr), system: listing(withDamask, nil), wantInstalled: true},
		{name: "flatpak missing reads as not installed", user: listing(nil, missing), system: listing(nil, missing)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useDamaskFakes(t, tt.user, tt.system)
			installed, err := DamaskInstalled()
			if installed != tt.wantInstalled || (err != nil) != tt.wantErr {
				t.Fatalf("DamaskInstalled() = %v, %v; want %v, error=%v", installed, err, tt.wantInstalled, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, listErr) {
				t.Fatalf("probe error %v does not wrap the flatpak failure", err)
			}
		})
	}
}

func TestDamaskInstallRunsOnlyWhenAbsentAndLive(t *testing.T) {
	t.Run("absent installs once", func(t *testing.T) {
		installs, launches := useDamaskFakes(t, listing(nil, nil), listing(nil, nil))
		if err := DamaskInstall(); err != nil {
			t.Fatalf("DamaskInstall: %v", err)
		}
		if *installs != 1 || *launches != 0 {
			t.Fatalf("installs=%d launches=%d, want 1 and 0", *installs, *launches)
		}
	})
	t.Run("already installed is a no-op", func(t *testing.T) {
		installs, _ := useDamaskFakes(t, listing(nil, nil), listing(withDamask, nil))
		if err := DamaskInstall(); err != nil || *installs != 0 {
			t.Fatalf("DamaskInstall = %v with %d installs, want nil and 0", err, *installs)
		}
	})
	t.Run("a failed probe never installs", func(t *testing.T) {
		installs, _ := useDamaskFakes(t, listing(nil, &flatpak.Error{Message: "list failed"}), listing(nil, nil))
		if err := DamaskInstall(); err == nil || *installs != 0 {
			t.Fatalf("DamaskInstall = %v with %d installs, want an error and 0", err, *installs)
		}
	})
	t.Run("dry run installs nothing", func(t *testing.T) {
		installs, _ := useDamaskFakes(t, listing(nil, nil), listing(nil, nil))
		dryrun.Set(true)
		if err := DamaskInstall(); err != nil || *installs != 0 {
			t.Fatalf("dry-run DamaskInstall = %v with %d installs, want nil and 0", err, *installs)
		}
	})
	t.Run("a failed install is returned for retry", func(t *testing.T) {
		useDamaskFakes(t, listing(nil, nil), listing(nil, nil))
		cause := errors.New("polkit denied")
		installDamask = func() error { return cause }
		if err := DamaskInstall(); !errors.Is(err, cause) {
			t.Fatalf("DamaskInstall = %v, want it to wrap %v", err, cause)
		}
	})
}

func TestDamaskOpenLaunchesUnlessDryRun(t *testing.T) {
	installs, launches := useDamaskFakes(t, listing(withDamask, nil), listing(nil, nil))
	if err := DamaskOpen(); err != nil {
		t.Fatalf("DamaskOpen: %v", err)
	}
	if *launches != 1 || *installs != 0 {
		t.Fatalf("launches=%d installs=%d, want 1 and 0", *launches, *installs)
	}

	dryrun.Set(true)
	if err := DamaskOpen(); err != nil {
		t.Fatalf("dry-run DamaskOpen: %v", err)
	}
	if *launches != 1 {
		t.Fatal("dry-run DamaskOpen launched Damask")
	}

	dryrun.Set(false)
	cause := errors.New("exec: flatpak: not found")
	launchDamask = func() error { return cause }
	if err := DamaskOpen(); !errors.Is(err, cause) {
		t.Fatalf("DamaskOpen = %v, want it to wrap %v", err, cause)
	}
}

// TestDamaskCommandRunsTheFlatpak pins the launch: desktops register no
// handler for an app ID, so the command must be `flatpak run <id>`.
func TestDamaskCommandRunsTheFlatpak(t *testing.T) {
	want := []string{"flatpak", "run", DamaskID}
	if got := damaskCommand().Args; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Damask launch = %q, want %q", got, want)
	}
}
