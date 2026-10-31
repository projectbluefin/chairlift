package livery

import (
	"context"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
)

func TestDamaskInstalledReportsPresenceInEitherScope(t *testing.T) {
	original := damaskScopes
	t.Cleanup(func() { damaskScopes = original })

	t.Run("present in user scope", func(t *testing.T) {
		damaskScopes = []func() ([]flatpak.Application, error){
			func() ([]flatpak.Application, error) {
				return []flatpak.Application{{ApplicationID: "org.other.App"}}, nil
			},
			func() ([]flatpak.Application, error) {
				return []flatpak.Application{{ApplicationID: DamaskID}}, nil
			},
		}
		if installed, err := DamaskInstalled(context.Background()); err != nil || !installed {
			t.Fatalf("expected Damask reported installed from the system scope, got %v, %v", installed, err)
		}
	})

	t.Run("absent in both scopes returns false", func(t *testing.T) {
		damaskScopes = []func() ([]flatpak.Application, error){
			func() ([]flatpak.Application, error) { return nil, nil },
			func() ([]flatpak.Application, error) { return nil, nil },
		}
		if installed, err := DamaskInstalled(context.Background()); err != nil || installed {
			t.Fatalf("expected Damask reported absent, got %v, %v", installed, err)
		}
	})

	t.Run("a failed query is unknown, not absent", func(t *testing.T) {
		damaskScopes = []func() ([]flatpak.Application, error){
			func() ([]flatpak.Application, error) { return nil, &flatpak.Error{Message: "list failed"} },
			func() ([]flatpak.Application, error) { return nil, nil },
		}
		if installed, err := DamaskInstalled(context.Background()); err == nil || installed {
			t.Fatalf("expected a failed scope query to surface as an error, got %v, %v", installed, err)
		}
	})

	t.Run("a failed query still finds Damask in the other scope", func(t *testing.T) {
		damaskScopes = []func() ([]flatpak.Application, error){
			func() ([]flatpak.Application, error) { return nil, &flatpak.Error{Message: "list failed"} },
			func() ([]flatpak.Application, error) {
				return []flatpak.Application{{ApplicationID: DamaskID}}, nil
			},
		}
		if installed, err := DamaskInstalled(context.Background()); err != nil || !installed {
			t.Fatalf("expected Damask found despite one failed scope, got %v, %v", installed, err)
		}
	})

	t.Run("flatpak absent is not installed, not an error", func(t *testing.T) {
		damaskScopes = []func() ([]flatpak.Application, error){
			func() ([]flatpak.Application, error) {
				return nil, &flatpak.NotFoundError{Message: "flatpak not found"}
			},
			func() ([]flatpak.Application, error) {
				return nil, &flatpak.NotFoundError{Message: "flatpak not found"}
			},
		}
		if installed, err := DamaskInstalled(context.Background()); err != nil || installed {
			t.Fatalf("expected a missing Flatpak to read as absent, got %v, %v", installed, err)
		}
	})
}

func TestDamaskInstallRespectsDryRun(t *testing.T) {
	newFakeCommands(t)
	useTempDataHome(t)
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	original := damaskScopes
	damaskScopes = []func() ([]flatpak.Application, error){
		func() ([]flatpak.Application, error) { return nil, nil },
	}
	t.Cleanup(func() { damaskScopes = original })

	if err := DamaskInstall(context.Background()); err != nil {
		t.Fatalf("dry-run install should succeed: %v", err)
	}
}

// TestDamaskCommandRunsTheFlatpak pins the launch itself: desktops register
// no app:// handler, so the command must be `flatpak run <id>`.
func TestDamaskCommandRunsTheFlatpak(t *testing.T) {
	cmd := damaskCommand()
	want := []string{"flatpak", "run", DamaskID}
	if strings.Join(cmd.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("Damask launch = %q, want %q", cmd.Args, want)
	}
}

func TestDamaskOpenLaunchesUnlessDryRun(t *testing.T) {
	original := launchDamask
	t.Cleanup(func() { launchDamask = original })
	launches := 0
	launchDamask = func(context.Context) error {
		launches++
		return nil
	}

	if err := DamaskOpen(context.Background()); err != nil {
		t.Fatalf("DamaskOpen: %v", err)
	}
	if launches != 1 {
		t.Fatalf("DamaskOpen launched %d times, want 1", launches)
	}

	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	if err := DamaskOpen(context.Background()); err != nil {
		t.Fatalf("dry-run DamaskOpen: %v", err)
	}
	if launches != 1 {
		t.Fatal("dry-run DamaskOpen launched Damask")
	}
}
