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
			wantImage:       "quay.io/ramalama/cuda:latest",
			wantAccelerator: "CUDA",
			wantAccelerated: true,
			wantDevices:     []string{"nvidia.com/gpu=all"},
		},
		{
			name:            "amd workstation",
			set:             gpu.Set{AMD: true},
			wantVendor:      gpu.VendorAMD,
			wantImage:       "quay.io/ramalama/rocm:latest",
			wantAccelerator: "ROCm",
			wantAccelerated: true,
			wantDevices:     []string{"/dev/kfd", "/dev/dri"},
		},
		{
			name:            "intel laptop",
			set:             gpu.Set{Intel: true},
			wantVendor:      gpu.VendorIntel,
			wantImage:       "quay.io/ramalama/intel-gpu:latest",
			wantAccelerator: "Intel oneAPI",
			wantAccelerated: true,
			wantDevices:     []string{"/dev/dri"},
		},
		{
			name:            "no gpu",
			set:             gpu.Set{},
			wantVendor:      gpu.VendorNone,
			wantImage:       "quay.io/ramalama/ramalama:latest",
			wantAccelerator: "CPU",
			wantAccelerated: false,
		},
		{
			// The hybrid laptop is the case a vendor-directory catalog gets
			// wrong: it has an Intel chip and an NVIDIA chip, and the model
			// should run on the NVIDIA one.
			name:            "hybrid laptop prefers the discrete card",
			set:             gpu.Set{Intel: true, NVIDIA: true},
			wantVendor:      gpu.VendorNVIDIA,
			wantImage:       "quay.io/ramalama/cuda:latest",
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
			if stack.Image != tt.wantImage {
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
	t.Cleanup(func() {
		unitDir = previousDir
		runSystemctl = previousSystemctl
		dryrun.Set(false)
	})

	unitDir = func() (string, error) { return tmp, nil }

	recorded := []string{}
	runSystemctl = func(_ context.Context, args ...string) error {
		recorded = append(recorded, strings.Join(args, " "))
		return nil
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
	dir, _ := stubUnitDir(t)

	if err := Enable(context.Background(), Select(gpu.Set{})); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "stop" {
			return errors.New("unit is not loaded")
		}
		return nil
	}

	if err := Disable(context.Background()); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, UnitName)); !os.IsNotExist(err) {
		t.Error("Disable left the quadlet on disk after a failed stop")
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
	if Select(gpu.Set{AMD: true}).Image != "quay.io/ramalama/rocm:latest" {
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

func TestNoOverridesIsANoOp(t *testing.T) {
	if err := ApplyOverrides(nil, ""); err != nil {
		t.Fatalf("ApplyOverrides(nil, \"\"): %v", err)
	}
	if Select(gpu.Set{}).Image != "quay.io/ramalama/ramalama:latest" {
		t.Error("an empty override changed the CPU stack")
	}
}
