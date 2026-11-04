package flatpak

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// installLoggingFlatpak installs a fake flatpak that appends each argv as one
// line, so tests can assert every invocation rather than only the last.
func installLoggingFlatpak(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "flatpak"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return log
}

func loggedArgv(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestDescribeApps(t *testing.T) {
	log := installLoggingFlatpak(t, `case "$3" in
 org.gnome.Calculator) printf 'org.gnome.Calculator\tPerform  arithmetic\n' ;;
 org.mozilla.firefox) printf 'org.mozilla.firefox.Nightly\tNightly build\norg.mozilla.firefox\tFast web browser\n' ;;
 com.example.Missing) echo 'No matches found' ;;
 com.example.Broken) echo 'error: broken' >&2; exit 1 ;;
 esac`)
	got, err := DescribeApps(context.Background(), []string{
		"org.gnome.Calculator", "org.mozilla.firefox", "org.gnome.Calculator",
		"com.example.Missing", "com.example.Broken", "--system", "../etc", "app/org.x.Y/x86_64/stable", "nodots",
	})
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("err = %v, want the failed search reported", err)
	}
	want := map[string]string{"org.gnome.Calculator": "Perform arithmetic", "org.mozilla.firefox": "Fast web browser"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	wantArgv := []string{
		"search --columns=application,description org.gnome.Calculator",
		"search --columns=application,description org.mozilla.firefox",
		"search --columns=application,description com.example.Missing",
		"search --columns=application,description com.example.Broken",
	}
	if argv := loggedArgv(t, log); !reflect.DeepEqual(argv, wantArgv) {
		t.Fatalf("argv = %q, want %q", argv, wantArgv)
	}
}

func TestDescribeAppsStopsWhenCanceled(t *testing.T) {
	log := installLoggingFlatpak(t, "exit 0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := DescribeApps(ctx, []string{"org.gnome.Calculator", "org.gnome.Maps"})
	if !errors.Is(err, context.Canceled) || len(got) != 0 {
		t.Fatalf("got %#v, %v; want nothing and context.Canceled", got, err)
	}
	if argv := loggedArgv(t, log); len(argv) != 0 {
		t.Fatalf("ran %q after cancellation", argv)
	}
}

func TestDescribeAppsWithoutFlatpak(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got, err := DescribeApps(context.Background(), []string{"org.gnome.Calculator"})
	var notFound *NotFoundError
	if !errors.As(err, &notFound) || len(got) != 0 {
		t.Fatalf("got %#v, %v; want nothing and NotFoundError", got, err)
	}
}
