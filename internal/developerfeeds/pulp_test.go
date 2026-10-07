package developerfeeds

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// fakeFlatpak writes a shell script standing in for the flatpak executable,
// following the fake-runner pattern of internal/flatpak's tests. `list`
// prints userList or systemList by the scope flag it was given;
// state-changing commands record their arguments to the returned path. This
// drives IsInstalled/Provision without a real Flatpak installation.
func fakeFlatpak(t *testing.T, userList, systemList, installBody string) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "args")
	script := filepath.Join(dir, "flatpak")
	source := "#!/bin/sh\n" +
		"case \"$1 $2\" in\n" +
		"install*) printf '%s\\n' \"$@\" > \"" + capture + "\" ;;\n" +
		"'list --user') printf '%s' \"" + userList + "\" ;;\n" +
		"'list --system') printf '%s' \"" + systemList + "\" ;;\n" +
		"esac\n" +
		installBody +
		"\n"
	if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
		t.Fatalf("write fake flatpak: %v", err)
	}
	t.Setenv("PATH", dir)
	return capture
}

func capturedInstallArgs(t *testing.T, path string) ([]string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false // no install was issued
	}
	if err != nil {
		t.Fatalf("read captured flatpak args: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n"), true
}

const (
	pulpRow    = "Pulp\torg.gnome.gitlab.cheywood.Pulp\t3.22.0\tstable\tflathub\tapp/org.gnome.gitlab.cheywood.Pulp/x86_64/stable\n"
	firefoxRow = "Firefox\torg.mozilla.firefox\t120.0\tstable\tflathub\tapp/org.mozilla.firefox/x86_64/stable\n"
)

// Pulp counts as present in either scope: Provision installs it system-wide
// now, and an earlier ChairLift release installed it per-user.
func TestDetectsPulp(t *testing.T) {
	tests := []struct {
		name       string
		userList   string
		systemList string
		want       bool
	}{
		{name: "system copy", systemList: pulpRow + firefoxRow, want: true},
		{name: "per-user copy from an earlier release", userList: pulpRow, systemList: firefoxRow, want: true},
		{name: "absent from both scopes", userList: firefoxRow, systemList: firefoxRow, want: false},
		{name: "empty lists", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeFlatpak(t, test.userList, test.systemList, "")

			got, err := IsInstalled()
			if err != nil {
				t.Fatalf("IsInstalled() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("IsInstalled() = %v, want %v", got, test.want)
			}
		})
	}
}

// Issue #503: Bluefin and Dakota configure Flathub only as a system remote,
// so Pulp is installed system-wide from it.
func TestProvisionInstallsSystemWideWhenMissing(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	// Empty lists: Pulp is missing, so Provision must install it.
	capture := fakeFlatpak(t, "", "", "")

	if err := Provision(); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	args, issued := capturedInstallArgs(t, capture)
	if !issued {
		t.Fatal("Provision did not issue a flatpak install")
	}
	want := []string{"install", "-y", "--system", "flathub", PulpID}
	if got := strings.Join(args, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("install args = %v, want %v", args, want)
	}
}

func TestProvisionSkipsWhenPresentInEitherScope(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	for name, lists := range map[string][2]string{
		"user":   {pulpRow, ""},
		"system": {"", pulpRow},
	} {
		t.Run(name, func(t *testing.T) {
			capture := fakeFlatpak(t, lists[0], lists[1], "")

			if err := Provision(); err != nil {
				t.Fatalf("Provision() error = %v", err)
			}
			if _, issued := capturedInstallArgs(t, capture); issued {
				t.Fatal("Provision issued an install even though Pulp was already present")
			}
		})
	}
}

func TestProvisionDryRunDoesNotInstall(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	// Empty lists would normally trigger an install; dry-run must not.
	capture := fakeFlatpak(t, "", "", "")

	if err := Provision(); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	if _, issued := capturedInstallArgs(t, capture); issued {
		t.Fatal("Provision issued a flatpak install under dry-run")
	}
}

// A scope that cannot be listed might hold Pulp, so Provision fails closed
// rather than installing a second copy. Each scope is checked on its own.
func TestProvisionPropagatesQueryError(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	for _, scope := range []string{"user", "system"} {
		t.Run(scope, func(t *testing.T) {
			capture := fakeFlatpak(t, "", "", "if [ \"$1 $2\" = 'list --"+scope+"' ]; then exit 7; fi")

			err := Provision()
			if err == nil {
				t.Fatal("Provision() = nil error, want the propagated query error")
			}
			if _, issued := capturedInstallArgs(t, capture); issued {
				t.Fatal("Provision attempted an install after a query failure")
			}
			if !strings.Contains(err.Error(), "listing "+scope+" flatpaks") {
				t.Errorf("Provision() error = %v, want it wrapped with the %s list step", err, scope)
			}
		})
	}
}

func TestStageOPMLWritesFileWithPermissions(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	home := t.TempDir()
	t.Setenv("HOME", home)
	original := opmlContent
	opmlContent = "<opml version=\"2.0\"><head/><body/></opml>\n"
	t.Cleanup(func() { opmlContent = original })

	if err := StageOPML(); err != nil {
		t.Fatalf("StageOPML() error = %v", err)
	}

	path := filepath.Join(home, ".local", "share", "chairlift", OPMLFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read staged OPML: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != strings.TrimSpace(opmlContent) {
		t.Fatalf("staged OPML = %q, want %q", got, opmlContent)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat staged OPML: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("staged OPML perms = %o, want 644", perm)
	}
}

// OPMLPath answers where StageOPML writes without writing anything, which is
// what the Developer Mode feedback names to the user. It must agree with the
// file the staging step actually produces, or the banner points at a path
// that does not exist.
func TestOPMLPathMatchesTheStagedFile(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := OPMLPath()
	if err != nil {
		t.Fatalf("OPMLPath() error = %v", err)
	}
	if want := filepath.Join(home, ".local", "share", "chairlift", OPMLFileName); path != want {
		t.Fatalf("OPMLPath() = %q, want %q", path, want)
	}

	if err := StageOPML(); err != nil {
		t.Fatalf("StageOPML() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("stat OPMLPath() = %v, want the staged catalog to exist there", err)
	}
}

func TestStageOPMLDryRunWritesNothing(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := StageOPML(); err != nil {
		t.Fatalf("StageOPML() error = %v", err)
	}

	path := filepath.Join(home, ".local", "share", "chairlift", OPMLFileName)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote the OPML file (stat err = %v)", err)
	}
}

func TestStagedOPMLIsWellFormed(t *testing.T) {
	// The shipped catalog must be well-formed XML with balanced outline tags
	// and unique feed URLs, verified offline with no network access.
	doc, err := Load()
	if err != nil {
		t.Fatalf("embedded OPML parse failed: %v", err)
	}
	if problems := Validate(doc); len(problems) > 0 {
		for _, p := range problems {
			t.Errorf("embedded OPML problem: %s", p)
		}
		t.Fatalf("embedded OPML invalid (%d problems)", len(problems))
	}
}
