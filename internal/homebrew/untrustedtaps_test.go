package homebrew

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

// brewTrustStub builds a fake-brew body that answers the two commands
// ListUntrustedTaps issues — `tap-info --installed --json` and `--prefix` —
// and fails loudly on anything else, so an unexpected third invocation
// surfaces as a test failure rather than as empty output.
func brewTrustStub(tapInfo, prefix string) string {
	return "case \"$1\" in\n" +
		"tap-info) printf '%s' '" + tapInfo + "' ;;\n" +
		"--prefix) printf '%s\\n' '" + prefix + "' ;;\n" +
		"*) echo \"unexpected: $*\" >&2; exit 1 ;;\n" +
		"esac"
}

// brewTrustPrefix stages a Homebrew prefix whose Cellar and Caskroom carry
// the install receipts installedFormulaeByTap and installedCasksByTap read.
// formulae maps keg name -> source tap; casks maps cask token -> source tap.
func brewTrustPrefix(t *testing.T, formulae, casks map[string]string) string {
	t.Helper()
	prefix := t.TempDir()
	for keg, tap := range formulae {
		writeFile(t, filepath.Join(prefix, "Cellar", keg, "1.0", "INSTALL_RECEIPT.json"),
			`{"source": {"tap": "`+tap+`"}}`)
	}
	for token, tap := range casks {
		writeFile(t, filepath.Join(prefix, "Caskroom", token, ".metadata", "INSTALL_RECEIPT.json"),
			`{"source": {"tap": "`+tap+`"}}`)
	}
	return prefix
}

// TestListUntrustedTapsReportsInstalledPackages drives the whole composition
// end to end: brew reports four taps, two of them untrusted, and the receipts
// on disk decide which of those two are actionable. The result must carry
// fully qualified names — that is what `brew trust` is handed by
// TrustPackages — and must be sorted at both levels so the remediation UI
// does not reorder between refreshes.
func TestListUntrustedTapsReportsInstalledPackages(t *testing.T) {
	prefix := brewTrustPrefix(t,
		map[string]string{
			"multica": "multica-ai/tap",
			"zx":      "multica-ai/tap",
			"jq":      "homebrew/core",
		},
		map[string]string{
			"somecask": "ublue-os/tap",
			"codex":    "homebrew/cask",
		})
	argvLog := fakeBrewOnPath(t, brewTrustStub(tapInfoJSON, prefix))

	taps, err := ListUntrustedTaps()
	if err != nil {
		t.Fatalf("ListUntrustedTaps: %v", err)
	}

	want := []UntrustedTap{
		{
			Name:     "multica-ai/tap",
			Formulae: []string{"multica-ai/tap/multica", "multica-ai/tap/zx"},
		},
		{
			Name:  "ublue-os/tap",
			Casks: []string{"ublue-os/tap/somecask"},
		},
	}
	if !reflect.DeepEqual(taps, want) {
		t.Errorf("ListUntrustedTaps() = %#v, want %#v", taps, want)
	}

	assertArgv(t, argvLog, []string{"tap-info --installed --json", "--prefix"})
}

// TestListUntrustedTapsSkipsTapsWithNothingInstalled pins the "not
// actionable" rule: a tap brew marks untrusted but from which nothing is
// installed has no package to hand `brew trust`, so it must be dropped
// entirely rather than surfaced as an empty row.
func TestListUntrustedTapsSkipsTapsWithNothingInstalled(t *testing.T) {
	prefix := brewTrustPrefix(t, map[string]string{"jq": "homebrew/core"}, nil)
	fakeBrewOnPath(t, brewTrustStub(tapInfoJSON, prefix))

	taps, err := ListUntrustedTaps()
	if err != nil {
		t.Fatalf("ListUntrustedTaps: %v", err)
	}
	if len(taps) != 0 {
		t.Errorf("ListUntrustedTaps() = %#v, want no actionable taps", taps)
	}
}

// TestListUntrustedTapsWithoutUntrustedTapsSkipsPrefix covers the early
// return: when every installed tap is trusted there is nothing to correlate,
// so the receipt scan — and the second brew invocation that locates it — must
// not happen at all.
func TestListUntrustedTapsWithoutUntrustedTapsSkipsPrefix(t *testing.T) {
	const allTrusted = `[{"name":"homebrew/core","installed":true,"trusted":true}]`
	argvLog := fakeBrewOnPath(t, brewTrustStub(allTrusted, t.TempDir()))

	taps, err := ListUntrustedTaps()
	if err != nil {
		t.Fatalf("ListUntrustedTaps: %v", err)
	}
	if taps != nil {
		t.Errorf("ListUntrustedTaps() = %#v, want nil", taps)
	}
	assertArgv(t, argvLog, []string{"tap-info --installed --json"})
}

// TestListUntrustedTapsMissingPrefixYieldsNoPackages covers a prefix that
// exists but holds neither Cellar nor Caskroom: the directory reads fail, the
// maps come back empty, and every untrusted tap is therefore non-actionable.
// It must degrade to an empty result rather than an error.
func TestListUntrustedTapsMissingPrefixYieldsNoPackages(t *testing.T) {
	fakeBrewOnPath(t, brewTrustStub(tapInfoJSON, filepath.Join(t.TempDir(), "absent")))

	taps, err := ListUntrustedTaps()
	if err != nil {
		t.Fatalf("ListUntrustedTaps: %v", err)
	}
	if len(taps) != 0 {
		t.Errorf("ListUntrustedTaps() = %#v, want no actionable taps", taps)
	}
}

// TestListUntrustedTapsPropagatesTapInfoFailure asserts the first brew call's
// failure is returned, not swallowed into an empty list: an empty list is
// indistinguishable from "everything is trusted" and would quietly hide
// untrusted packages from the UI.
func TestListUntrustedTapsPropagatesTapInfoFailure(t *testing.T) {
	argvLog := fakeBrewOnPath(t, "echo 'Error: brew is broken' >&2; exit 1")

	taps, err := ListUntrustedTaps()
	if err == nil {
		t.Fatalf("ListUntrustedTaps() = %#v, want error", taps)
	}
	if taps != nil {
		t.Errorf("ListUntrustedTaps() = %#v, want nil result alongside the error", taps)
	}
	var brewErr *Error
	if !errors.As(err, &brewErr) {
		t.Errorf("error %v (%T), want *homebrew.Error", err, err)
	}
	assertArgv(t, argvLog, []string{"tap-info --installed --json"})
}

// TestListUntrustedTapsRejectsMalformedTapInfo covers brew answering
// successfully with output that is not the expected JSON array.
func TestListUntrustedTapsRejectsMalformedTapInfo(t *testing.T) {
	fakeBrewOnPath(t, emit("not json"))

	if taps, err := ListUntrustedTaps(); err == nil {
		t.Fatalf("ListUntrustedTaps() = %#v, want parse error", taps)
	}
}

// TestListUntrustedTapsPropagatesPrefixFailure covers the second brew call
// failing after the first succeeded: brew answers tap-info but not --prefix,
// so the Cellar location is unknown and the correlation cannot be completed.
func TestListUntrustedTapsPropagatesPrefixFailure(t *testing.T) {
	body := "case \"$1\" in\n" +
		"tap-info) printf '%s' '" + tapInfoJSON + "' ;;\n" +
		"*) exit 1 ;;\n" +
		"esac"
	argvLog := fakeBrewOnPath(t, body)

	if taps, err := ListUntrustedTaps(); err == nil {
		t.Fatalf("ListUntrustedTaps() = %#v, want the --prefix failure", taps)
	}
	assertArgv(t, argvLog, []string{"tap-info --installed --json", "--prefix"})
}

// TestBrewPrefixTrimsTrailingNewline pins the trim: `brew --prefix` prints a
// trailing newline, and an untrimmed value would be joined into
// "<prefix>\n/Cellar", a path that never exists.
func TestBrewPrefixTrimsTrailingNewline(t *testing.T) {
	argvLog := fakeBrewOnPath(t, "printf '%s\\n' '/home/linuxbrew/.linuxbrew'")

	prefix, err := brewPrefix()
	if err != nil {
		t.Fatalf("brewPrefix: %v", err)
	}
	if prefix != "/home/linuxbrew/.linuxbrew" {
		t.Errorf("brewPrefix() = %q, want the path with no surrounding whitespace", prefix)
	}
	assertArgv(t, argvLog, []string{"--prefix"})
}

// TestBrewPrefixWithoutBrewReportsNotFound covers brew being absent from
// $PATH entirely, which must surface as the actionable NotFoundError rather
// than a bare exec failure.
func TestBrewPrefixWithoutBrewReportsNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	prefix, err := brewPrefix()
	if err == nil {
		t.Fatalf("brewPrefix() = %q, want error with no brew on PATH", prefix)
	}
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("error %v (%T), want *homebrew.NotFoundError", err, err)
	}
	if prefix != "" {
		t.Errorf("brewPrefix() = %q, want empty prefix alongside the error", prefix)
	}
}
