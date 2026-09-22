package updex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	updexapi "github.com/frostyard/updex/updex"

	updexconfig "github.com/frostyard/updex/config"
)

// hermeticRoot is the single systemd-style search root every test in this
// package sees in place of /etc, /run, /usr/local/lib and /usr/lib. It is
// installed by TestMain, before any test can construct the package's client
// singleton: updex captures its runtime paths once, at client construction,
// so a test that redirected the roots afterwards would still be reading the
// host's real feature definitions.
var hermeticRoot string

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "chairlift-updex-roots")
	if err != nil {
		panic("creating hermetic updex root: " + err.Error())
	}
	hermeticRoot = root

	osRelease := filepath.Join(root, "os-release")
	if err := os.WriteFile(osRelease, []byte("ID=snow\nIMAGE_NAME=snow-test\n"), 0o644); err != nil {
		panic("writing hermetic os-release: " + err.Error())
	}

	origRoots, origOSRelease := updexconfig.SearchRoots, updexconfig.OSReleasePaths
	updexconfig.SearchRoots = []string{root}
	updexconfig.OSReleasePaths = []string{osRelease}

	code := m.Run()

	updexconfig.SearchRoots, updexconfig.OSReleasePaths = origRoots, origOSRelease
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// definitionDir returns the hermetic sysupdate.d directory, empty, and keeps
// it empty for the next test. Definitions are read at call time (only the
// roots are captured at construction), so writing into it changes what the
// already-constructed singleton sees.
func definitionDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(hermeticRoot, "sysupdate.d")
	reset := func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("clearing hermetic definition dir: %v", err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating hermetic definition dir: %v", err)
		}
	}
	reset()
	t.Cleanup(reset)
	return dir
}

// writeFeature writes a minimal enabled feature and the sysext transfer that
// belongs to it — the smallest pair updex reports as one feature.
func writeFeature(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".feature"),
		[]byte("[Feature]\nEnabled=true\n"), 0o644); err != nil {
		t.Fatalf("writing %s.feature: %v", name, err)
	}
	transfer := `[Transfer]
Features=` + name + `

[Source]
Type=url-file
Path=https://example.com/` + name + `
MatchPattern=` + name + `_@v.raw

[Target]
Type=regular-file
Path=/var/lib/extensions.d
MatchPattern=` + name + `_@v.raw
CurrentSymlink=` + name + `.raw
`
	if err := os.WriteFile(filepath.Join(dir, name+".transfer"), []byte(transfer), 0o644); err != nil {
		t.Fatalf("writing %s.transfer: %v", name, err)
	}
}

func TestGetClientReturnsOneSharedClient(t *testing.T) {
	first := getClient()
	if first == nil {
		t.Fatal("getClient() = nil, want a client")
	}
	if second := getClient(); second != first {
		t.Fatalf("getClient() returned %p then %p, want the same singleton", first, second)
	}
}

// newClient is the package's single construction site: the singleton and
// featuresChecker's per-call, warning-capturing client both go through it.
// If it dropped the supplied config, featuresChecker would silently lose its
// reporter and every warning with it, so assert the config reaches the
// client by giving it roots that differ from the package defaults.
func TestNewClientAppliesTheSuppliedConfig(t *testing.T) {
	ownRoot := t.TempDir()
	dir := filepath.Join(ownRoot, "sysupdate.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating definition dir: %v", err)
	}
	writeFeature(t, dir, "scoped")

	client := newClient(updexapi.ClientConfig{
		Paths: updexapi.RuntimePaths{DefinitionRoots: []string{ownRoot}},
	})
	if client == nil {
		t.Fatal("newClient() = nil, want a client")
	}

	features, err := client.Features(context.Background())
	if err != nil {
		t.Fatalf("Features() on the configured client: %v", err)
	}
	if len(features) != 1 || features[0].Name != "scoped" {
		t.Fatalf("Features() = %v, want the single feature defined under the supplied DefinitionRoots", features)
	}
}

func TestListFeaturesReportsDefinedFeatures(t *testing.T) {
	dir := definitionDir(t)
	writeFeature(t, dir, "alpha")
	writeFeature(t, dir, "beta")

	features, err := ListFeatures(context.Background())
	if err != nil {
		t.Fatalf("ListFeatures(): %v", err)
	}

	got := make([]string, 0, len(features))
	for _, f := range features {
		got = append(got, f.Name)
	}
	want := map[string]bool{"alpha": true, "beta": true}
	if len(got) != len(want) {
		t.Fatalf("ListFeatures() = %v, want exactly %v", got, want)
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("ListFeatures() returned unexpected feature %q (all: %v)", name, got)
		}
	}
}

func TestListFeaturesReportsNoFeaturesWithoutDefinitions(t *testing.T) {
	definitionDir(t)

	features, err := ListFeatures(context.Background())
	if err != nil {
		t.Fatalf("ListFeatures() with no definitions returned an error: %v", err)
	}
	if len(features) != 0 {
		t.Fatalf("ListFeatures() = %v, want no features", features)
	}
}

// A malformed definition must surface as the package's own *Error — callers
// classify failures with errors.As on that type, and a raw library error
// would slip past them unrecognised.
func TestListFeaturesWrapsLoadFailuresInPackageError(t *testing.T) {
	dir := definitionDir(t)
	if err := os.WriteFile(filepath.Join(dir, "broken.feature"),
		[]byte("not an ini file at all\n"), 0o644); err != nil {
		t.Fatalf("writing malformed feature: %v", err)
	}

	features, err := ListFeatures(context.Background())
	if features != nil {
		t.Fatalf("ListFeatures() = %v on a load failure, want nil", features)
	}
	var updexErr *Error
	if !errors.As(err, &updexErr) {
		t.Fatalf("error = %T (%v), want *Error", err, err)
	}
	if !strings.HasPrefix(updexErr.Error(), "failed to list features:") {
		t.Fatalf("error = %q, want it to keep the \"failed to list features:\" prefix", updexErr.Error())
	}
	if !strings.Contains(updexErr.Error(), "broken.feature") {
		t.Fatalf("error = %q, want it to name the offending definition", updexErr.Error())
	}
}

// IsInstalledCached runs the availability probe at most once for the process:
// it is called from UI construction paths where an uncached probe would pay
// the 3-second listing timeout every time.
func TestCachedAvailabilityProbeRunsOnce(t *testing.T) {
	origLister, origResult := featuresLister, installedResult
	t.Cleanup(func() {
		featuresLister, installedResult = origLister, origResult
		installedOnce = sync.Once{}
	})

	var mu sync.Mutex
	calls := 0
	installedOnce, installedResult = sync.Once{}, false
	featuresLister = func(ctx context.Context) ([]Feature, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return []Feature{{Name: "demo", Enabled: true}}, nil
	}

	if !IsInstalledCached() {
		t.Fatal("IsInstalledCached() = false with a non-empty feature list, want true")
	}

	// The underlying probe now reports the opposite; the cached answer must
	// not move, and the probe must not run a second time.
	featuresLister = func(ctx context.Context) ([]Feature, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return nil, errors.New("updex went away")
	}
	if !IsInstalledCached() {
		t.Fatal("IsInstalledCached() = false on the second call, want the cached true")
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("availability probe ran %d times, want exactly 1", calls)
	}
}

func TestCachedAvailabilityProbeCachesUnavailability(t *testing.T) {
	origLister, origResult := featuresLister, installedResult
	t.Cleanup(func() {
		featuresLister, installedResult = origLister, origResult
		installedOnce = sync.Once{}
	})

	installedOnce, installedResult = sync.Once{}, true
	featuresLister = func(ctx context.Context) ([]Feature, error) {
		return []Feature{}, nil
	}

	if IsInstalledCached() {
		t.Fatal("IsInstalledCached() = true with an empty feature list, want false")
	}

	featuresLister = func(ctx context.Context) ([]Feature, error) {
		return []Feature{{Name: "demo", Enabled: true}}, nil
	}
	if IsInstalledCached() {
		t.Fatal("IsInstalledCached() = true after caching unavailability, want the cached false")
	}
}
