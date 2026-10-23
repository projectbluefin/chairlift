package homebrew

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The installed inventory must carry the package's qualified name and its
// tap: without them a collection entry that names a tap package cannot be
// told apart from a homebrew/core package of the same name.
func TestParsePackagesJSONReadsQualifiedNamesAndTaps(t *testing.T) {
	const data = `{
  "formulae": [
    {"name": "dcg", "full_name": "dtg01100/tap/dcg", "tap": "dtg01100/tap",
     "installed": [{"version": "1.0.0", "installed_on_request": true}], "pinned": false, "outdated": false},
    {"name": "gh", "full_name": "gh", "tap": "homebrew/core",
     "installed": [{"version": "2.0.0", "installed_on_request": true}], "pinned": false, "outdated": false}
  ],
  "casks": [
    {"token": "goose-linux", "full_token": "ublue-os/tap/goose-linux", "tap": "ublue-os/tap",
     "installed": "1.2.3", "outdated": false},
    {"token": "visual-studio-code", "full_token": "visual-studio-code", "tap": "homebrew/core",
     "installed": "1.104.0", "outdated": false}
  ]
}`

	formulae, err := parsePackagesJSON(data, true)
	if err != nil {
		t.Fatalf("parsePackagesJSON(formulae) returned error: %v", err)
	}
	wantFormulae := []Package{
		{Name: "dcg", FullName: "dtg01100/tap/dcg", Tap: "dtg01100/tap", Version: "1.0.0", InstalledOnRequest: true},
		{Name: "gh", FullName: "gh", Tap: "homebrew/core", Version: "2.0.0", InstalledOnRequest: true},
	}
	if !reflect.DeepEqual(formulae, wantFormulae) {
		t.Errorf("parsePackagesJSON(formulae) = %+v, want %+v", formulae, wantFormulae)
	}
	if !formulae[0].FromTap() {
		t.Error("a package from a tap must report FromTap")
	}
	if formulae[1].FromTap() {
		t.Error("a homebrew/core package must not report FromTap")
	}
	if got := formulae[0].QualifiedName(); got != "dtg01100/tap/dcg" {
		t.Errorf("QualifiedName() = %q, want the qualified name", got)
	}

	casks, err := parsePackagesJSON(data, false)
	if err != nil {
		t.Fatalf("parsePackagesJSON(casks) returned error: %v", err)
	}
	wantCasks := []Package{
		{Name: "goose-linux", FullName: "ublue-os/tap/goose-linux", Tap: "ublue-os/tap", Version: "1.2.3"},
		{Name: "visual-studio-code", FullName: "visual-studio-code", Tap: "homebrew/core", Version: "1.104.0"},
	}
	if !reflect.DeepEqual(casks, wantCasks) {
		t.Errorf("parsePackagesJSON(casks) = %+v, want %+v", casks, wantCasks)
	}
}

// A package whose tap is unknown must not be reported as coming from a
// third-party tap: an empty tap field means Homebrew did not say.
func TestPackageFromTapNeedsAKnownTap(t *testing.T) {
	if (Package{Name: "gh"}).FromTap() {
		t.Error("a package with no tap must not report FromTap")
	}
	if (Package{Name: "gh", Tap: CoreTap}).FromTap() {
		t.Error("a homebrew/core package must not report FromTap")
	}
}

// One brew transaction per kind: a partially selected collection installs
// its formulae in a single command, not one command per formula.
func TestBatchedInstallRunsOneTransactionPerKind(t *testing.T) {
	var err error
	got := captureSingleBrewArgv(t, func() { err = InstallMany([]string{"gh", "jq"}, false) })
	if err != nil {
		t.Fatalf("InstallMany returned error %v, want nil in dry-run mode", err)
	}
	if got != "install gh jq" {
		t.Errorf("brew argv = %q, want %q", got, "install gh jq")
	}

	got = captureSingleBrewArgv(t, func() { err = InstallMany([]string{"font-hack"}, true) })
	if err != nil {
		t.Fatalf("InstallMany returned error %v, want nil in dry-run mode", err)
	}
	if got != "install --cask font-hack" {
		t.Errorf("brew argv = %q, want %q", got, "install --cask font-hack")
	}
}

// An empty list must never reach brew: `brew install` with no argument is an
// error, and the caller has nothing to install in that case.
func TestBatchedInstallWithNoNamesRunsNoCommand(t *testing.T) {
	argv := captureBrewArgv(t, func() {
		if err := InstallMany(nil, false); err != nil {
			t.Errorf("InstallMany(nil) returned error %v, want nil", err)
		}
	})
	if len(argv) != 0 {
		t.Errorf("InstallMany(nil) ran %q, want no command", argv)
	}
}

// Trusting a whole tap is the breadth `brew bundle` gives a tap its Brewfile
// marks trusted, and it is a different granularity from the per-package one.
func TestTrustTapBuildsTheTapGranularity(t *testing.T) {
	var err error
	got := captureSingleBrewArgv(t, func() { err = TrustTap("ublue-os/tap") })
	if err != nil {
		t.Fatalf("TrustTap returned error %v, want nil in dry-run mode", err)
	}
	if got != "trust --tap ublue-os/tap" {
		t.Errorf("brew argv = %q, want %q", got, "trust --tap ublue-os/tap")
	}
}

// The trust store names the taps trusted wholesale beside the packages
// trusted one by one; without the taps, an install cannot tell a wholesale
// trust from a per-package one.
func TestParseTrustedPackagesReadsTheTaps(t *testing.T) {
	set, err := parseTrustedPackages([]byte(`{
  "taps": ["ublue-os/tap", "anomalyco/tap"],
  "formulae": ["llmmanorg/tap/llmman"],
  "casks": ["ublue-os/tap/goose-linux"],
  "commands": []
}`))
	if err != nil {
		t.Fatalf("parseTrustedPackages returned error: %v", err)
	}
	if !set.taps["ublue-os/tap"] || !set.taps["anomalyco/tap"] {
		t.Errorf("taps = %v, want both taps trusted", set.taps)
	}
	if set.taps["llmmanorg/tap"] {
		t.Error("a tap that only one package came from is not a trusted tap")
	}
	if !set.formulae["llmmanorg/tap/llmman"] || !set.casks["ublue-os/tap/goose-linux"] {
		t.Errorf("packages = %v / %v, want both packages trusted", set.formulae, set.casks)
	}
}

// The trust store's lookups are case-insensitive, because brew lowercases
// every entry it stores.
func TestTrustStoreLookupsAreCaseInsensitive(t *testing.T) {
	store := TrustStore{
		taps:     map[string]bool{"ublue-os/tap": true},
		formulae: map[string]bool{"llmmanorg/tap/llmman": true},
		casks:    map[string]bool{"ublue-os/tap/goose-linux": true},
	}
	if !store.TapTrusted("Ublue-OS/Tap") {
		t.Error("TapTrusted must ignore case")
	}
	if !store.PackageTrusted(Formula, "LLMMANORG/tap/llmman") {
		t.Error("PackageTrusted must ignore case for formulae")
	}
	if !store.PackageTrusted(Cask, "Ublue-OS/tap/goose-linux") {
		t.Error("PackageTrusted must ignore case for casks")
	}
	if store.PackageTrusted(Cask, "llmmanorg/tap/llmman") {
		t.Error("a formula's trust must not answer for a cask of the same name")
	}
}

// tap-info reports whether a tap is installed and whether it is trusted. The
// trust key is optional: Homebrew before 6 has no tap trust at all.
func TestParseTapInfoReadsInstalledAndTrusted(t *testing.T) {
	state, err := parseTapInfo([]byte(`[{"name":"ublue-os/tap","installed":true,"trusted":true}]`), "ublue-os/tap")
	if err != nil {
		t.Fatalf("parseTapInfo returned error: %v", err)
	}
	if !state.Installed || !state.Trusted || !state.HasTrust {
		t.Errorf("state = %+v, want installed and trusted", state)
	}

	state, err = parseTapInfo([]byte(`[{"name":"someowner/sometap","installed":false,"trusted":false}]`), "someowner/sometap")
	if err != nil {
		t.Fatalf("parseTapInfo returned error: %v", err)
	}
	if state.Installed || state.Trusted || !state.HasTrust {
		t.Errorf("state = %+v, want an untapped, untrusted tap", state)
	}

	state, err = parseTapInfo([]byte(`[{"name":"old/tap","installed":true}]`), "old/tap")
	if err != nil {
		t.Fatalf("parseTapInfo returned error: %v", err)
	}
	if state.HasTrust {
		t.Error("a tap-info without a trust key must report no trust concept")
	}

	if _, err := parseTapInfo([]byte(`[]`), "ublue-os/tap"); err == nil {
		t.Error("parseTapInfo must fail when the tap is not in the answer")
	}
}

// A Brewfile's tap directives carry the trust the distribution declared for
// them; BundleContents drops them, so they are parsed on their own.
func TestBundleTapsReadsTheDeclaredTrust(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ai-tools.Brewfile")
	body := "" +
		"# a comment\n" +
		"tap \"anomalyco/tap\"\n" +
		"tap \"charmbracelet/tap\"\n" +
		"tap \"ublue-os/tap\", trusted: true\n" +
		"tap \"projectbluefin/tap\", trusted: true\n" +
		"tap \"colindean/fonts-nonfree\", trusted: { formula: [\"font-foo\"] }\n" +
		"tap \"notatap\"\n" +
		"brew \"gh\"\n" +
		"tap \"anomalyco/tap\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	taps, err := BundleTaps(path)
	if err != nil {
		t.Fatalf("BundleTaps returned error: %v", err)
	}
	want := []BundleTap{
		{Name: "anomalyco/tap"},
		{Name: "charmbracelet/tap"},
		{Name: "ublue-os/tap", Trusted: true},
		{Name: "projectbluefin/tap", Trusted: true},
		{Name: "colindean/fonts-nonfree"},
	}
	if !reflect.DeepEqual(taps, want) {
		t.Errorf("BundleTaps = %+v, want %+v", taps, want)
	}
}

// A tap directive that names no repository is not a tap and must never
// become a trust target.
func TestBundleTapsSkipsMalformedDirectives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.Brewfile")
	body := "" +
		"tap\n" +
		"tap \"unclosed\n" +
		"tap \"norepository\"\n" +
		"tap ''\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	taps, err := BundleTaps(path)
	if err != nil {
		t.Fatalf("BundleTaps returned error: %v", err)
	}
	if len(taps) != 0 {
		t.Errorf("BundleTaps = %+v, want none", taps)
	}
}

// A missing Brewfile is an error, not an empty list: the caller cannot tell
// "no taps" from "no file" otherwise.
func TestBundleTapsOnAMissingFileReturnsError(t *testing.T) {
	if _, err := BundleTaps(filepath.Join(t.TempDir(), "absent.Brewfile")); err == nil {
		t.Error("BundleTaps must fail on a missing file")
	}
}

// fakeTapBrew points brew at a script that answers the tap-info query
// PrepareInstall makes with tapInfo, and records every argv it is given.
func fakeTapBrew(t *testing.T, tapInfo string) string {
	t.Helper()

	script, _ := fakeBrewExecutable(t, "case \"$1\" in\n  tap-info) printf '%s' '"+tapInfo+"' ;;\nesac\nexit 0")
	withHostResolution(t, script, "")
	return script
}

// The trust decision is the narrow one by default, and the breadth a
// Brewfile's own `trusted: true` asks for is the exception.
func TestPrepareInstallTrustsOnePackageByDefault(t *testing.T) {
	fakeTapBrew(t, `[{"name":"ublue-os/tap","installed":true,"trusted":false}]`)
	store := TrustStore{}

	change, err := PrepareInstall("ublue-os/tap/linux-mcp-server", Formula, &store, map[string]bool{})
	if err != nil {
		t.Fatalf("PrepareInstall returned error: %v", err)
	}
	if change != TrustChangedPackage {
		t.Errorf("change = %v, want a package trust", change)
	}
	if !store.PackageTrusted(Formula, "ublue-os/tap/linux-mcp-server") {
		t.Error("the package must be recorded as trusted")
	}
}

// A homebrew/core package needs neither a tap nor a trust.
func TestPrepareInstallLeavesCorePackagesAlone(t *testing.T) {
	fakeTapBrew(t, `[]`)
	store := TrustStore{}

	change, err := PrepareInstall("gh", Formula, &store, nil)
	if err != nil {
		t.Fatalf("PrepareInstall returned error: %v", err)
	}
	if change != TrustUnchanged {
		t.Errorf("change = %v, want no change", change)
	}
}

// A package that is already trusted changes nothing, so the run does not
// report a trust change it did not make.
func TestPrepareInstallLeavesTrustedPackagesAlone(t *testing.T) {
	fakeTapBrew(t, `[{"name":"ublue-os/tap","installed":true,"trusted":false}]`)
	store := TrustStore{formulae: map[string]bool{"ublue-os/tap/linux-mcp-server": true}}

	change, err := PrepareInstall("ublue-os/tap/linux-mcp-server", Formula, &store, nil)
	if err != nil {
		t.Fatalf("PrepareInstall returned error: %v", err)
	}
	if change != TrustUnchanged {
		t.Errorf("change = %v, want no change", change)
	}
}

// A tap the collection's own Brewfile marks trusted is trusted wholesale,
// the way `brew bundle` trusts it.
func TestPrepareInstallTrustsTheWholeTapWhenTheBrewfileDeclaresIt(t *testing.T) {
	fakeTapBrew(t, `[{"name":"ublue-os/tap","installed":true,"trusted":false}]`)
	store := TrustStore{}
	declared := map[string]bool{"ublue-os/tap": true}

	change, err := PrepareInstall("ublue-os/tap/linux-mcp-server", Formula, &store, declared)
	if err != nil {
		t.Fatalf("PrepareInstall returned error: %v", err)
	}
	if change != TrustChangedTap {
		t.Errorf("change = %v, want a tap trust", change)
	}
	if !store.TapTrusted("ublue-os/tap") {
		t.Error("the tap must be recorded as trusted")
	}
}

// A tap that is already trusted wholesale needs nothing: the package loads
// as it is.
func TestPrepareInstallLeavesTrustedTapsAlone(t *testing.T) {
	fakeTapBrew(t, `[{"name":"ublue-os/tap","installed":true,"trusted":true}]`)
	store := TrustStore{}

	change, err := PrepareInstall("ublue-os/tap/linux-mcp-server", Formula, &store, map[string]bool{})
	if err != nil {
		t.Fatalf("PrepareInstall returned error: %v", err)
	}
	if change != TrustUnchanged {
		t.Errorf("change = %v, want no change", change)
	}
}

// Homebrew before 6 has no tap trust at all: nothing needs trusting and the
// package loads as it is.
func TestPrepareInstallTrustsNothingWithoutATrustConcept(t *testing.T) {
	fakeTapBrew(t, `[{"name":"ublue-os/tap","installed":true}]`)
	store := TrustStore{}

	change, err := PrepareInstall("ublue-os/tap/linux-mcp-server", Formula, &store, map[string]bool{})
	if err != nil {
		t.Fatalf("PrepareInstall returned error: %v", err)
	}
	if change != TrustUnchanged {
		t.Errorf("change = %v, want no change", change)
	}
}

// A tap that is not there yet is cloned before its package is installed.
func TestPrepareInstallClonesAMissingTap(t *testing.T) {
	script, argvLog := fakeBrewExecutable(t, "case \"$1\" in\n  tap-info) printf '%s' '[{\"name\":\"ublue-os/tap\",\"installed\":false,\"trusted\":false}]' ;;\nesac\nexit 0")
	withHostResolution(t, script, "")
	store := TrustStore{}

	if _, err := PrepareInstall("ublue-os/tap/linux-mcp-server", Formula, &store, map[string]bool{}); err != nil {
		t.Fatalf("PrepareInstall returned error: %v", err)
	}
	argv := recordedArgv(t, argvLog)
	if len(argv) < 2 || argv[1] != "tap ublue-os/tap" {
		t.Errorf("brew invocations = %#v, want the tap cloned before the trust", argv)
	}
}

// The trust flags are the ones brew uses: a formula and a cask are trusted
// at different granularities.
func TestTrustFlagsAreTheOnesBrewUses(t *testing.T) {
	if got := trustFlag(Formula); got != "--formula" {
		t.Errorf("trustFlag(Formula) = %q, want %q", got, "--formula")
	}
	if got := trustFlag(Cask); got != "--cask" {
		t.Errorf("trustFlag(Cask) = %q, want %q", got, "--cask")
	}
}
