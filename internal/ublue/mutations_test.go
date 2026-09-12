package ublue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

// journalledArgv runs call under dry-run with journalling enabled and
// returns the argv ChairLift assembled for the pkexec boundary.
//
// Dry-run is the right seam for the exported mutation functions: unlike
// runHelper they bind pkexecCommand ("pkexec") themselves, so there is no
// path parameter to point at a stand-in. Under dry-run nothing is spawned at
// all, yet the journal still records the exact argv a live run would have
// executed — which is the contract these functions own. Every one of them is
// irreversible on a real host (FactoryReset destroys local state, Restart
// ends the session), so the test must never reach a real pkexec.
func journalledArgv(t *testing.T, call func(context.Context) error) journal.Entry {
	t.Helper()

	path := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, path)
	journal.Reset()
	t.Cleanup(journal.Reset)

	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := call(context.Background()); err != nil {
		t.Fatalf("call error = %v, want nil under dry-run", err)
	}

	entries := readJournal(t, path)
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want exactly 1", len(entries))
	}
	return entries[0]
}

// Each exported action must send exactly one fixed command word, and the
// helper must derive everything else itself. Sending the wrong word here is
// not a cosmetic bug: dx-enable instead of dx-disable grants a privilege the
// user asked to drop, and factory-reset is unrecoverable.
func TestExportedActionsSendTheirOwnCommandWord(t *testing.T) {
	tests := []struct {
		name     string
		call     func(context.Context) error
		wantArgs []string
	}{
		{
			name:     "SwitchChannel stable",
			call:     func(ctx context.Context) error { return SwitchChannel(ctx, imageinfo.ChannelStable) },
			wantArgs: []string{ubluehelper.CommandChannelSwitch, "stable"},
		},
		{
			name:     "SwitchChannel testing",
			call:     func(ctx context.Context) error { return SwitchChannel(ctx, imageinfo.ChannelTesting) },
			wantArgs: []string{ubluehelper.CommandChannelSwitch, "testing"},
		},
		{
			name:     "SetDeveloperMode enabled",
			call:     func(ctx context.Context) error { return SetDeveloperMode(ctx, true) },
			wantArgs: []string{ubluehelper.CommandDXEnable},
		},
		{
			name:     "SetDeveloperMode disabled",
			call:     func(ctx context.Context) error { return SetDeveloperMode(ctx, false) },
			wantArgs: []string{ubluehelper.CommandDXDisable},
		},
		{
			name:     "Restart",
			call:     Restart,
			wantArgs: []string{ubluehelper.CommandRestart},
		},
		{
			name:     "Rollback",
			call:     Rollback,
			wantArgs: []string{ubluehelper.CommandRollback},
		},
		{
			name:     "FactoryReset",
			call:     FactoryReset,
			wantArgs: []string{ubluehelper.CommandFactoryReset},
		},
		{
			name:     "SetAutomaticUpdates enabled",
			call:     func(ctx context.Context) error { return SetAutomaticUpdates(ctx, true) },
			wantArgs: []string{ubluehelper.CommandAutoEnable},
		},
		{
			name:     "SetAutomaticUpdates disabled",
			call:     func(ctx context.Context) error { return SetAutomaticUpdates(ctx, false) },
			wantArgs: []string{ubluehelper.CommandAutoDisable},
		},
		{
			name:     "SwitchDriver standard",
			call:     func(ctx context.Context) error { return SwitchDriver(ctx, imageinfo.DriverStandard) },
			wantArgs: []string{ubluehelper.CommandDriverSwitch, string(imageinfo.DriverStandard)},
		},
		{
			name:     "SwitchDriver nvidia",
			call:     func(ctx context.Context) error { return SwitchDriver(ctx, imageinfo.DriverNVIDIA) },
			wantArgs: []string{ubluehelper.CommandDriverSwitch, string(imageinfo.DriverNVIDIA)},
		},
		{
			name:     "SwitchDriver nvidia-open",
			call:     func(ctx context.Context) error { return SwitchDriver(ctx, imageinfo.DriverNVIDIAOpen) },
			wantArgs: []string{ubluehelper.CommandDriverSwitch, string(imageinfo.DriverNVIDIAOpen)},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry := journalledArgv(t, test.call)

			want := append([]string{pkexecCommand, HelperPath}, test.wantArgs...)
			want = append(want, "--dry-run")
			if !reflect.DeepEqual(entry.WouldRun, want) {
				t.Fatalf("assembled argv = %v, want %v", entry.WouldRun, want)
			}
			if entry.Action != test.wantArgs[0] {
				t.Errorf("journalled action = %q, want %q", entry.Action, test.wantArgs[0])
			}
			if entry.Suppressed != journal.SuppressedDryRun {
				t.Errorf("suppressed = %q, want %q", entry.Suppressed, journal.SuppressedDryRun)
			}
			for _, arg := range test.wantArgs {
				if filepath.IsAbs(arg) {
					t.Errorf("argument %q crosses the pkexec boundary as a path; the helper must derive paths itself", arg)
				}
			}
		})
	}
}

// Every command word the exported actions can emit must be one the
// privileged helper actually accepts. A word that is not in
// SupportedCommands is rejected at the far side of pkexec, so the user's
// action silently fails.
func TestEmittedCommandsAreAllHelperSupported(t *testing.T) {
	supported := make(map[string]bool)
	for _, command := range ubluehelper.SupportedCommands() {
		supported[command] = true
	}

	emitted := []string{
		ubluehelper.CommandChannelSwitch,
		ubluehelper.CommandDXEnable,
		ubluehelper.CommandDXDisable,
		ubluehelper.CommandRestart,
		ubluehelper.CommandRollback,
		ubluehelper.CommandFactoryReset,
		ubluehelper.CommandAutoEnable,
		ubluehelper.CommandAutoDisable,
		ubluehelper.CommandDriverSwitch,
	}
	for _, command := range emitted {
		if !supported[command] {
			t.Errorf("ublue emits %q but the helper does not list it in SupportedCommands()", command)
		}
	}
}

// A channel word that is not stable or testing must be refused before
// anything reaches pkexec — no journal entry, no process.
func TestSwitchChannelRejectionNeverReachesTheHelper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, path)
	journal.Reset()
	t.Cleanup(journal.Reset)

	err := SwitchChannel(context.Background(), imageinfo.Channel("gts"))
	if err == nil {
		t.Fatal("SwitchChannel(\"gts\") error = nil, want a rejection")
	}
	var ublueErr *Error
	if !errors.As(err, &ublueErr) {
		t.Fatalf("SwitchChannel error type = %T, want *ublue.Error", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("rejected channel produced a journal entry; it must be refused before the helper is invoked")
	}
}

func TestSwitchDriverRejectionNeverReachesTheHelper(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, path)
	journal.Reset()
	t.Cleanup(journal.Reset)

	err := SwitchDriver(context.Background(), imageinfo.Driver("nouveau"))
	if err == nil {
		t.Fatal("SwitchDriver(\"nouveau\") error = nil, want a rejection")
	}
	var ublueErr *Error
	if !errors.As(err, &ublueErr) {
		t.Fatalf("SwitchDriver error type = %T, want *ublue.Error", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("rejected driver produced a journal entry; it must be refused before the helper is invoked")
	}
}

// DefaultContext bounds every helper invocation. An unbounded context would
// let a wedged bootc transaction hang the UI forever.
func TestDefaultContextCarriesTheDefaultDeadline(t *testing.T) {
	ctx, cancel := DefaultContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("DefaultContext returned a context with no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > DefaultTimeout {
		t.Errorf("DefaultContext deadline in %v, want (0, %v]", remaining, DefaultTimeout)
	}

	cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("after cancel, ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
}

// SetDescriptorOverride must redirect only the unprivileged descriptor read.
// It is dry-run-only by contract, so the test also pins that it changes what
// Detect reports without touching HelperPath or the command words.
func TestSetDescriptorOverrideRedirectsDetectOnly(t *testing.T) {
	t.Cleanup(func() { descriptorOverride = "" })

	path := filepath.Join(t.TempDir(), "image-info.json")
	descriptor := `{"image-name":"bluefin","image-tag":"stable","image-ref":"ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:stable","image-vendor":"ublue-os"}`
	if err := os.WriteFile(path, []byte(descriptor), 0o644); err != nil {
		t.Fatalf("writing descriptor: %v", err)
	}

	SetDescriptorOverride(path)
	if descriptorOverride != path {
		t.Fatalf("descriptorOverride = %q, want %q", descriptorOverride, path)
	}

	status, err := Detect()
	if err != nil {
		t.Fatalf("Detect with override error = %v, want nil", err)
	}
	if !status.Available {
		t.Fatal("Detect with a valid overridden descriptor reported unavailable")
	}

	// The override is read-only: the privileged surface is untouched by it.
	if HelperPath != "/usr/bin/chairlift-ublue-helper" {
		t.Errorf("HelperPath = %q; the descriptor override must never influence the privileged path", HelperPath)
	}
}

// A missing override path must fall back to "unavailable", not to the real
// host descriptor — otherwise a broken walkthrough silently reports the
// developer's own machine.
func TestDescriptorOverridePointingAtNothingIsUnavailable(t *testing.T) {
	t.Cleanup(func() { descriptorOverride = "" })

	SetDescriptorOverride(filepath.Join(t.TempDir(), "absent.json"))

	status, err := Detect()
	if err != nil {
		t.Fatalf("Detect error = %v, want nil for an absent descriptor", err)
	}
	if status.Available {
		t.Error("Detect reported available for an absent overridden descriptor")
	}
}

// osUserGroups is the production value of the lookupGroups seam. The rest of
// the suite substitutes that seam, so the real implementation itself is
// never executed; this pins that it resolves the invoking user's groups
// without error on an ordinary account.
func TestOSUserGroupsResolvesInvokingUser(t *testing.T) {
	groups, err := osUserGroups()
	if err != nil {
		t.Skipf("user lookup unavailable in this environment: %v", err)
	}
	for _, group := range groups {
		if group == "" {
			t.Error("osUserGroups returned an empty group name")
		}
	}
}

// StatusCached must run detection at most once and keep returning the same
// answer: the image descriptor cannot change without a reboot, and the views
// layer calls it on every page build.
func TestStatusCachedDetectsOnceAndRepeats(t *testing.T) {
	calls := 0
	original := detectInfo
	detectInfo = func() (imageinfo.Info, error) {
		calls++
		return original()
	}
	t.Cleanup(func() { detectInfo = original })

	first := StatusCached()
	second := StatusCached()

	if !reflect.DeepEqual(first, second) {
		t.Errorf("StatusCached returned %+v then %+v; the cached status must be stable", first, second)
	}
	if calls > 1 {
		t.Errorf("StatusCached ran detection %d times, want at most 1", calls)
	}
}
