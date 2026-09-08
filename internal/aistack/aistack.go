// Package aistack implements ChairLift's local-AI switch: one container that
// serves a local large language model on this machine's graphics hardware.
//
// It is ChairLift's port of bluefinctl's AI stacks, deliberately reduced.
// bluefinctl ships twelve quadlet definitions across two vendor directories
// (NIM, Triton, NeMo, RAPIDS, TensorFlow and PyTorch labs for NVIDIA; vLLM,
// Lemonade, two llama.cpp builds and RamaLama for AMD) and asks the user to
// pick one. That is a catalog for someone who already knows which serving
// runtime they want, and it has no answer at all for an Intel or a GPU-less
// host. ChairLift instead ships the single runtime that covers all four
// cases: RamaLama publishes a per-accelerator image, so the hardware picks
// the image and the user sees one switch.
//
// Nothing here is privileged. Quadlet units are written under the user's
// ~/.config/containers/systemd and started with `systemctl --user`, so the
// container runs rootless in the invoking account — the same reasoning that
// keeps gaming mode off the pkexec path. On a bootc host that also means
// nothing is layered onto the image.
package aistack

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/gpu"
)

const commandTimeout = 5 * time.Minute

// UnitName is the quadlet file ChairLift writes. It is deliberately not
// `ramalama.container`: bluefinctl installs its stacks under their own
// names into the same directory, and a user who has run both tools must not
// have one silently overwrite the other's unit.
const UnitName = "chairlift-ai.container"

// ServiceName is the systemd unit quadlet generates from UnitName.
const ServiceName = "chairlift-ai.service"

// containerName is the running container's name, matched to the unit so
// `podman ps` output is recognizable.
const containerName = "chairlift-ai"

// Port is the host port the OpenAI-compatible API is published on.
const Port = 8080

// servedModel is the model served. The default is small enough to run on a
// 4 GB card or on CPU, which matters because the stack is offered on every
// host including those with no GPU at all; ApplyOverrides replaces it.
var servedModel = "ollama://llama3.2:3b"

// Stack is the accelerator-specific container definition selected for this
// machine's hardware.
type Stack struct {
	// Vendor is the graphics vendor this stack targets.
	Vendor gpu.Vendor
	// Image is the RamaLama image built for that accelerator.
	Image string
	// Accelerator names the compute stack in user-facing text.
	Accelerator string
	// Devices are the quadlet AddDevice= values needed to reach the GPU.
	Devices []string
	// PodmanArgs are the quadlet PodmanArgs= values needed alongside them.
	PodmanArgs []string
}

// Accelerated reports whether this stack reaches a GPU. The CPU stack is a
// real, working choice — it is just markedly slower, which the UI says.
func (s Stack) Accelerated() bool {
	return s.Vendor != gpu.VendorNone
}

// stacks maps each vendor to its RamaLama image. Every reference was
// verified against quay.io by manifest request on 2026-08-17: the
// ramalama namespace publishes `ramalama` (CPU), `cuda`, `rocm`, and
// `intel-gpu`, each returning 200 for :latest.
var stacks = map[gpu.Vendor]Stack{
	gpu.VendorNVIDIA: {
		Vendor:      gpu.VendorNVIDIA,
		Image:       "quay.io/ramalama/cuda:latest",
		Accelerator: "CUDA",
		Devices:     []string{"nvidia.com/gpu=all"},
		PodmanArgs:  []string{"--security-opt=label=disable"},
	},
	gpu.VendorAMD: {
		Vendor:      gpu.VendorAMD,
		Image:       "quay.io/ramalama/rocm:latest",
		Accelerator: "ROCm",
		Devices:     []string{"/dev/kfd", "/dev/dri"},
		PodmanArgs:  []string{"--security-opt=label=disable", "--group-add=video"},
	},
	gpu.VendorIntel: {
		Vendor:      gpu.VendorIntel,
		Image:       "quay.io/ramalama/intel-gpu:latest",
		Accelerator: "Intel oneAPI",
		Devices:     []string{"/dev/dri"},
		PodmanArgs:  []string{"--security-opt=label=disable"},
	},
	gpu.VendorNone: {
		Vendor:      gpu.VendorNone,
		Image:       "quay.io/ramalama/ramalama:latest",
		Accelerator: "CPU",
	},
}

// ApplyOverrides replaces the image for one or more vendors, and the served
// model, from configuration. Both are ordinary config.yml settings rather
// than root-only ones: the container runs rootless in the invoking account,
// so pointing it at another image grants nothing a user could not get by
// running podman themselves. An air-gapped site mirrors the images; someone
// with a 24 GB card serves a larger model.
//
// An unknown vendor key is an error rather than a silent no-op, since a
// typo'd key would otherwise leave the site believing its mirror was in use.
func ApplyOverrides(images map[string]string, model string) error {
	for name, image := range images {
		vendor := gpu.Vendor(name)
		stack, known := stacks[vendor]
		if !known {
			return fmt.Errorf("ai_images: unknown vendor %q", name)
		}
		if image == "" {
			return fmt.Errorf("ai_images: vendor %q has an empty image", name)
		}
		stack.Image = image
		stacks[vendor] = stack
	}
	if model != "" {
		servedModel = model
	}
	return nil
}

// Select returns the stack for the detected hardware. It reuses
// gpu.Set.Primary, so a hybrid laptop gets the NVIDIA stack for the same
// reason it gets the NVIDIA image: that is the card the workload should run
// on.
func Select(set gpu.Set) Stack {
	return stacks[set.Primary()]
}

// Detect returns the stack for this machine.
func Detect() Stack {
	return Select(gpu.Detect())
}

// RenderUnit returns the quadlet .container file for a stack.
//
// There is no companion .network file. bluefinctl gives every stack its own
// podman network because its catalog anticipates stacks talking to each
// other; ChairLift runs exactly one container that talks only to the host
// over a published port, so a dedicated network would be a second file to
// install, remove, and keep in step for no behavior.
func RenderUnit(stack Stack) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[Unit]\nDescription=Local AI model server (%s)\nAfter=network-online.target\n\n", stack.Accelerator)

	b.WriteString("[Container]\n")
	fmt.Fprintf(&b, "ContainerName=%s\n", containerName)
	fmt.Fprintf(&b, "Image=%s\n", stack.Image)
	fmt.Fprintf(&b, "Exec=serve --port %d %s\n", Port, servedModel)
	for _, device := range stack.Devices {
		fmt.Fprintf(&b, "AddDevice=%s\n", device)
	}
	for _, arg := range stack.PodmanArgs {
		fmt.Fprintf(&b, "PodmanArgs=%s\n", arg)
	}
	fmt.Fprintf(&b, "PublishPort=%d:%d\n", Port, Port)
	b.WriteString("Volume=%h/ai-workspaces/ramalama:/root/.cache/ramalama:z\n\n")

	b.WriteString("[Service]\nRestart=on-failure\nRestartSec=10\n\n")
	b.WriteString("[Install]\nWantedBy=default.target\n")

	return b.String()
}

// unitDir is an injection seam for the quadlet directory, so the install and
// remove paths are testable without writing into a real home directory.
var unitDir = defaultUnitDir

func defaultUnitDir() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "containers", "systemd"), nil
}

// runSystemctl is an injection seam for the `systemctl --user` calls.
var runSystemctl = execSystemctl

func execSystemctl(ctx context.Context, args ...string) error {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	full := append([]string{"--user"}, args...)
	cmd := exec.CommandContext(runCtx, "systemctl", full...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", strings.Join(full, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

var dryRun = false

// SetDryRun enables/disables dry-run mode.
func SetDryRun(mode bool) {
	dryRun = mode
	log.Printf("aistack dry-run mode: %v", mode)
}

// IsDryRun reports whether dry-run mode is active.
func IsDryRun() bool {
	return dryRun
}

// IsAvailable reports whether this host can run the stack at all. Quadlet is
// a Podman feature, so without Podman there is nothing to install into.
func IsAvailable() bool {
	_, err := exec.LookPath("podman")
	return err == nil
}

// UnitPath returns the absolute path of the quadlet file.
func UnitPath() (string, error) {
	dir, err := unitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, UnitName), nil
}

// IsEnabled reports whether ChairLift's quadlet is installed. The unit file's
// presence is the state, not the container's running status: a stack whose
// container is restarting or whose image is still pulling is enabled, and
// reading it any other way would make the switch flicker during a multi-
// gigabyte first pull.
func IsEnabled() bool {
	path, err := UnitPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Enable writes the quadlet for the given stack and starts it.
func Enable(ctx context.Context, stack Stack) error {
	path, err := UnitPath()
	if err != nil {
		return err
	}

	if dryRun {
		log.Printf("[DRY-RUN] would write %s for %s and start %s", path, stack.Image, ServiceName)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(RenderUnit(stack)), 0o644); err != nil {
		return err
	}

	if err := runSystemctl(ctx, "daemon-reload"); err != nil {
		// The unit is on disk but systemd has not seen it. Take it back off
		// rather than leaving a host whose switch reads "on" and whose
		// service does not exist.
		_ = os.Remove(path)
		return err
	}
	if err := runSystemctl(ctx, "start", ServiceName); err != nil {
		_ = os.Remove(path)
		_ = runSystemctl(ctx, "daemon-reload")
		return err
	}
	return nil
}

// Disable stops the stack and removes its quadlet. The pulled image and the
// model cache under ~/ai-workspaces are left alone: they are large, they are
// expensive to re-fetch, and removing them is a disk-space decision the user
// did not make by turning a switch off.
func Disable(ctx context.Context) error {
	path, err := UnitPath()
	if err != nil {
		return err
	}

	if dryRun {
		log.Printf("[DRY-RUN] would stop %s and remove %s", ServiceName, path)
		return nil
	}

	// A stop failure is not fatal: the service may already be down, and the
	// unit still has to come off disk for the switch to mean anything.
	if err := runSystemctl(ctx, "stop", ServiceName); err != nil {
		log.Printf("aistack: stopping %s: %v", ServiceName, err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return runSystemctl(ctx, "daemon-reload")
}

// DefaultContext returns a context with the package's standard timeout.
func DefaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), commandTimeout)
}
