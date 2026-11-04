package ublue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

func TestSetGamingModeJournalsOnlyTheRequestedModeWord(t *testing.T) {
	for _, test := range []struct {
		enabled bool
		word    string
	}{
		{true, "enable"},
		{false, "disable"},
	} {
		t.Run(test.word, func(t *testing.T) {
			entry := journalledArgv(t, func(ctx context.Context) error {
				return SetGamingMode(ctx, test.enabled)
			})
			want := []string{pkexec.Command, HelperPath, "gaming-switch", test.word, "--dry-run"}
			if !reflect.DeepEqual(entry.WouldRun, want) {
				t.Fatalf("gaming switch argv = %v; want %v", entry.WouldRun, want)
			}
			if entry.Action != "gaming-switch" || entry.Args["args"] != test.word || entry.Suppressed != journal.SuppressedDryRun {
				t.Errorf("gaming switch journal = %+v; want suppressed gaming-switch %s", entry, test.word)
			}
		})
	}
}

func TestSetGamingModePreservesHelperFailureAndOutcome(t *testing.T) {
	previousCommand := pkexecCommand
	t.Cleanup(func() { pkexecCommand = previousCommand })
	previousDryRun := dryrun.Enabled()
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(previousDryRun) })

	dir := t.TempDir()
	pkexecCommand = filepath.Join(dir, "fake-pkexec")
	if err := os.WriteFile(pkexecCommand, []byte("#!/bin/sh\necho 'gaming target refused' >&2\nexit 17\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.jsonl")
	t.Setenv(journal.PathEnv, path)
	journal.Reset()
	t.Cleanup(journal.Reset)

	err := SetGamingMode(context.Background(), true)
	var helperErr *Error
	if !errors.As(err, &helperErr) || !strings.Contains(helperErr.Message, "exit 17") || !strings.Contains(helperErr.Message, "gaming target refused") {
		t.Fatalf("SetGamingMode error = %v; want the preserved helper refusal", err)
	}
	entries := readJournal(t, path)
	if len(entries) != 2 || entries[1].Action != "gaming-switch" || entries[1].Outcome != journal.OutcomeFailed {
		t.Fatalf("journal = %+v; want invocation and failed gaming-switch outcome", entries)
	}
}

func TestDetectGamingSwitchabilityUsesOppositeBootedState(t *testing.T) {
	for _, test := range []struct {
		name      string
		info      imageinfo.Info
		gaming    bool
		canSwitch bool
	}{
		{"standard stable", imageinfo.Info{Name: "dakota", Ref: "docker://ghcr.io/projectbluefin/dakota:stable", Tag: "stable"}, false, true},
		{"gaming testing", imageinfo.Info{Name: "dakota-gaming", Ref: "docker://ghcr.io/projectbluefin/dakota-gaming:testing", Tag: "testing", Flavor: "gaming"}, true, true},
		{"nvidia next", imageinfo.Info{Name: "dakota-nvidia", Ref: "docker://ghcr.io/projectbluefin/dakota-nvidia:next", Tag: "next"}, false, true},
		{"gaming nvidia btw", imageinfo.Info{Name: "dakota-nvidia-gaming", Ref: "docker://ghcr.io/projectbluefin/dakota-nvidia-gaming:btw", Tag: "btw", Flavor: "gaming"}, true, true},
		{"deprecated latest", imageinfo.Info{Name: "dakota", Ref: "docker://ghcr.io/projectbluefin/dakota:latest", Tag: "latest"}, false, false},
		{"pinned gaming", imageinfo.Info{Name: "dakota-gaming", Ref: "docker://ghcr.io/projectbluefin/dakota-gaming:stable-20261101", Tag: "stable-20261101", Flavor: "gaming"}, true, false},
		{"unpublished open driver", imageinfo.Info{Name: "dakota-nvidia-open", Ref: "docker://ghcr.io/projectbluefin/dakota-nvidia-open:stable", Tag: "stable"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stubDetection(t, test.info, nil, nil)
			got, err := Detect()
			if err != nil {
				t.Fatal(err)
			}
			if got.Gaming != test.gaming || got.CanSwitchGaming != test.canSwitch {
				t.Errorf("Detect() Gaming=%t CanSwitchGaming=%t; want %t, %t", got.Gaming, got.CanSwitchGaming, test.gaming, test.canSwitch)
			}
			if got.Supports(ubluehelper.CommandGamingSwitch) {
				t.Error("descriptor support incorrectly claims an absent installed helper supports gaming-switch")
			}
		})
	}
}

func TestDetectGamingSwitchFailsClosedOnMalformedAuthoritativeTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channels.yml")
	if err := os.WriteFile(path, []byte("images:\n  ghcr.io/example/image:\n    stable_tags: [stable]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(imageinfo.ResetTable)
	if _, err := imageinfo.LoadTable([]string{path}); err == nil {
		t.Fatal("malformed table was accepted")
	}
	stubDetection(t, imageinfo.Info{Name: "dakota", Ref: "docker://ghcr.io/projectbluefin/dakota:stable", Tag: "stable"}, nil, nil)
	got, err := Detect()
	if err != nil {
		t.Fatal(err)
	}
	if got.ChannelTableError == "" || got.CanSwitchGaming {
		t.Errorf("Detect() error=%q CanSwitchGaming=%t; want table error and no gaming switch", got.ChannelTableError, got.CanSwitchGaming)
	}
}

func TestGamingHelperSupportRequiresInstalledGamingPolicyAction(t *testing.T) {
	for _, test := range []struct {
		command string
		want    bool
	}{
		{"driver-switch", false},
		{"gaming-switch", true},
	} {
		t.Run(test.command, func(t *testing.T) {
			dir := t.TempDir()
			writePolicy(t, dir, "installed.policy", `<policyconfig><action id="installed.switch">
<annotate key="org.freedesktop.policykit.exec.path">/usr/bin/chairlift-helper</annotate>
<annotate key="org.freedesktop.policykit.exec.argv1">`+test.command+`</annotate>
</action></policyconfig>`)
			stubHost(t, dir, true)
			got := (Status{Commands: authorizedCommands()}).Supports(ubluehelper.CommandGamingSwitch)
			if got != test.want {
				t.Errorf("Supports(gaming-switch) = %t with installed %s action; want %t", got, test.command, test.want)
			}
		})
	}
}

func TestSetGamingModeCallsOnlyTheFixedInstalledHelper(t *testing.T) {
	previousCommand := pkexecCommand
	t.Cleanup(func() { pkexecCommand = previousCommand })
	previousDryRun := dryrun.Enabled()
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(previousDryRun) })

	for _, test := range []struct {
		enabled bool
		word    string
	}{
		{true, "enable"},
		{false, "disable"},
	} {
		t.Run(test.word, func(t *testing.T) {
			dir := t.TempDir()
			captured := filepath.Join(dir, "argv")
			pkexecCommand = writeFakePkexec(t, captured)
			path := filepath.Join(dir, "journal.jsonl")
			t.Setenv(journal.PathEnv, path)
			journal.Reset()
			t.Cleanup(journal.Reset)

			if err := SetGamingMode(context.Background(), test.enabled); err != nil {
				t.Fatal(err)
			}
			got := readCapturedArgs(t, captured)
			want := []string{"/usr/bin/chairlift-helper", "gaming-switch", test.word}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("helper argv = %v; want %v", got, want)
			}
			entries := readJournal(t, path)
			if len(entries) != 2 || entries[1].Action != "gaming-switch" || entries[1].Outcome != journal.OutcomeSucceeded {
				t.Fatalf("journal = %+v; want exactly one gaming switch and its success, without a restart", entries)
			}
		})
	}
}
