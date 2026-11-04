package hardware

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func hardwareBrewFixture(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dmi := filepath.Join(root, "sys/class/dmi/id")
	if err := os.MkdirAll(dmi, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmi, "chassis_vendor"), []byte("Framework\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "brew"), []byte("#!/bin/sh\n"+body), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOMEBREW_PREFIX", root)
	return root
}

func TestEnableHardwareRejectsUnverifiedInstall(t *testing.T) {
	root := hardwareBrewFixture(t, `case "$1" in
--version|tap|install) exit 0 ;;
info) echo '{"casks":[]}' ;;
*) exit 7 ;;
esac
`)
	status, err := EnableHardware(context.Background(), root)
	if err == nil || status.Installed {
		t.Fatalf("unverified install: installed=%v, error=%v; want inactive and verification error", status.Installed, err)
	}
}

func TestEnableHardwareDryRunDoesNotClaimInstalled(t *testing.T) {
	root := hardwareBrewFixture(t, `case "$1" in
--version) exit 0 ;;
info) echo '{"casks":[]}' ;;
*) echo 'unexpected mutation' >&2; exit 7 ;;
esac
`)
	before := dryrun.Enabled()
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(before) })
	status, err := EnableHardware(context.Background(), root)
	if err != nil || status.Installed || len(status.Missing) != 2 {
		t.Fatalf("preview: status=%+v, error=%v; want two still-missing packages", status, err)
	}
}

func TestEnableHardwareReportsInventoryFailure(t *testing.T) {
	root := hardwareBrewFixture(t, `case "$1" in
--version) exit 0 ;;
*) echo 'inventory unavailable' >&2; exit 7 ;;
esac
`)
	status, err := EnableHardware(context.Background(), root)
	if err == nil || status.Installed {
		t.Fatalf("failed inventory: status=%+v, error=%v", status, err)
	}
}
