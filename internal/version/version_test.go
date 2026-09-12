package version

import "testing"

// The four build vars are injected by ldflags at release time; tests restore
// whatever the current build set so they pass in both dev and release builds.
func withBuildInfo(t *testing.T, ver, commit string) {
	t.Helper()
	origVersion, origCommit := Version, Commit
	t.Cleanup(func() {
		Version, Commit = origVersion, origCommit
	})
	Version, Commit = ver, commit
}

func TestDefaultBuildValues(t *testing.T) {
	// A binary built without ldflags must still report placeholders rather
	// than empty strings, so version output is never blank.
	for name, got := range map[string]string{
		"Version": Version,
		"Commit":  Commit,
		"Date":    Date,
		"BuiltBy": BuiltBy,
	} {
		if got == "" {
			t.Errorf("%s is empty, want a non-empty build placeholder", name)
		}
	}
}

func TestInfoReturnsVersionOnly(t *testing.T) {
	withBuildInfo(t, "1.2.3", "abcdef1")

	if got := Info(); got != "1.2.3" {
		t.Errorf("Info() = %q, want %q", got, "1.2.3")
	}
}

func TestFullCombinesVersionAndCommit(t *testing.T) {
	withBuildInfo(t, "1.2.3", "abcdef1")

	if got := Full(); got != "1.2.3 (abcdef1)" {
		t.Errorf("Full() = %q, want %q", got, "1.2.3 (abcdef1)")
	}
}

func TestFullWithUnsetLdflags(t *testing.T) {
	withBuildInfo(t, "dev", "unknown")

	if got := Full(); got != "dev (unknown)" {
		t.Errorf("Full() = %q, want %q", got, "dev (unknown)")
	}
}
