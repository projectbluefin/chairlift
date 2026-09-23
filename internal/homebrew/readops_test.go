package homebrew

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeBrewOnPath installs an executable named "brew" ahead of the real one on
// $PATH so the exported read operations — which resolve "brew" through $PATH
// inside runBrewCommand — can be driven end to end. Every invocation appends
// its argv to a log file whose path is returned, so tests can assert the exact
// command and flags each operation sends.
func fakeBrewOnPath(t *testing.T, body string) (argvLog string) {
	t.Helper()
	dir := t.TempDir()
	argvLog = filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + argvLog + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "brew"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvLog
}

// recordedArgv returns one entry per fake brew invocation, in call order.
func recordedArgv(t *testing.T, argvLog string) []string {
	t.Helper()
	data, err := os.ReadFile(argvLog)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func assertArgv(t *testing.T, argvLog string, want []string) {
	t.Helper()
	if got := recordedArgv(t, argvLog); !reflect.DeepEqual(got, want) {
		t.Fatalf("brew invocations = %#v, want %#v", got, want)
	}
}

// emit builds a fake-brew body that prints s verbatim on stdout. printf is a
// shell builtin, so the body stays independent of the rest of $PATH.
func emit(s string) string {
	return "printf '%s' '" + s + "'"
}

func TestListInstalledFormulaeEndToEnd(t *testing.T) {
	t.Run("sends the formula namespace query", func(t *testing.T) {
		argvLog := fakeBrewOnPath(t, emit(installedInfoJSON))

		if _, err := ListInstalledFormulae(); err != nil {
			t.Fatalf("ListInstalledFormulae() error = %v", err)
		}

		assertArgv(t, argvLog, []string{"info --installed --json=v2 --formula"})
	})

	t.Run("propagates a brew failure as *Error", func(t *testing.T) {
		fakeBrewOnPath(t, "echo 'boom' >&2\nexit 1")

		packages, err := ListInstalledFormulae()
		if packages != nil {
			t.Fatalf("ListInstalledFormulae() packages = %#v, want nil", packages)
		}
		var brewErr *Error
		if !errors.As(err, &brewErr) {
			t.Fatalf("ListInstalledFormulae() error = %#v, want *Error", err)
		}
		if !strings.Contains(brewErr.Error(), "boom") {
			t.Fatalf("error = %q, want it to carry brew stderr", brewErr.Error())
		}
	})

	t.Run("reports unparseable output as *Error", func(t *testing.T) {
		fakeBrewOnPath(t, emit("not json"))

		if _, err := ListInstalledFormulae(); err == nil {
			t.Fatal("ListInstalledFormulae() error = nil, want a parse error")
		}
	})
}

func TestListInstalledCasksEndToEnd(t *testing.T) {
	t.Run("sends the cask namespace query and parses installed casks", func(t *testing.T) {
		argvLog := fakeBrewOnPath(t, emit(installedInfoJSON))

		packages, err := ListInstalledCasks()
		if err != nil {
			t.Fatalf("ListInstalledCasks() error = %v", err)
		}

		assertArgv(t, argvLog, []string{"info --installed --json=v2 --cask"})

		if len(packages) == 0 {
			t.Fatal("ListInstalledCasks() returned no casks")
		}
		for _, pkg := range packages {
			if pkg.Name == "ripgrep" || pkg.Name == "jq" {
				t.Fatalf("ListInstalledCasks() leaked formula %q into the cask list", pkg.Name)
			}
		}
	})

	t.Run("propagates a brew failure", func(t *testing.T) {
		fakeBrewOnPath(t, "echo 'cask lookup failed' >&2\nexit 1")

		if _, err := ListInstalledCasks(); err == nil {
			t.Fatal("ListInstalledCasks() error = nil, want a brew failure")
		}
	})
}

func TestListOutdatedEndToEnd(t *testing.T) {
	const outdatedJSON = `{
  "formulae": [
    {"name": "ripgrep", "installed_versions": ["14.1.0", "13.0.0"], "current_version": "14.1.1", "pinned": true},
    {"name": "jq", "installed_versions": ["1.7.0"], "current_version": "1.7.1", "pinned": false}
  ],
  "casks": [
    {"name": "firefox", "installed_versions": ["130.0"], "current_version": "131.0"}
  ]
}`

	t.Run("sends the v2 outdated query and flattens both namespaces", func(t *testing.T) {
		argvLog := fakeBrewOnPath(t, emit(outdatedJSON))

		packages, err := ListOutdated()
		if err != nil {
			t.Fatalf("ListOutdated() error = %v", err)
		}

		assertArgv(t, argvLog, []string{"outdated --json=v2"})

		want := []Package{
			{Name: "ripgrep", Version: "14.1.0, 13.0.0", Outdated: true, Pinned: true},
			{Name: "jq", Version: "1.7.0", Outdated: true},
			{Name: "firefox", Version: "130.0", Outdated: true},
		}
		if !reflect.DeepEqual(packages, want) {
			t.Fatalf("ListOutdated() = %#v, want %#v", packages, want)
		}
	})

	t.Run("reports unparseable output as *Error", func(t *testing.T) {
		fakeBrewOnPath(t, emit("{"))

		packages, err := ListOutdated()
		if packages != nil {
			t.Fatalf("ListOutdated() packages = %#v, want nil", packages)
		}
		var brewErr *Error
		if !errors.As(err, &brewErr) {
			t.Fatalf("ListOutdated() error = %#v, want *Error", err)
		}
	})

	t.Run("propagates a brew failure", func(t *testing.T) {
		fakeBrewOnPath(t, "echo 'outdated failed' >&2\nexit 1")

		if _, err := ListOutdated(); err == nil {
			t.Fatal("ListOutdated() error = nil, want a brew failure")
		}
	})
}

// searchBody answers `brew search --formula|--cask <query>` per namespace so a
// single fake can serve both invocations Search makes.
const searchBody = `case "$2" in
  --formula) printf '%s' "$FORMULA_OUT" ;;
  --cask) printf '%s' "$CASK_OUT" ;;
esac
exit 0`

func TestSearchEndToEnd(t *testing.T) {
	t.Run("queries both namespaces with the trimmed query", func(t *testing.T) {
		argvLog := fakeBrewOnPath(t, searchBody)
		t.Setenv("FORMULA_OUT", "==> Formulae\nripgrep\nrga\n")
		t.Setenv("CASK_OUT", "==> Casks\nfirefox\n")

		if _, err := Search("  rg  "); err != nil {
			t.Fatalf("Search() error = %v", err)
		}

		assertArgv(t, argvLog, []string{"search --formula rg", "search --cask rg"})
	})

	t.Run("does not shell out for a blank query", func(t *testing.T) {
		argvLog := fakeBrewOnPath(t, searchBody)

		results, err := Search("   ")
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if results != nil {
			t.Fatalf("Search() = %#v, want nil", results)
		}
		assertArgv(t, argvLog, nil)
	})
}

func TestBrewIsInstalledEndToEnd(t *testing.T) {
	t.Run("true when brew --version succeeds", func(t *testing.T) {
		argvLog := fakeBrewOnPath(t, "exit 0")

		if !IsInstalled() {
			t.Fatal("IsInstalled() = false, want true")
		}
		assertArgv(t, argvLog, []string{"--version"})
	})

	t.Run("false when brew exits non-zero", func(t *testing.T) {
		fakeBrewOnPath(t, "exit 1")

		if IsInstalled() {
			t.Fatal("IsInstalled() = true, want false")
		}
	})

	t.Run("false when brew is absent from PATH and from the fallback", func(t *testing.T) {
		// Both resolution inputs are pinned: brew is nowhere on $PATH and the
		// Linuxbrew fallback does not exist, so the assertion holds on a host
		// that has a real Homebrew installed at the fallback path too.
		t.Setenv("PATH", t.TempDir())
		withHostResolution(t, "", absentPath(t))

		if IsInstalled() {
			t.Fatal("IsInstalled() = true, want false")
		}
	})
}

// TestCachedInstallCheckRunsOnce pins the sync.Once contract: the first answer
// is reused for the process lifetime even after $PATH changes underneath it.
// It is the only test that may consume the package-level installedOnce.
func TestCachedInstallCheckRunsOnce(t *testing.T) {
	installedOnce = sync.Once{}
	installedResult = false
	t.Cleanup(func() {
		installedOnce = sync.Once{}
		installedResult = false
	})
	argvLog := fakeBrewOnPath(t, "exit 0")

	first := IsInstalledCached()
	if !first {
		t.Fatal("IsInstalledCached() = false, want true with a working fake brew")
	}
	assertArgv(t, argvLog, []string{"--version"})

	t.Setenv("PATH", t.TempDir())
	if got := IsInstalledCached(); got != first {
		t.Fatalf("IsInstalledCached() = %v after PATH changed, want the cached %v", got, first)
	}
	assertArgv(t, argvLog, []string{"--version"})
}
