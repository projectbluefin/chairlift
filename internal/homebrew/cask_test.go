package homebrew

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// writeCaskReceipt creates the Caskroom state Homebrew writes for one
// installed cask: the receipt that records its source tap and the versioned
// staged directory.
func writeCaskReceipt(t *testing.T, prefix, token, tap, version string) {
	t.Helper()
	writeFile(t, filepath.Join(prefix, "Caskroom", token, ".metadata", "INSTALL_RECEIPT.json"),
		`{"source": {"tap": "`+tap+`"}}`)
	if err := os.MkdirAll(filepath.Join(prefix, "Caskroom", token, version), 0o755); err != nil {
		t.Fatal(err)
	}
}

// caskFixture installs a fake brew whose `--prefix` and `--repository` probes
// answer with a temporary Homebrew layout. The body is appended to the same
// script, so a test supplies only the command-specific behavior it cares
// about. unsetDryRun restores the process-wide preview flag afterwards.
func caskFixture(t *testing.T, body string) (prefix, repo, argvLog string) {
	t.Helper()
	root := t.TempDir()
	prefix = filepath.Join(root, "prefix")
	repo = filepath.Join(root, "prefix", "Homebrew")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	argvLog = fakeBrewOnPath(t, `case "$1" in
--prefix) printf '%s' '`+prefix+`' ;;
--repository) printf '%s' '`+repo+`' ;;
`+body+`
esac`)
	before := dryrun.Enabled()
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(before) })
	return prefix, repo, argvLog
}

const ambiguityStderr = `echo 'Error: Cask chairlift exists in multiple taps:' >&2
echo '       * projectbluefin/tap/chairlift' >&2
echo '       * ublue-os/tap/chairlift' >&2
exit 1`

func TestCaskReceiptsNameTheInstalledTap(t *testing.T) {
	prefix, _, _ := caskFixture(t, "")
	writeCaskReceipt(t, prefix, "chairlift", "projectbluefin/tap", "26.11.7")
	writeCaskReceipt(t, prefix, "bluefin-wallpapers-extra", "ublue-os/tap", "0.1.0")
	if err := os.MkdirAll(filepath.Join(prefix, "Caskroom", "no-receipt", "1.0"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := InstalledCaskTaps()
	if err != nil {
		t.Fatalf("InstalledCaskTaps() error = %v", err)
	}
	want := map[string]string{
		"chairlift":                "projectbluefin/tap",
		"bluefin-wallpapers-extra": "ublue-os/tap",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InstalledCaskTaps() = %v, want %v", got, want)
	}
}

// Two taps define chairlift. Homebrew's JSON may name either one when it
// loads a cask definition; the receipt named the tap the copy came from, and
// that is the answer the Apps page and the OEM inventory must match against.
func TestListInstalledCasksQualifiesTapFromTheReceipt(t *testing.T) {
	prefix, _, _ := caskFixture(t, `info) printf '%s' '{"casks":[{"token":"chairlift","full_token":"ublue-os/tap/chairlift","tap":"ublue-os/tap","installed":"26.11.7"}]}' ;;`)
	writeCaskReceipt(t, prefix, "chairlift", "projectbluefin/tap", "26.11.7")

	packages, err := ListInstalledCasks()
	if err != nil {
		t.Fatalf("ListInstalledCasks() error = %v", err)
	}
	want := []Package{{
		Name: "chairlift", FullName: "projectbluefin/tap/chairlift", Tap: "projectbluefin/tap", Version: "26.11.7",
	}}
	if !reflect.DeepEqual(packages, want) {
		t.Fatalf("ListInstalledCasks() = %+v, want %+v", packages, want)
	}
}

// A Homebrew whose installed-cask query resolves by token refuses the whole
// listing once two taps share a token. The receipts still describe the
// installed inventory, so the read degrades to them instead of failing.
func TestListInstalledCasksFallsBackToReceiptsOnAmbiguity(t *testing.T) {
	prefix, _, argvLog := caskFixture(t, `info) `+ambiguityStderr+` ;;`)
	writeCaskReceipt(t, prefix, "chairlift", "projectbluefin/tap", "26.11.7")

	packages, err := ListInstalledCasks()
	if err != nil {
		t.Fatalf("ListInstalledCasks() error = %v", err)
	}
	want := []Package{{
		Name: "chairlift", FullName: "projectbluefin/tap/chairlift", Tap: "projectbluefin/tap", Version: "26.11.7",
	}}
	if !reflect.DeepEqual(packages, want) {
		t.Fatalf("ListInstalledCasks() = %+v, want %+v", packages, want)
	}
	assertArgv(t, argvLog, []string{"info --installed --json=v2 --cask", "--prefix"})
}

// `brew uninstall --cask <bare token>` refuses or picks the wrong tap once
// two taps define the token. The command must name the tap the receipt does.
func TestUninstallQualifiesTheInstalledCask(t *testing.T) {
	prefix, _, argvLog := caskFixture(t, `uninstall) exit 0 ;;`)
	writeCaskReceipt(t, prefix, "chairlift", "ublue-os/tap", "0.1.0")

	if err := Uninstall("chairlift", true); err != nil {
		t.Fatalf("Uninstall(chairlift) error = %v", err)
	}
	assertArgv(t, argvLog, []string{"--prefix", "uninstall --cask ublue-os/tap/chairlift"})
}

func TestUninstallLeavesFormulaNamesAlone(t *testing.T) {
	_, _, argvLog := caskFixture(t, `uninstall) exit 0 ;;`)
	if err := Uninstall("gh", false); err != nil {
		t.Fatalf("Uninstall(gh) error = %v", err)
	}
	assertArgv(t, argvLog, []string{"uninstall gh"})
}

// An official homebrew/cask receipt keeps the bare token as its qualified
// name; only a third-party tap's token gains an owner/repo prefix.
func TestOfficialCaskReceiptsKeepTheBareName(t *testing.T) {
	prefix, _, _ := caskFixture(t, "")
	writeCaskReceipt(t, prefix, "firefox", "homebrew/cask", "142.0")

	packages := applyReceiptTaps([]Package{{Name: "firefox", FullName: "firefox"}})
	if packages[0].Tap != "homebrew/cask" || packages[0].FullName != "firefox" {
		t.Fatalf("official cask = %+v, want the bare token qualified by homebrew/cask", packages[0])
	}
}

func TestTapDefinesCaskReadsTheTapDirectory(t *testing.T) {
	_, repo, _ := caskFixture(t, "")
	tapDir := filepath.Join(repo, "Library", "Taps", "ublue-os", "homebrew-tap", "Casks")
	writeFile(t, filepath.Join(tapDir, "bluefin-wallpapers-extra.rb"), "cask\n")

	served, err := TapDefinesCask("ublue-os/tap", "bluefin-wallpapers-extra")
	if err != nil || !served {
		t.Fatalf("TapDefinesCask(installed cask) = %v, %v; want true, nil", served, err)
	}
	served, err = TapDefinesCask("projectbluefin/tap", "bluefin-wallpapers-extra")
	if err != nil || served {
		t.Fatalf("TapDefinesCask(moved cask) = %v, %v; want false, nil", served, err)
	}
	if _, err := TapDefinesCask("broken", "x"); err == nil {
		t.Fatal("TapDefinesCask accepted a tap name without an owner")
	}
}

// A Brewfile names a cask by bare token. Once the token is defined by two
// taps, Homebrew refuses to resolve it unless the token is qualified; the
// installed receipt supplies that qualification.
func TestBundleDescriptionsResolveAmbiguousCaskToken(t *testing.T) {
	prefix, _, argvLog := caskFixture(t, `info)
 if [ "$4" = "chairlift" ]; then `+ambiguityStderr+`
 else printf '%s' '{"casks":[{"token":"chairlift","full_token":"projectbluefin/tap/chairlift","desc":"System management tool"}]}'
 fi ;;`)
	writeCaskReceipt(t, prefix, "chairlift", "projectbluefin/tap", "26.11.7")

	got, err := DescribeBundleItems(context.Background(), []BundleItem{{Name: "chairlift", Kind: BundleItemCask}})
	if err != nil {
		t.Fatalf("DescribeBundleItems() error = %v", err)
	}
	if got[0].Description != "System management tool" {
		t.Fatalf("description = %q, want the receipt-qualified cask's description", got[0].Description)
	}
	assertArgv(t, argvLog, []string{"info --json=v2 --cask chairlift", "--prefix", "info --json=v2 --cask projectbluefin/tap/chairlift"})
}

// When no receipt names the ambiguous token, nothing on this host can choose
// a tap. The error Homebrew printed names both, which is the actionable
// answer; inventing a tap would be the silent wrong answer.
func TestBundleDescriptionsReportAmbiguousUninstalledCask(t *testing.T) {
	caskFixture(t, `info) `+ambiguityStderr+` ;;`)

	_, err := DescribeBundleItems(context.Background(), []BundleItem{{Name: "bluefin-wallpapers-extra", Kind: BundleItemCask}})
	if err == nil || !strings.Contains(err.Error(), "exists in multiple taps") {
		t.Fatalf("DescribeBundleItems() error = %v, want the ambiguous-token message", err)
	}
	if !isAmbiguousCaskError(errors.New("Brew command failed: Error: Cask x exists in multiple taps:")) {
		t.Fatal("isAmbiguousCaskError did not recognize Homebrew's refusal")
	}
}
