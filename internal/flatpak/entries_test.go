package flatpak

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// The origin listing is the query `brew bundle` itself uses, and it names
// only the application id and the remote it came from.
func TestParseApplicationOriginsReadsThePairs(t *testing.T) {
	got := parseApplicationOrigins("app.drey.Damask\tflathub\ncom.example.Other\tmixtapes\n")
	want := map[string]string{
		"app.drey.Damask":   "flathub",
		"com.example.Other": "mixtapes",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseApplicationOrigins = %v, want %v", got, want)
	}
}

// A blank line and a row without an application id are not applications.
func TestParseApplicationOriginsSkipsEmptyRows(t *testing.T) {
	got := parseApplicationOrigins("\n\tflathub\napp.drey.Damask\tflathub\n")
	if len(got) != 1 {
		t.Fatalf("parseApplicationOrigins = %v, want one application", got)
	}
	if got["app.drey.Damask"] != "flathub" {
		t.Errorf("parseApplicationOrigins = %v, want the Flathub application", got)
	}
}

// One Flatpak transaction installs a whole partially selected collection:
// the scope is the system installation and the remote is Flathub, because
// Bluefin's images configure Flathub only as a system remote.
func TestBatchedFlatpakInstallUsesOneSystemFlathubTransaction(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	body := `case "$1" in
list) printf 'app.drey.Damask\tflathub\n' ;;
esac`
	capture := installCapturingFlatpak(t, body)

	if err := InstallMany([]string{"app.drey.Damask", "app.drey.EarTag"}); err != nil {
		t.Fatalf("InstallMany returned error: %v", err)
	}
	want := []string{"install", "-y", "--system", "flathub", "app.drey.Damask", "app.drey.EarTag"}
	if got := capturedFlatpakArgs(t, capture); !reflect.DeepEqual(got, want) {
		t.Fatalf("flatpak arguments = %v, want %v", got, want)
	}
}

// An empty list must never reach flatpak: an install with no ref is an
// error, and the caller has nothing to install in that case.
func TestBatchedFlatpakInstallWithNoIDsRunsNoCommand(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	capture := installCapturingFlatpak(t, "exit 0")
	if err := InstallMany(nil); err != nil {
		t.Fatalf("InstallMany returned error: %v", err)
	}
	if _, err := os.Stat(capture); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("InstallMany(nil) ran flatpak: %s exists", capture)
	}
}

// The origin listing asks for the two columns it needs and nothing else.
func TestListInstalledOriginsUsesTheBundleQuery(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	body := `case "$1" in
list) printf 'app.drey.Damask\tflathub\n' ;;
esac`
	capture := installCapturingFlatpak(t, body)

	origins, err := ListInstalledOrigins()
	if err != nil {
		t.Fatalf("ListInstalledOrigins returned error: %v", err)
	}
	if origins["app.drey.Damask"] != "flathub" {
		t.Errorf("origins = %v, want the Flathub application", origins)
	}
	want := []string{"list", "--app", "--columns=application,origin"}
	if got := capturedFlatpakArgs(t, capture); !reflect.DeepEqual(got, want) {
		t.Fatalf("flatpak arguments = %v, want %v", got, want)
	}
}

// A listing that fails must be an error, not an empty inventory that reads
// as "nothing is installed".
func TestListInstalledOriginsFailsClosed(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	installCapturingFlatpak(t, "echo 'boom' >&2\nexit 1")
	origins, err := ListInstalledOrigins()
	if err == nil {
		t.Fatal("ListInstalledOrigins must fail when the listing fails")
	}
	if origins != nil {
		t.Errorf("origins = %v, want none alongside the error", origins)
	}
	var notFound *NotFoundError
	if errors.As(err, &notFound) {
		t.Error("a failed listing is not a missing executable")
	}
}
