package sysupdate

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// The exported entry points in this file bind fixed, absolute system paths
// (UpdateCheckPath, StagedSemaphorePath, osReleasePath) that are consts, so
// they cannot be redirected at a seam the way readUpdateCheckFrom and
// runLsblk can. They are still the functions the UI actually calls, so the
// contract worth pinning is the one that must hold on a host where those
// paths are absent or carry no snosi identity: ChairLift degrades to the
// idle, no-rollback state instead of erroring or inventing a target.

// requireAbsent skips when a fixed path exists, so the assertions below
// describe a host without snosi state rather than silently inverting on a
// real Bluefin box where /run/snosi is populated.
func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Skipf("%s exists on this host; this test describes a host without snosi state", path)
	}
}

func TestReadUpdateCheckAbsentFileIsNotAnError(t *testing.T) {
	requireAbsent(t, UpdateCheckPath)

	check, err := ReadUpdateCheck()
	if err != nil {
		t.Fatalf("ReadUpdateCheck() error = %v, want nil: an absent check file is the normal no-check-yet state", err)
	}
	if check != nil {
		t.Errorf("ReadUpdateCheck() = %+v, want nil", check)
	}
}

func TestReadStagedUpdateAbsentSemaphoreIsNotAnError(t *testing.T) {
	requireAbsent(t, StagedSemaphorePath)

	staged, err := ReadStagedUpdate()
	if err != nil {
		t.Fatalf("ReadStagedUpdate() error = %v, want nil: an absent semaphore is the normal nothing-staged state", err)
	}
	if staged != nil {
		t.Errorf("ReadStagedUpdate() = %+v, want nil", staged)
	}
}

// GetStatus is the single call the update UI makes. With both state files
// absent it must reduce to the idle prompt: nothing staged, and the
// Presentation triple empty so the subtitle formatter renders the idle row.
func TestGetStatusWithNoStateFilesIsIdle(t *testing.T) {
	requireAbsent(t, UpdateCheckPath)
	requireAbsent(t, StagedSemaphorePath)

	status := GetStatus()
	if status.Check != nil {
		t.Errorf("GetStatus().Check = %+v, want nil", status.Check)
	}
	if status.Staged != nil {
		t.Errorf("GetStatus().Staged = %+v, want nil", status.Staged)
	}
	if status.IsStaged() {
		t.Error("GetStatus().IsStaged() = true, want false: no semaphore and no check record means no reboot is pending")
	}
	outcome, version, checkedAt := status.Presentation()
	if outcome != "" || version != "" || checkedAt != "" {
		t.Errorf("GetStatus().Presentation() = (%q, %q, %q), want three empty strings (the idle prompt)", outcome, version, checkedAt)
	}
}

// RollbackVersion walks os-release -> lsblk -> slot comparison. The lsblk
// leg is substitutable through $PATH, so the refusal paths are reachable
// without a snosi partition layout. The lsblk-failure leg already has
// coverage in readers_test.go; what is missing is the case where lsblk
// succeeds and the labels simply do not describe a rollback.
func TestRollbackVersionRefusesWhenNoSlotIsLabelled(t *testing.T) {
	fakeLsblk(t, `{"blockdevices":[{"partlabel":"","children":[{"partlabel":"EFI-SYSTEM"}]}]}`, 0)

	version, ok := RollbackVersion(context.Background())
	if ok || version != "" {
		t.Errorf("RollbackVersion() with no snosi slot labels = (%q, %v), want (\"\", false)", version, ok)
	}
}

// Fail-closed identity check: a host whose os-release carries no IMAGE_ID is
// not a snosi A/B host, so no PARTLABEL may be believed — even one shaped
// exactly like a rollback slot. Without this the empty imageID would make
// the "<imageID>_" prefix filter match every label.
//
// On a host that does carry an IMAGE_ID the same call must instead find the
// older sibling slot, so the assertion follows the host's own identity.
func TestRollbackVersionFollowsOsReleaseIdentity(t *testing.T) {
	osRelease, err := os.ReadFile(osReleasePath)
	if err != nil {
		t.Skipf("reading %s: %v", osReleasePath, err)
	}
	imageID, runningVersion := imageIdentity(osRelease)

	if imageID == "" {
		fakeLsblk(t, `{"blockdevices":[{"partlabel":"snow-ab_20260101000000_r"}]}`, 0)

		version, ok := RollbackVersion(context.Background())
		if ok || version != "" {
			t.Errorf("RollbackVersion() on a host with no IMAGE_ID = (%q, %v), want (\"\", false): an unidentified image must not adopt a foreign slot label", version, ok)
		}
		return
	}

	if !ValidVersion(runningVersion) {
		t.Skipf("host IMAGE_VERSION %q is not the 14-digit snosi grammar", runningVersion)
	}
	older := "20000101000000"
	body := fmt.Sprintf(
		`{"blockdevices":[{"partlabel":"","children":[{"partlabel":"%s_%s_r"},{"partlabel":"%s_%s_r"}]}]}`,
		imageID, runningVersion, imageID, older,
	)
	fakeLsblk(t, body, 0)

	version, ok := RollbackVersion(context.Background())
	if !ok || version != older {
		t.Errorf("RollbackVersion() = (%q, %v), want (%q, true): the inactive slot holds an older version of the running image", version, ok, older)
	}
}
