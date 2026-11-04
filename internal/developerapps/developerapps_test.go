package developerapps

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func packageCommands(t *testing.T, brew, flatpak string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"brew": brew, "flatpak": flatpak} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("IDE_TEST_ROOT", dir)
	return dir
}

func TestExistingFlatpakInEitherScopeDoesNotRequireRemote(t *testing.T) {
	packageCommands(t, "exit 7", `case "$*" in
'--version') exit 0 ;;
'list --user --app --columns=name,application,version,branch') printf 'Editor\torg.example.Editor\t1\tstable\n' ;;
'list --system --app --columns=name,application,version,branch') exit 7 ;;
'remotes --system --columns=name,options') exit 7 ;;
*) exit 9 ;;
esac
`)
	app := App{ID: "editor", Name: "Editor", Source: Flatpak, Package: "org.example.Editor", Remote: "flathub"}
	status, err := Install(app)
	if err != nil || !status.Installed {
		t.Fatalf("existing user copy must be retained without reinstall: %+v, %v", status, err)
	}
}

func TestFailedInventoryCannotBecomeAnInstallableEmptyList(t *testing.T) {
	root := packageCommands(t, `case "$1" in
--version) exit 0 ;;
install) : > "$IDE_TEST_ROOT/mutated"; exit 0 ;;
*) echo 'inventory failure' >&2; exit 7 ;;
esac
`, "exit 7")
	app := App{ID: "editor", Name: "Editor", Source: Homebrew, Package: "ublue-os/tap/editor"}
	status, err := Install(app)
	if err == nil || status.Installed {
		t.Fatalf("failed inventory: status=%+v error=%v", status, err)
	}
	if _, err := os.Stat(filepath.Join(root, "mutated")); !os.IsNotExist(err) {
		t.Fatal("install ran despite unknown installed state")
	}
}

func TestSuccessfulCommandWithoutInstalledVersionIsNotSuccess(t *testing.T) {
	packageCommands(t, `case "$1" in
--version|tap|install) exit 0 ;;
tap-info) echo '[{"name":"ublue-os/tap","trusted":true}]' ;;
info) echo '{"casks":[]}' ;;
*) exit 9 ;;
esac
`, "exit 7")
	app := App{ID: "editor", Name: "Editor", Source: Homebrew, Package: "ublue-os/tap/editor"}
	status, err := Install(app)
	if err == nil || status.Installed {
		t.Fatalf("unverified install: status=%+v error=%v", status, err)
	}
}

func TestDryRunDoesNotMutateOrInventInstalledState(t *testing.T) {
	root := packageCommands(t, `case "$1" in
--version) exit 0 ;;
info) echo '{"casks":[]}' ;;
*) : > "$IDE_TEST_ROOT/mutated"; exit 9 ;;
esac
`, "exit 7")
	before := dryrun.Enabled()
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(before) })
	app := App{ID: "editor", Name: "Editor", Source: Homebrew, Package: "ublue-os/tap/editor"}
	status, err := Install(app)
	if err != nil || status.Installed {
		t.Fatalf("preview: status=%+v error=%v", status, err)
	}
	if _, err := os.Stat(filepath.Join(root, "mutated")); !os.IsNotExist(err) {
		t.Fatal("preview mutated the package manager")
	}
}

func TestUnapprovedTapCannotBeInstalled(t *testing.T) {
	root := packageCommands(t, `case "$1" in
info) echo '{"casks":[]}' ;;
tap|trust|install) : > "$IDE_TEST_ROOT/mutated"; exit 0 ;;
tap-info) echo '[{"name":"example/tap","trusted":true}]' ;;
*) exit 7 ;;
esac
`, "exit 7")
	app := App{ID: "editor", Name: "Editor", Source: Homebrew, Package: "example/tap/editor"}
	if _, err := Install(app); err == nil {
		t.Fatal("an unapproved tap was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "mutated")); !os.IsNotExist(err) {
		t.Fatal("an unapproved tap was used for package installation")
	}
}

func TestUntrustedCaskIsTrustedWithoutTrustingWholeTap(t *testing.T) {
	packageCommands(t, `case "$1" in
info)
 if [ -f "$IDE_TEST_ROOT/installed" ]; then
  echo '{"casks":[{"token":"editor","installed":"1"}]}'
 else echo '{"casks":[]}'
 fi ;;
tap) exit 0 ;;
tap-info) echo '[{"name":"ublue-os/tap","trusted":false}]' ;;
trust)
 [ "$2" = '--cask' ] && [ "$3" = 'ublue-os/tap/editor' ] || exit 8
 : > "$IDE_TEST_ROOT/trusted" ;;
install)
 [ -f "$IDE_TEST_ROOT/trusted" ] || exit 9
 : > "$IDE_TEST_ROOT/installed" ;;
*) exit 7 ;;
esac
`, "exit 7")
	app := App{ID: "editor", Name: "Editor", Source: Homebrew, Package: "ublue-os/tap/editor"}
	status, err := Install(app)
	if err != nil || !status.Installed {
		t.Fatalf("untrusted cask could not be installed safely: status=%+v error=%v", status, err)
	}
}
