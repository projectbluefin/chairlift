package ublue

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/gpu"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

// writeFakePkexec writes an executable shell script standing in for pkexec:
// it records the argv it was handed to capturedArgsFile, then exits 0. It
// never execs the real pkexec or requires root.
func writeFakePkexec(t *testing.T, capturedArgsFile string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-pkexec")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + capturedArgsFile + "\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake pkexec: %v", err)
	}
	return path
}

func readCapturedArgs(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading captured pkexec argv: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// The whole point of the helper indirection is that ChairLift hands pkexec
// the fixed absolute helper path and a validated command word — never an
// image reference, never a username.
func TestRunHelperPassesFixedHelperPathAndCommandOnly(t *testing.T) {
	tests := []struct {
		name string
		call func(context.Context, string) error
		want []string
	}{
		{
			name: "switch to testing",
			call: func(ctx context.Context, pkexec string) error {
				_, _, err := runHelper(ctx, pkexec, ubluehelper.CommandChannelSwitch, string(imageinfo.ChannelTesting))
				return err
			},
			want: []string{HelperPath, "channel-switch", "testing"},
		},
		{
			name: "switch to stable",
			call: func(ctx context.Context, pkexec string) error {
				_, _, err := runHelper(ctx, pkexec, ubluehelper.CommandChannelSwitch, string(imageinfo.ChannelStable))
				return err
			},
			want: []string{HelperPath, "channel-switch", "stable"},
		},
		{
			name: "developer mode on",
			call: func(ctx context.Context, pkexec string) error {
				_, _, err := runHelper(ctx, pkexec, ubluehelper.CommandDXEnable)
				return err
			},
			want: []string{HelperPath, "dx-enable"},
		},
		{
			name: "developer mode off",
			call: func(ctx context.Context, pkexec string) error {
				_, _, err := runHelper(ctx, pkexec, ubluehelper.CommandDXDisable)
				return err
			},
			want: []string{HelperPath, "dx-disable"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			captured := filepath.Join(t.TempDir(), "captured-args")
			pkexec := writeFakePkexec(t, captured)

			if err := test.call(context.Background(), pkexec); err != nil {
				t.Fatalf("helper call error = %v, want nil", err)
			}

			got := readCapturedArgs(t, captured)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("pkexec argv = %v, want %v", got, test.want)
			}
			if got[0] != HelperPath {
				t.Errorf("helper path passed to pkexec = %q, want the fixed absolute path matching data/io.projectbluefin.chairlift.ublue.policy's exec.path annotation", got[0])
			}
			for _, arg := range got[1:] {
				if strings.Contains(arg, "/") {
					t.Errorf("argument %q crosses the pkexec boundary carrying a path or image reference; the helper must derive those itself", arg)
				}
			}
		})
	}
}

func TestDryRunNeverExecutesPkexec(t *testing.T) {
	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	nonexistentPkexec := filepath.Join(t.TempDir(), "pkexec-should-never-run")
	if _, _, err := runHelper(context.Background(), nonexistentPkexec, ubluehelper.CommandDXEnable); err != nil {
		t.Fatalf("dry-run runHelper error = %v, want nil", err)
	}
	if _, err := os.Stat(nonexistentPkexec); err == nil {
		t.Fatal("dry-run created the pkexec stand-in, which should never have been touched")
	}
}

func TestRunHelperClassifiesFailures(t *testing.T) {
	t.Run("missing pkexec", func(t *testing.T) {
		_, _, err := runHelper(context.Background(), "chairlift-pkexec-that-does-not-exist", ubluehelper.CommandDXEnable)
		var notFound *NotFoundError
		if err == nil || !asNotFound(err, &notFound) {
			t.Fatalf("runHelper error = %v, want *NotFoundError", err)
		}
	})

	t.Run("authorization denied", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "failing-pkexec")
		script := "#!/bin/sh\necho 'authorization denied' >&2\nexit 17\n"
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("writing failing pkexec: %v", err)
		}
		_, stderr, err := runHelper(context.Background(), path, ubluehelper.CommandDXEnable)
		if err == nil {
			t.Fatal("runHelper error = nil, want a non-nil error for exit 17")
		}
		if !strings.Contains(err.Error(), "exit 17") {
			t.Errorf("runHelper error = %q, want it to name exit 17", err)
		}
		if !strings.Contains(stderr, "authorization denied") {
			t.Errorf("stderr = %q, want the helper's own message preserved for the toast", stderr)
		}
	})
}

func asNotFound(err error, target **NotFoundError) bool {
	notFound, ok := err.(*NotFoundError)
	if ok {
		*target = notFound
	}
	return ok
}

func TestSwitchChannelRejectsUnswitchableChannels(t *testing.T) {
	for _, channel := range []imageinfo.Channel{imageinfo.ChannelUnknown, imageinfo.Channel(""), imageinfo.Channel("nightly")} {
		if err := SwitchChannel(context.Background(), channel); err == nil {
			t.Errorf("SwitchChannel(%q) error = nil, want a refusal before pkexec is reached", channel)
		}
	}
}

func TestDeveloperStateMatchesAnyDeveloperGroup(t *testing.T) {
	tests := []struct {
		name       string
		groups     []string
		wantActive bool
		wantGroups []string
	}{
		{
			name:       "full membership",
			groups:     []string{"james", "wheel", "docker", "incus-admin", "libvirt", "dialout"},
			wantActive: true,
			wantGroups: []string{"docker", "incus-admin", "libvirt", "dialout"},
		},
		{
			name:       "one group is enough",
			groups:     []string{"james", "wheel", "libvirt"},
			wantActive: true,
			wantGroups: []string{"libvirt"},
		},
		{
			name:       "reported in canonical order, not membership order",
			groups:     []string{"dialout", "docker"},
			wantActive: true,
			wantGroups: []string{"docker", "dialout"},
		},
		{name: "no developer groups", groups: []string{"james", "wheel", "audio"}},
		{name: "no groups at all", groups: nil},
		{name: "similar but different group", groups: []string{"dockerroot"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			active, matched := DeveloperState(test.groups)
			if active != test.wantActive {
				t.Fatalf("DeveloperState(%v) active = %v, want %v", test.groups, active, test.wantActive)
			}
			if !test.wantActive {
				if matched != nil {
					t.Errorf("DeveloperState(%v) groups = %v, want nil", test.groups, matched)
				}
				return
			}
			if !reflect.DeepEqual(matched, test.wantGroups) {
				t.Errorf("DeveloperState(%v) groups = %v, want %v", test.groups, matched, test.wantGroups)
			}
		})
	}
}

// stubDetection replaces both host-dependent reads for one test: the image
// descriptor and the invoking user's group membership.
func stubDetection(t *testing.T, info imageinfo.Info, infoErr error, groups []string) {
	t.Helper()
	stubDetectionWithGPU(t, info, infoErr, groups, gpu.Set{})
}

func stubDetectionWithGPU(t *testing.T, info imageinfo.Info, infoErr error, groups []string, hardware gpu.Set) {
	t.Helper()

	previousInfo, previousGroups, previousGPU := detectInfo, lookupGroups, detectGPU
	detectInfo = func() (imageinfo.Info, error) { return info, infoErr }
	lookupGroups = func() ([]string, error) { return groups, nil }
	detectGPU = func() gpu.Set { return hardware }
	t.Cleanup(func() {
		detectInfo, lookupGroups, detectGPU = previousInfo, previousGroups, previousGPU
	})
}

func TestDetectCoversEverySupportedVariant(t *testing.T) {
	tests := []struct {
		name   string
		info   imageinfo.Info
		groups []string
		want   Status
	}{
		{
			name:   "dakota stable, developer mode off",
			info:   imageinfo.Info{Name: "dakota", Tag: "latest", Ref: "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota"},
			groups: []string{"james", "wheel"},
			want: Status{
				Available:   true,
				Variant:     imageinfo.VariantDakota,
				Channel:     imageinfo.ChannelStable,
				Tag:         "latest",
				Ref:         "ghcr.io/projectbluefin/dakota",
				CanSwitchTo: imageinfo.ChannelTesting,
				Driver:      imageinfo.DriverStandard,
				GPU:         "No graphics hardware detected",
			},
		},
		{
			// Bluefin Stable publishes no testing tag, so the row is
			// correctly inert even though the host is fully recognized.
			name:   "bluefin stable, developer mode on, no channel to switch to",
			info:   imageinfo.Info{Name: "bluefin", Tag: "stable", Ref: "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin"},
			groups: []string{"james", "docker", "libvirt"},
			want: Status{
				Available:   true,
				Variant:     imageinfo.VariantBluefin,
				Channel:     imageinfo.ChannelStable,
				Tag:         "stable",
				Ref:         "ghcr.io/ublue-os/bluefin",
				Developer:   true,
				DevGroups:   []string{"docker", "libvirt"},
				CanSwitchTo: imageinfo.ChannelUnknown,
				Driver:      imageinfo.DriverStandard,
				GPU:         "No graphics hardware detected",
			},
		},
		{
			name:   "bluefin on the lts stream can switch",
			info:   imageinfo.Info{Name: "bluefin", Tag: "lts", Ref: "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin"},
			groups: nil,
			want: Status{
				Available:   true,
				Variant:     imageinfo.VariantBluefin,
				Channel:     imageinfo.ChannelStable,
				Tag:         "lts",
				Ref:         "ghcr.io/ublue-os/bluefin",
				CanSwitchTo: imageinfo.ChannelTesting,
				Driver:      imageinfo.DriverStandard,
				GPU:         "No graphics hardware detected",
			},
		},
		{
			name:   "bluefin lts stable",
			info:   imageinfo.Info{Name: "bluefin-lts", Tag: "lts", Ref: "ostree-image-signed:docker://ghcr.io/projectbluefin/bluefin-lts"},
			groups: nil,
			want: Status{
				Available:   true,
				Variant:     imageinfo.VariantBluefinLTS,
				Channel:     imageinfo.ChannelStable,
				Tag:         "lts",
				Ref:         "ghcr.io/projectbluefin/bluefin-lts",
				CanSwitchTo: imageinfo.ChannelTesting,
				Driver:      imageinfo.DriverStandard,
				GPU:         "No graphics hardware detected",
			},
		},
		{
			name:   "bluefin lts already on testing offers the way back",
			info:   imageinfo.Info{Name: "bluefin-lts", Tag: "testing", Ref: "docker://ghcr.io/projectbluefin/bluefin-lts"},
			groups: nil,
			want: Status{
				Available:   true,
				Variant:     imageinfo.VariantBluefinLTS,
				Channel:     imageinfo.ChannelTesting,
				Tag:         "testing",
				Ref:         "ghcr.io/projectbluefin/bluefin-lts",
				CanSwitchTo: imageinfo.ChannelStable,
				Driver:      imageinfo.DriverStandard,
				GPU:         "No graphics hardware detected",
			},
		},
		{
			name:   "pinned build offers no switch at all",
			info:   imageinfo.Info{Name: "dakota", Tag: "latest.20260212", Ref: "docker://ghcr.io/projectbluefin/dakota"},
			groups: nil,
			want: Status{
				Available:   true,
				Variant:     imageinfo.VariantDakota,
				Channel:     imageinfo.ChannelUnknown,
				Tag:         "latest.20260212",
				Ref:         "ghcr.io/projectbluefin/dakota",
				CanSwitchTo: imageinfo.ChannelUnknown,
				Driver:      imageinfo.DriverStandard,
				GPU:         "No graphics hardware detected",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stubDetection(t, test.info, nil, test.groups)

			got, err := Detect()
			if err != nil {
				t.Fatalf("Detect() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("Detect() = %+v, want %+v", got, test.want)
			}
		})
	}
}

// Snow Linux and every other non-ublue host has no descriptor. That is the
// normal case for ChairLift's original target, and must not surface as an
// error the user has to read.
func TestDetectTreatsAbsentDescriptorAsUnavailable(t *testing.T) {
	stubDetection(t, imageinfo.Info{}, os.ErrNotExist, nil)

	got, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v, want nil for a host with no image descriptor", err)
	}
	if got.Available {
		t.Errorf("Detect() available = true, want false")
	}
	if !reflect.DeepEqual(got, Status{}) {
		t.Errorf("Detect() = %+v, want the zero Status", got)
	}
}

func TestDetectReportsMalformedDescriptor(t *testing.T) {
	stubDetection(t, imageinfo.Info{}, os.ErrInvalid, nil)

	if _, err := Detect(); err == nil {
		t.Fatal("Detect() error = nil, want a non-nil error for an unreadable descriptor")
	}
}

// The driver recommendation must only appear where a switch is both possible
// and useful, so Detect is asserted across the hardware/image matrix rather
// than only on the machine the tests happen to run on.
func TestDetectRecommendsADriverOnlyWhenItHelps(t *testing.T) {
	tests := []struct {
		name            string
		info            imageinfo.Info
		hardware        gpu.Set
		wantDriver      imageinfo.Driver
		wantRecommended imageinfo.Driver
		wantGPU         string
	}{
		{
			name:            "nvidia card on the standard dakota image",
			info:            imageinfo.Info{Name: "dakota", Tag: "latest", Ref: "docker://ghcr.io/projectbluefin/dakota"},
			hardware:        gpu.Set{NVIDIA: true},
			wantDriver:      imageinfo.DriverStandard,
			wantRecommended: imageinfo.DriverNVIDIA,
			wantGPU:         "NVIDIA",
		},
		{
			name:            "hybrid laptop is recommended the nvidia image",
			info:            imageinfo.Info{Name: "bluefin", Tag: "latest", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			hardware:        gpu.Set{NVIDIA: true, Intel: true},
			wantDriver:      imageinfo.DriverStandard,
			wantRecommended: imageinfo.DriverNVIDIA,
			wantGPU:         "NVIDIA + Intel",
		},
		{
			name:       "amd machine is left alone",
			info:       imageinfo.Info{Name: "bluefin", Tag: "latest", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			hardware:   gpu.Set{AMD: true},
			wantDriver: imageinfo.DriverStandard,
			wantGPU:    "AMD",
		},
		{
			name:       "already on the nvidia image",
			info:       imageinfo.Info{Name: "bluefin", Tag: "latest", Ref: "docker://ghcr.io/ublue-os/bluefin-nvidia"},
			hardware:   gpu.Set{NVIDIA: true},
			wantDriver: imageinfo.DriverNVIDIA,
			wantGPU:    "NVIDIA",
		},
		{
			// No driver image is published for the LTS streams, so there is
			// nothing to recommend even with the hardware present.
			name:       "nvidia card on an lts host",
			info:       imageinfo.Info{Name: "bluefin", Tag: "lts", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			hardware:   gpu.Set{NVIDIA: true},
			wantDriver: imageinfo.DriverStandard,
			wantGPU:    "NVIDIA",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stubDetectionWithGPU(t, test.info, nil, nil, test.hardware)

			got, err := Detect()
			if err != nil {
				t.Fatalf("Detect() error = %v, want nil", err)
			}
			if got.Driver != test.wantDriver {
				t.Errorf("Driver = %q, want %q", got.Driver, test.wantDriver)
			}
			if got.RecommendedDriver != test.wantRecommended {
				t.Errorf("RecommendedDriver = %q, want %q", got.RecommendedDriver, test.wantRecommended)
			}
			if got.GPU != test.wantGPU {
				t.Errorf("GPU = %q, want %q", got.GPU, test.wantGPU)
			}
		})
	}
}

func TestSwitchDriverRejectsUnpublishedDrivers(t *testing.T) {
	for _, driver := range []imageinfo.Driver{"", "amdgpu", "nouveau", imageinfo.Driver("nvidia-closed")} {
		if err := SwitchDriver(context.Background(), driver); err == nil {
			t.Errorf("SwitchDriver(%q) error = nil, want a refusal before pkexec is reached", driver)
		}
	}
}

// This is the assertion the journal exists to make possible: prove that
// clicking a switch would have run a specific, real argv — without granting
// privilege, and without inspecting fake-pkexec's captured file (that proves
// pkexec was invoked correctly; this proves ChairLift's own record of intent
// matches).
func TestRunHelperJournalsEveryInvocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, path)
	journal.Reset()
	t.Cleanup(journal.Reset)

	captured := filepath.Join(t.TempDir(), "captured-args")
	pkexec := writeFakePkexec(t, captured)

	if _, _, err := runHelper(context.Background(), pkexec, ubluehelper.CommandChannelSwitch, "testing"); err != nil {
		t.Fatalf("runHelper() error = %v, want nil", err)
	}

	entries := readJournal(t, path)
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Action != ubluehelper.CommandChannelSwitch {
		t.Errorf("journalled action = %q, want %q", entry.Action, ubluehelper.CommandChannelSwitch)
	}
	if entry.Suppressed != journal.SuppressedNone {
		t.Errorf("journalled suppressed = %q, want %q (a live run)", entry.Suppressed, journal.SuppressedNone)
	}
	wantArgv := []string{pkexec, HelperPath, ubluehelper.CommandChannelSwitch, "testing"}
	if !reflect.DeepEqual(entry.WouldRun, wantArgv) {
		t.Errorf("journalled WouldRun = %v, want %v", entry.WouldRun, wantArgv)
	}
}

// Under dry-run the journal must record the --dry-run flag was appended and
// mark the entry suppressed, so a reader of the journal alone — without the
// application log — can tell nothing actually happened.
func TestRunHelperJournalsDryRunAsSuppressed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, path)
	journal.Reset()
	t.Cleanup(journal.Reset)

	SetDryRun(true)
	t.Cleanup(func() { SetDryRun(false) })

	if _, _, err := runHelper(context.Background(), "pkexec-should-never-run", ubluehelper.CommandDXEnable); err != nil {
		t.Fatalf("runHelper() error = %v, want nil", err)
	}

	entries := readJournal(t, path)
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Suppressed != journal.SuppressedDryRun {
		t.Errorf("journalled suppressed = %q, want %q", entry.Suppressed, journal.SuppressedDryRun)
	}
	if entry.WouldRun[len(entry.WouldRun)-1] != "--dry-run" {
		t.Errorf("journalled WouldRun = %v, want it to end in --dry-run", entry.WouldRun)
	}
}

func readJournal(t *testing.T, path string) []journal.Entry {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading journal %s: %v", path, err)
	}

	var entries []journal.Entry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var entry journal.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decoding journal line %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}
