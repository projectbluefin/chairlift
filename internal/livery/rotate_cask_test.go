package livery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// caskInstall lays out a Homebrew prefix the way a cask install does:
// the binary under Caskroom/<token>/<version>/ and a bin/ symlink to it.
// It returns the prefix and the versioned binary path os.Executable reports.
func caskInstall(t *testing.T, version string) (prefix, versioned string) {
	t.Helper()
	prefix = t.TempDir()
	versioned = filepath.Join(prefix, "Caskroom", "chairlift", version, "chairlift")
	if err := os.MkdirAll(filepath.Dir(versioned), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(versioned, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(prefix, "bin", "chairlift")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versioned, link); err != nil {
		t.Fatal(err)
	}
	return prefix, versioned
}

func stubExecutable(t *testing.T, path string) {
	t.Helper()
	original := executablePath
	executablePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { executablePath = original })
}

func rotationUnitFor(t *testing.T, exe string) string {
	t.Helper()
	quoted, err := systemdQuote(exe)
	if err != nil {
		t.Fatal(err)
	}
	return "[Service]\nType=oneshot\nExecStart=" + quoted + " " + RotateFlag + "\n"
}

// TestRotationUnitNamesTheCaskBinLinkNotTheVersionedBinary is #491: the
// versioned Caskroom directory is deleted by the next cask upgrade, so the
// unit must name the prefix's bin link, which the upgrade repoints.
func TestRotationUnitNamesTheCaskBinLinkNotTheVersionedBinary(t *testing.T) {
	newFakeCommands(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GSETTINGS_SCHEMA_DIR", "")
	prefix, versioned := caskInstall(t, "26.10.2")
	stubExecutable(t, versioned)

	if err := InstallRotation(context.Background()); err != nil {
		t.Fatalf("InstallRotation: %v", err)
	}
	path, err := UnitPath()
	if err != nil {
		t.Fatal(err)
	}
	unit, ok := unitBody(t, path)
	if !ok {
		t.Fatal("no unit was written")
	}
	recorded, ok := unitExecStart(unit)
	if !ok {
		t.Fatalf("the unit has no readable ExecStart:\n%s", unit)
	}
	if want := filepath.Join(prefix, "bin", "chairlift"); recorded != want {
		t.Errorf("ExecStart = %q, want the stable link %q", recorded, want)
	}
	if strings.Contains(unit, "Caskroom") {
		t.Errorf("the unit still names a versioned Caskroom path:\n%s", unit)
	}
	if !RunsFromStablePath() {
		t.Error("a cask install reached through its bin link was reported as an unstable path")
	}
}

// TestStableExecutableKeepsPathsItCannotVouchFor covers the two cases where
// mapping would point the unit at a different program.
func TestStableExecutableKeepsPathsItCannotVouchFor(t *testing.T) {
	prefix, versioned := caskInstall(t, "26.10.2")

	other := filepath.Join(t.TempDir(), "chairlift")
	if err := os.WriteFile(other, []byte("another build"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(prefix, "bin", "chairlift")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if got := stableExecutable(versioned); got != versioned {
		t.Errorf("a bin link to another build was used: got %q", got)
	}

	source := "/home/dev/src/chairlift/build/chairlift"
	if got := stableExecutable(source); got != source {
		t.Errorf("a source build path was rewritten to %q", got)
	}
}

// TestReconcileRepointsAUnitWrittenWithAVersionedCaskPath migrates units
// installed before #491: the running release rewrites a unit that names an
// older Caskroom version to the bin link.
func TestReconcileRepointsAUnitWrittenWithAVersionedCaskPath(t *testing.T) {
	f := newFakeCommands(t)
	path := installedUnit(t, "")
	t.Setenv("GSETTINGS_SCHEMA_DIR", "")
	prefix, versioned := caskInstall(t, "26.11.1")
	stubExecutable(t, versioned)
	stale := filepath.Join(prefix, "Caskroom", "chairlift", "26.10.2", "chairlift")
	if err := writeFileAtomically(path, []byte(rotationUnitFor(t, stale))); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileRotationUnit(context.Background()); err != nil {
		t.Fatalf("ReconcileRotationUnit: %v", err)
	}
	unit, _ := unitBody(t, path)
	recorded, _ := unitExecStart(unit)
	if want := filepath.Join(prefix, "bin", "chairlift"); recorded != want {
		t.Errorf("ExecStart = %q after reconcile, want %q", recorded, want)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "systemctl --user daemon-reload") {
		t.Errorf("the rewritten unit was not reloaded: %v", f.calls)
	}
}

// TestReconcileLeavesOtherUnitsAlone holds the migration to its one case: no
// unit, or a unit naming a non-Caskroom path, is not written.
func TestReconcileLeavesOtherUnitsAlone(t *testing.T) {
	f := newFakeCommands(t)
	path := installedUnit(t, "")
	_, versioned := caskInstall(t, "26.11.1")
	stubExecutable(t, versioned)

	if err := ReconcileRotationUnit(context.Background()); err != nil {
		t.Fatalf("ReconcileRotationUnit without a unit: %v", err)
	}
	if _, ok := unitBody(t, path); ok {
		t.Fatal("reconcile created a unit nobody asked for")
	}

	source := rotationUnitFor(t, "/home/a b/100% done/chairlift")
	if err := writeFileAtomically(path, []byte(source)); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRotationUnit(context.Background()); err != nil {
		t.Fatalf("ReconcileRotationUnit: %v", err)
	}
	if unit, _ := unitBody(t, path); unit != source {
		t.Errorf("a unit naming a source build was rewritten:\n%s", unit)
	}
	if len(f.calls) != 0 {
		t.Errorf("reconcile ran commands for units it should not touch: %v", f.calls)
	}
}
