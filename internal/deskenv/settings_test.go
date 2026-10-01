package deskenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsModulesUnifiesTheNativeStoreWithoutOverridingTestBackends(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "libdconfsettings.so"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	previous := settingsModuleDirs
	settingsModuleDirs = []string{filepath.Join(dir, "missing"), dir}
	t.Cleanup(func() { settingsModuleDirs = previous })
	t.Setenv("GIO_EXTRA_MODULES", "/existing/modules")
	t.Setenv("GSETTINGS_BACKEND", "memory")
	if err := ConfigureSettingsModules(); err != nil {
		t.Fatal(err)
	}
	want := "/existing/modules" + string(os.PathListSeparator) + dir
	if got := os.Getenv("GIO_EXTRA_MODULES"); got != want {
		t.Fatalf("native settings module discovery = %q, want %q", got, want)
	}
	if got := os.Getenv("GSETTINGS_BACKEND"); got != "memory" {
		t.Fatalf("explicit backend changed to %q", got)
	}
	if err := ConfigureSettingsModules(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("GIO_EXTRA_MODULES"); got != want {
		t.Fatalf("repeated startup duplicated a module directory: %q", got)
	}
}
