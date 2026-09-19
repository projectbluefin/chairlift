package aistack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/gpu"
)

func TestSelectCoversEveryHardwareCase(t *testing.T) {
	tests := []struct {
		name            string
		set             gpu.Set
		wantVendor      gpu.Vendor
		wantImage       string
		wantAccelerator string
		wantAccelerated bool
		wantDevices     []string
	}{
		{
			name:            "nvidia workstation",
			set:             gpu.Set{NVIDIA: true},
			wantVendor:      gpu.VendorNVIDIA,
			wantImage:       "quay.io/ramalama/cuda@sha256:e6a6ccfe9e60ed05708a88eb3303711c188c155cee69500d21b9166871afba9e",
			wantAccelerator: "CUDA",
			wantAccelerated: true,
			wantDevices:     []string{"nvidia.com/gpu=all"},
		},
		{
			name:            "amd workstation",
			set:             gpu.Set{AMD: true},
			wantVendor:      gpu.VendorAMD,
			wantImage:       "quay.io/ramalama/rocm@sha256:e592700576a4a5bc7c3eebbbe8af4ae2c2351adb05f03e66aaaa822b4d31298f",
			wantAccelerator: "ROCm",
			wantAccelerated: true,
			wantDevices:     []string{"/dev/kfd", "/dev/dri"},
		},
		{
			name:            "intel laptop",
			set:             gpu.Set{Intel: true},
			wantVendor:      gpu.VendorIntel,
			wantImage:       "quay.io/ramalama/intel-gpu@sha256:02dc186b6eb9a4dba886cbdc05490e297ffee590b093c0293940273f663f025b",
			wantAccelerator: "Intel oneAPI",
			wantAccelerated: true,
			wantDevices:     []string{"/dev/dri"},
		},
		{
			name:            "no gpu",
			set:             gpu.Set{},
			wantVendor:      gpu.VendorNone,
			wantImage:       "quay.io/ramalama/ramalama@sha256:a3c0ee8d06554add6808fffe7476a16db8c821de34949fa6213449ac9d95f9f3",
			wantAccelerator: "CPU",
			wantAccelerated: false,
		},
		{
			// The hybrid laptop is the case a vendor-directory catalog gets
			// wrong: it has an Intel chip and an NVIDIA chip, and the model
			// should run on the NVIDIA one. What this case proves is the
			// selection, not the digest: leaving wantImage empty keeps a
			// digest roll from having to be typed twice, and the pinned shape
			// of every reference is asserted once by
			// TestEveryStackIsPinnedByAnImmutableDigest.
			name:            "hybrid laptop prefers the discrete card",
			set:             gpu.Set{Intel: true, NVIDIA: true},
			wantVendor:      gpu.VendorNVIDIA,
			wantAccelerator: "CUDA",
			wantAccelerated: true,
			wantDevices:     []string{"nvidia.com/gpu=all"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := Select(tt.set)

			if stack.Vendor != tt.wantVendor {
				t.Errorf("vendor = %q, want %q", stack.Vendor, tt.wantVendor)
			}
			if tt.wantImage != "" && stack.Image != tt.wantImage {
				t.Errorf("image = %q, want %q", stack.Image, tt.wantImage)
			}
			if stack.Accelerator != tt.wantAccelerator {
				t.Errorf("accelerator = %q, want %q", stack.Accelerator, tt.wantAccelerator)
			}
			if stack.Accelerated() != tt.wantAccelerated {
				t.Errorf("accelerated = %v, want %v", stack.Accelerated(), tt.wantAccelerated)
			}
			if len(stack.Devices) != len(tt.wantDevices) {
				t.Fatalf("devices = %v, want %v", stack.Devices, tt.wantDevices)
			}
			for i, device := range tt.wantDevices {
				if stack.Devices[i] != device {
					t.Errorf("device[%d] = %q, want %q", i, stack.Devices[i], device)
				}
			}
		})
	}
}

func TestEveryStackRendersAStartableUnit(t *testing.T) {
	for vendor, stack := range stacks {
		unit := RenderUnit(stack)

		for _, required := range []string{
			"[Container]",
			"Image=" + stack.Image,
			"ContainerName=chairlift-ai",
			"PublishPort=127.0.0.1:8080:8080",
			"WantedBy=default.target",
		} {
			if !strings.Contains(unit, required) {
				t.Errorf("%s unit is missing %q:\n%s", vendor, required, unit)
			}
		}

		for _, device := range stack.Devices {
			if !strings.Contains(unit, "AddDevice="+device) {
				t.Errorf("%s unit is missing AddDevice=%s", vendor, device)
			}
		}

		// A CPU stack that quietly asked for /dev/dri would fail to start on
		// the headless hosts it exists for.
		if !stack.Accelerated() && strings.Contains(unit, "AddDevice=") {
			t.Errorf("%s unit passes a device through:\n%s", vendor, unit)
		}
	}
}

func TestUnitNameDoesNotCollideWithBluefinctl(t *testing.T) {
	// bluefinctl installs ramalama.container into the same directory. If
	// ChairLift ever takes that name it silently overwrites the user's
	// bluefinctl stack.
	if UnitName == "ramalama.container" {
		t.Fatal("UnitName collides with bluefinctl's ramalama stack")
	}
	if !strings.HasPrefix(UnitName, "chairlift-") {
		t.Errorf("UnitName = %q, want a chairlift- prefix", UnitName)
	}
	if ServiceName != strings.TrimSuffix(UnitName, ".container")+".service" {
		t.Errorf("ServiceName = %q does not match UnitName %q", ServiceName, UnitName)
	}
}

// stubUnitDir points the package at a temporary quadlet directory and
// records the systemctl calls it makes.
func stubUnitDir(t *testing.T) (dir string, calls *[]string) {
	t.Helper()

	tmp := t.TempDir()
	previousDir := unitDir
	previousSystemctl := runSystemctl
	previousSystemctlOutput := runSystemctlOutput
	previousWriteQuadlet := writeQuadletFile
	t.Cleanup(func() {
		unitDir = previousDir
		runSystemctl = previousSystemctl
		runSystemctlOutput = previousSystemctlOutput
		writeQuadletFile = previousWriteQuadlet
		dryrun.Set(false)
	})

	unitDir = func() (string, error) { return tmp, nil }

	recorded := []string{}
	runSystemctl = func(_ context.Context, args ...string) error {
		recorded = append(recorded, strings.Join(args, " "))
		return nil
	}
	runSystemctlOutput = func(_ context.Context, args ...string) (string, error) {
		recorded = append(recorded, strings.Join(args, " "))
		return "", nil
	}

	return tmp, &recorded
}

func TestEnableWritesTheUnitAndStartsIt(t *testing.T) {
	dir, calls := stubUnitDir(t)

	if IsEnabled() {
		t.Fatal("IsEnabled reported true before Enable")
	}

	stack := Select(gpu.Set{AMD: true})
	if err := Enable(context.Background(), stack); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, UnitName))
	if err != nil {
		t.Fatalf("reading written unit: %v", err)
	}
	if !strings.Contains(string(data), stack.Image) {
		t.Errorf("written unit does not name %q:\n%s", stack.Image, data)
	}

	want := []string{"daemon-reload", "start " + ServiceName}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}

	if !IsEnabled() {
		t.Error("IsEnabled reported false after Enable")
	}
}

func TestEnableRemovesTheUnitWhenTheServiceWillNotStart(t *testing.T) {
	dir, _ := stubUnitDir(t)

	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "start" {
			return errors.New("unit not found")
		}
		return nil
	}

	if err := Enable(context.Background(), Select(gpu.Set{})); err == nil {
		t.Fatal("Enable returned no error when the service failed to start")
	}

	if _, err := os.Stat(filepath.Join(dir, UnitName)); !os.IsNotExist(err) {
		t.Error("a failed Enable left its quadlet on disk")
	}
	if IsEnabled() {
		t.Error("IsEnabled reported true after a failed Enable")
	}
}

func TestDisableRemovesTheUnit(t *testing.T) {
	dir, calls := stubUnitDir(t)

	if err := Enable(context.Background(), Select(gpu.Set{NVIDIA: true})); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	*calls = nil

	if err := Disable(context.Background()); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, UnitName)); !os.IsNotExist(err) {
		t.Error("Disable left the quadlet on disk")
	}
	want := []string{"stop " + ServiceName, "daemon-reload"}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}
}

func TestDisableSucceedsWhenTheServiceIsAlreadyDown(t *testing.T) {
	for _, state := range []string{"inactive", "failed", "unknown"} {
		t.Run(state, func(t *testing.T) {
			dir, calls := stubUnitDir(t)

			if err := Enable(context.Background(), Select(gpu.Set{})); err != nil {
				t.Fatalf("Enable: %v", err)
			}
			*calls = nil

			stopErr := errors.New("unit is not loaded")
			runSystemctl = func(_ context.Context, args ...string) error {
				*calls = append(*calls, strings.Join(args, " "))
				if args[0] == "stop" {
					return stopErr
				}
				return nil
			}
			runSystemctlOutput = func(_ context.Context, args ...string) (string, error) {
				*calls = append(*calls, strings.Join(args, " "))
				return state, errors.New("exit status 3")
			}

			if err := Disable(context.Background()); err != nil {
				t.Fatalf("Disable: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, UnitName)); !os.IsNotExist(err) {
				t.Errorf("Disable left the quadlet on disk after a failed stop of a %s service", state)
			}
			want := []string{"stop " + ServiceName, "is-active " + ServiceName, "daemon-reload"}
			if strings.Join(*calls, "|") != strings.Join(want, "|") {
				t.Errorf("systemctl calls = %v, want %v", *calls, want)
			}
		})
	}
}

func TestDisablePreservesTheUnitWhenStopFailsAndServiceRemainsActive(t *testing.T) {
	dir, calls := stubUnitDir(t)

	if err := Enable(context.Background(), Select(gpu.Set{})); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	*calls = nil

	stopErr := errors.New("refused to stop")
	runSystemctl = func(_ context.Context, args ...string) error {
		*calls = append(*calls, strings.Join(args, " "))
		if args[0] == "stop" {
			return stopErr
		}
		return nil
	}
	runSystemctlOutput = func(_ context.Context, args ...string) (string, error) {
		*calls = append(*calls, strings.Join(args, " "))
		return "active", nil
	}

	err := Disable(context.Background())
	if !errors.Is(err, stopErr) {
		t.Fatalf("Disable error = %v, want it to wrap %v", err, stopErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, UnitName)); statErr != nil {
		t.Fatalf("Disable removed or lost the unit after a failed stop: %v", statErr)
	}
	want := []string{"stop " + ServiceName, "is-active " + ServiceName}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}
}

func TestDisablePreservesTheUnitWhenStopFailureCannotBeVerified(t *testing.T) {
	dir, calls := stubUnitDir(t)

	if err := Enable(context.Background(), Select(gpu.Set{})); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	*calls = nil

	stopErr := errors.New("stop failed")
	statusErr := errors.New("dbus unavailable")
	runSystemctl = func(_ context.Context, args ...string) error {
		*calls = append(*calls, strings.Join(args, " "))
		if args[0] == "stop" {
			return stopErr
		}
		return nil
	}
	runSystemctlOutput = func(_ context.Context, args ...string) (string, error) {
		*calls = append(*calls, strings.Join(args, " "))
		return "", statusErr
	}

	err := Disable(context.Background())
	if !errors.Is(err, stopErr) {
		t.Fatalf("Disable error = %v, want it to wrap %v", err, stopErr)
	}
	if !strings.Contains(err.Error(), statusErr.Error()) {
		t.Errorf("Disable error = %q, want it to mention verification failure %q", err, statusErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, UnitName)); statErr != nil {
		t.Fatalf("Disable removed or lost the unit without verified stop: %v", statErr)
	}
	want := []string{"stop " + ServiceName, "is-active " + ServiceName}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	dir, calls := stubUnitDir(t)
	dryrun.Set(true)

	if err := Enable(context.Background(), Select(gpu.Set{AMD: true})); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := Disable(context.Background()); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, UnitName)); !os.IsNotExist(err) {
		t.Error("dry-run wrote a quadlet")
	}
	if len(*calls) != 0 {
		t.Errorf("dry-run ran systemctl: %v", *calls)
	}
}

func TestEnableAtomicallyCreatesUnitWithPermissionsAndNoTempFiles(t *testing.T) {
	dir, calls := stubUnitDir(t)

	stack := Select(gpu.Set{AMD: true})
	if err := Enable(context.Background(), stack); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	unitPath := filepath.Join(dir, UnitName)
	info, err := os.Stat(unitPath)
	if err != nil {
		t.Fatalf("stat written unit: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("quadlet permissions = %#o, want 0644", perm)
	}

	content, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("reading written unit: %v", err)
	}
	wantContent := RenderUnit(stack)
	if string(content) != wantContent {
		t.Errorf("written unit content mismatch:\ngot:\n%s\nwant:\n%s", string(content), wantContent)
	}

	wantCalls := []string{"daemon-reload", "start " + ServiceName}
	if strings.Join(*calls, "|") != strings.Join(wantCalls, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, wantCalls)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != UnitName {
		t.Errorf("directory contains unexpected files: %v, want only %s", entries, UnitName)
	}
}

func TestEnableReplacesExistingUnitAtomically(t *testing.T) {
	dir, _ := stubUnitDir(t)

	unitPath := filepath.Join(dir, UnitName)
	oldContent := []byte("[Unit]\nDescription=Old Unit\n")
	if err := os.WriteFile(unitPath, oldContent, 0o644); err != nil {
		t.Fatalf("writing old unit: %v", err)
	}

	newStack := Select(gpu.Set{NVIDIA: true})
	if err := Enable(context.Background(), newStack); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	got, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("reading replaced unit: %v", err)
	}
	if string(got) != RenderUnit(newStack) {
		t.Errorf("replaced unit content mismatch:\ngot:\n%s\nwant:\n%s", string(got), RenderUnit(newStack))
	}
}

func TestEnablePreservesExistingUnitOnWriteFailure(t *testing.T) {
	dir, calls := stubUnitDir(t)

	unitPath := filepath.Join(dir, UnitName)
	existingContent := []byte("[Unit]\nDescription=Preserve Me\n")
	if err := os.WriteFile(unitPath, existingContent, 0o644); err != nil {
		t.Fatalf("writing initial unit: %v", err)
	}

	writeErr := errors.New("simulated disk full")
	writeQuadletFile = func(_ string, _ []byte) error {
		return writeErr
	}

	stack := Select(gpu.Set{Intel: true})
	err := Enable(context.Background(), stack)
	if !errors.Is(err, writeErr) {
		t.Fatalf("Enable error = %v, want %v", err, writeErr)
	}

	got, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("reading existing unit: %v", err)
	}
	if string(got) != string(existingContent) {
		t.Errorf("existing unit modified on write failure:\ngot:\n%s\nwant:\n%s", string(got), string(existingContent))
	}

	if len(*calls) != 0 {
		t.Errorf("systemctl called on write failure: %v", *calls)
	}
}

func TestWriteQuadletAtomicallyCleansTempFileOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	unitPath := filepath.Join(tmpDir, UnitName)

	existingContent := []byte("[Unit]\nDescription=Intact\n")
	if err := os.WriteFile(unitPath, existingContent, 0o644); err != nil {
		t.Fatalf("writing initial unit: %v", err)
	}

	// Writing into a read-only directory causes os.CreateTemp to fail before rename
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0o555); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(readOnlyDir, 0o755)
	})

	destInReadOnly := filepath.Join(readOnlyDir, UnitName)
	if err := writeQuadletAtomically(destInReadOnly, []byte("test")); err == nil {
		t.Error("writeQuadletAtomically into read-only dir succeeded unexpectedly")
	}

	entries, err := os.ReadDir(readOnlyDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("abandoned temporary files found in read-only dir: %v", entries)
	}
}

func TestWriteQuadletAtomicallyCleansTempFileOnRenameFailure(t *testing.T) {
	tmpDir := t.TempDir()
	// If the destination path is a directory, os.Rename will fail on Linux (EISDIR/EEXIST).
	destDir := filepath.Join(tmpDir, UnitName)
	if err := os.Mkdir(destDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := writeQuadletAtomically(destDir, []byte("test content"))
	if err == nil {
		t.Fatal("writeQuadletAtomically succeeded when renaming over a directory")
	}

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != UnitName {
			t.Errorf("found abandoned temp file: %s", entry.Name())
		}
	}
}

func TestApplyOverridesReplacesTheImageAndModel(t *testing.T) {
	original := stacks[gpu.VendorNVIDIA]
	originalModel := servedModel
	t.Cleanup(func() {
		stacks[gpu.VendorNVIDIA] = original
		servedModel = originalModel
	})

	err := ApplyOverrides(
		map[string]string{"nvidia": "registry.example.internal/ramalama/cuda:pinned"},
		"ollama://qwen2.5:7b",
	)
	if err != nil {
		t.Fatalf("ApplyOverrides: %v", err)
	}

	stack := Select(gpu.Set{NVIDIA: true})
	if stack.Image != "registry.example.internal/ramalama/cuda:pinned" {
		t.Errorf("image = %q, want the override", stack.Image)
	}
	if !strings.Contains(RenderUnit(stack), "ollama://qwen2.5:7b") {
		t.Error("rendered unit does not serve the overridden model")
	}

	// An override for one vendor leaves the others alone.
	if Select(gpu.Set{AMD: true}).Image != "quay.io/ramalama/rocm@sha256:e592700576a4a5bc7c3eebbbe8af4ae2c2351adb05f03e66aaaa822b4d31298f" {
		t.Error("overriding nvidia disturbed the amd stack")
	}
}

func TestApplyOverridesRejectsBadInput(t *testing.T) {
	tests := []struct {
		name   string
		images map[string]string
		want   string
	}{
		{
			name:   "unknown vendor",
			images: map[string]string{"matrox": "example.com/x:1"},
			want:   `unknown vendor "matrox"`,
		},
		{
			name:   "empty image",
			images: map[string]string{"amd": ""},
			want:   "empty image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := Select(gpu.Set{AMD: true}).Image

			if err := ApplyOverrides(tt.images, ""); err == nil {
				t.Fatal("ApplyOverrides accepted invalid input")
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}

			if got := Select(gpu.Set{AMD: true}).Image; got != before {
				t.Errorf("a rejected override changed the amd image to %q", got)
			}
		})
	}
}

func TestApplyOverridesRejectsTheWholeOverrideWhenAnyEntryIsInvalid(t *testing.T) {
	// A valid entry paired with an invalid one must not commit the valid one.
	// Map iteration order is nondeterministic, so this is the case the bug hid:
	// with the old per-entry mutation a valid earlier entry could survive even
	// though a later entry was rejected.
	originalNVIDIA := stacks[gpu.VendorNVIDIA]
	originalAMD := stacks[gpu.VendorAMD]
	originalModel := servedModel
	t.Cleanup(func() {
		stacks[gpu.VendorNVIDIA] = originalNVIDIA
		stacks[gpu.VendorAMD] = originalAMD
		servedModel = originalModel
	})

	// Map iteration order is randomized per range, so a single attempt only
	// catches the per-entry mutation about half the time. Repeat until the
	// invalid entry has certainly been reached after the valid one.
	for range 32 {
		err := ApplyOverrides(
			map[string]string{"nvidia": "registry.example.internal/ramalama/cuda:pinned", "matrox": "example.com/x:1"},
			"ollama://qwen2.5:7b",
		)
		if err == nil {
			t.Fatal("ApplyOverrides accepted an override containing an unknown vendor")
		}
		if got := Select(gpu.Set{NVIDIA: true}).Image; got != originalNVIDIA.Image {
			t.Fatalf("the valid entry was committed anyway: image = %q", got)
		}
		if got := Select(gpu.Set{AMD: true}).Image; got != originalAMD.Image {
			t.Fatalf("the amd stack was disturbed: image = %q", got)
		}
		if servedModel != originalModel {
			t.Fatalf("the model was committed anyway: model = %q", servedModel)
		}
	}
}

func TestNoOverridesIsANoOp(t *testing.T) {
	if err := ApplyOverrides(nil, ""); err != nil {
		t.Fatalf("ApplyOverrides(nil, \"\"): %v", err)
	}
	if Select(gpu.Set{}).Image != "quay.io/ramalama/ramalama@sha256:a3c0ee8d06554add6808fffe7476a16db8c821de34949fa6213449ac9d95f9f3" {
		t.Error("an empty override changed the CPU stack")
	}
}

// TestEveryStackIsPinnedByAnImmutableDigest guards the property the exact
// digests cannot: a future edit that "refreshes" a reference back to a moving
// tag reopens issue #8, because the unit runs with SELinux confinement
// disabled and GPU devices attached and would adopt whatever the tag points
// at on the next Restart=on-failure pull.
func TestEveryStackIsPinnedByAnImmutableDigest(t *testing.T) {
	for vendor, stack := range stacks {
		if !strings.Contains(stack.Image, "@sha256:") {
			t.Errorf("%s image %q is not pinned by digest", vendor, stack.Image)
		}
		if strings.Contains(stack.Image, ":latest") {
			t.Errorf("%s image %q uses the mutable latest tag", vendor, stack.Image)
		}
	}
}
