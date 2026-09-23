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
// following the fake-runner pattern of internal/flatpak's tests. The read
// command prints a canned list output; state-changing commands record their
// arguments to CHAIRLIFT_FLATPAK_ARGS. This drives IsInstalled/Provision
// without a real Flatpak installation.
func fakeFlatpak(t *testing.T, listOutput, installBody string) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "args")
	script := filepath.Join(dir, "flatpak")
	source := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"install) printf '%s\\n' \"$@\" > \"" + capture + "\" ;;\n" +
		"list) printf '%s' \"" + listOutput + "\" ;;\n" +
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

func TestDetectsPulp(t *testing.T) {
	t.Run("present detects Pulp", func(t *testing.T) {
		list := "Pulp\torg.gnome.gitlab.cheywood.Pulp\t3.22.0\tstable\tflathub\tapp/org.gnome.gitlab.cheywood.Pulp/x86_64/stable\n" +
			"Firefox\torg.mozilla.firefox\t120.0\tstable\tflathub\tapp/org.mozilla.firefox/x86_64/stable\n"
		fakeFlatpak(t, list, "")

		got, err := IsInstalled()
		if err != nil {
			t.Fatalf("IsInstalled() error = %v", err)
		}
		if !got {
			t.Fatalf("IsInstalled() = false, want true when Pulp is listed")
		}
	})

	t.Run("absent returns false", func(t *testing.T) {
		fakeFlatpak(t, "Firefox\torg.mozilla.firefox\t120.0\tstable\tflathub\tapp\n", "")

		got, err := IsInstalled()
		if err != nil {
			t.Fatalf("IsInstalled() error = %v", err)
		}
		if got {
			t.Fatalf("IsInstalled() = true, want false when Pulp is not listed")
		}
	})

	t.Run("empty list is absent", func(t *testing.T) {
		fakeFlatpak(t, "", "")

		got, err := IsInstalled()
		if err != nil {
			t.Fatalf("IsInstalled() error = %v", err)
		}
		if got {
			t.Fatalf("IsInstalled() = true, want false for an empty list")
		}
	})
}

func TestProvisionInstallsWhenMissing(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	// Empty list: Pulp is missing, so Provision must install it.
	capture := fakeFlatpak(t, "", "")

	if err := Provision(); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}

	args, issued := capturedInstallArgs(t, capture)
	if !issued {
		t.Fatal("Provision did not issue a flatpak install")
	}
	want := []string{"install", "-y", "--user", "flathub", PulpID}
	if got := strings.Join(args, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("install args = %v, want %v", args, want)
	}
}

func TestProvisionSkipsWhenPresent(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	capture := fakeFlatpak(t, "Pulp\torg.gnome.gitlab.cheywood.Pulp\t3.22.0\tstable\tflathub\tapp\n", "")

	if err := Provision(); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	if _, issued := capturedInstallArgs(t, capture); issued {
		t.Fatal("Provision issued an install even though Pulp was already present")
	}
}

func TestProvisionDryRunDoesNotInstall(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	// Empty list would normally trigger an install; dry-run must not.
	capture := fakeFlatpak(t, "", "")

	if err := Provision(); err != nil {
		t.Fatalf("Provision() error = %v", err)
	}
	if _, issued := capturedInstallArgs(t, capture); issued {
		t.Fatal("Provision issued a flatpak install under dry-run")
	}
}

func TestProvisionPropagatesQueryError(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	// list exits non-zero with stderr; the error must propagate and no
	// install must be attempted.
	capture := fakeFlatpak(t, "", "exit 7")

	err := Provision()
	if err == nil {
		t.Fatal("Provision() = nil error, want the propagated query error")
	}
	if _, issued := capturedInstallArgs(t, capture); issued {
		t.Fatal("Provision attempted an install after a query failure")
	}
	if !strings.Contains(err.Error(), "listing user flatpaks") {
		t.Errorf("Provision() error = %v, want it wrapped with the list step", err)
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
