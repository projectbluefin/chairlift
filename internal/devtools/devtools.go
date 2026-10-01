// Package devtools implements the user-scope parts of Bluefin's documented
// developer workstation: optional editors, Lima Ubuntu, and Docker CLI tools.
// Only fixed ublue helper commands grant host access or control Docker.
package devtools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/outputtail"
	"github.com/projectbluefin/chairlift/internal/ublue"
)

type Tool struct {
	Name      string
	Package   string
	Cask      bool
	AMD64Only bool
}

// ErrNewLogin means access was granted but this session cannot use it yet.
var ErrNewLogin = errors.New("hardware virtualization access granted; log out and back in, then enable WSL Mode again")

// Tools follows Common's ide.Brewfile and devmode terminal-editor choices.
// The current Toolbox cask contains only the x86_64 Linux archive.
func Tools() []Tool {
	return []Tool{
		{Name: "Dev Container CLI", Package: "devcontainer"},
		{Name: "VSCode Stable", Package: "ublue-os/tap/visual-studio-code-linux", Cask: true},
		{Name: "VSCode Insiders", Package: "ublue-os/tap/visual-studio-code-linux@insiders", Cask: true},
		{Name: "VSCodium", Package: "ublue-os/tap/vscodium-linux", Cask: true},
		{Name: "Antigravity", Package: "ublue-os/tap/antigravity-linux", Cask: true},
		{Name: "JetBrains Toolbox", Package: "ublue-os/tap/jetbrains-toolbox-linux", Cask: true, AMD64Only: true},
		{Name: "Neovim", Package: "neovim"},
		{Name: "Helix", Package: "helix"},
		{Name: "Vim", Package: "vim"},
		{Name: "Micro", Package: "micro"},
	}
}

func HostSupported() bool {
	return runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
}

func (t Tool) Supported() bool {
	return HostSupported() && (!t.AMD64Only || runtime.GOARCH == "amd64")
}

func Install(t Tool) error {
	if !t.Supported() {
		return fmt.Errorf("%s is not available for this architecture", t.Name)
	}
	if t.Cask {
		if err := homebrew.Tap("ublue-os/tap"); err != nil {
			return err
		}
		// Trust only the package the user explicitly chose, not the whole tap.
		if err := homebrew.TrustPackages(homebrew.UntrustedTap{Name: "ublue-os/tap", Casks: []string{t.Package}}); err != nil {
			return err
		}
	}
	return homebrew.Install(t.Package, t.Cask)
}

func executable(name string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if brew := homebrew.ExecutablePath(); brew != "" {
		path := filepath.Join(filepath.Dir(brew), name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return path
		}
	}
	return ""
}

func command(ctx context.Context, mutate bool, program string, args ...string) (string, error) {
	if mutate && dryrun.Enabled() {
		log.Printf("[DRY-RUN] would execute: %s %v", program, args)
		return "", nil
	}
	path := executable(program)
	if path == "" {
		return "", fmt.Errorf("%s is not installed", program)
	}
	timeout := 30 * time.Second
	if mutate {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = 5 * time.Second
	var capture interface {
		io.Writer
		String() string
	}
	var stderr *outputtail.Writer
	if mutate {
		capture = outputtail.New(64 * 1024)
	} else {
		capture = &bytes.Buffer{}
		stderr = outputtail.New(64 * 1024)
	}
	cmd.Stdout, cmd.Stderr = capture, capture
	if !mutate {
		cmd.Stderr = stderr
	}
	err := cmd.Run()
	if mutate && err == nil {
		return "", nil
	}
	output := capture.String()
	if err != nil {
		if stderr != nil {
			if diagnostic := stderr.String(); diagnostic != "" {
				output += "\n" + diagnostic
			}
		}
		return output, fmt.Errorf("%s %v: %w: %s", program, args, err, strings.TrimSpace(output))
	}
	return output, nil
}

type WSLState struct {
	Exists  bool
	Running bool
	Ready   bool
}

func WSLStatus(ctx context.Context) (WSLState, error) {
	if executable("limactl") == "" {
		return WSLState{}, nil
	}
	output, err := command(ctx, false, "limactl", "list", "--json")
	if err != nil {
		return WSLState{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	state := WSLState{}
	for {
		var vm struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		if err := decoder.Decode(&vm); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return WSLState{}, fmt.Errorf("reading Lima instances: %w", err)
		}
		if vm.Name == "ubuntu" {
			state.Exists = true
			state.Running = vm.Status == "Running"
		}
	}
	if state.Running {
		_, err := command(ctx, false, "limactl", "shell", "ubuntu", "true")
		state.Ready = err == nil
	}
	return state, nil
}

// KVMAvailable checks actual invoking-session read/write access, not account
// membership that only reaches the next login. This is a read-only probe.
func KVMAvailable() (exists, accessible bool) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false, false
	}
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return true, false
	}
	_ = f.Close()
	return true, true
}

func SetWSL(ctx context.Context, enabled bool, progress func(string)) error {
	stage := func(text string) {
		if progress != nil {
			progress(text)
		}
	}
	if dryrun.Enabled() {
		if enabled {
			log.Print("[DRY-RUN] would install Lima, wire SSH, start Ubuntu LTS with writable home, enable autostart, and probe its shell")
		} else {
			log.Print("[DRY-RUN] would disable Ubuntu autostart and stop the VM without deleting data")
		}
		return nil
	}
	if enabled {
		if !HostSupported() {
			return errors.New("lima requires a Linux x86_64 or ARM64 host")
		}
		exists, accessible := KVMAvailable()
		if !exists {
			return errors.New("hardware virtualization is unavailable: /dev/kvm is missing; enable it in firmware")
		}
		if !accessible {
			stage("Requesting hardware virtualization access…")
			if err := ublue.EnableKVMAccess(ctx); err != nil {
				return err
			}
			return ErrNewLogin
		}
		stage("Installing Lima and its virtual-machine dependencies…")
		if err := homebrew.Install("lima", false); err != nil {
			return err
		}
		if err := wireSSH(); err != nil {
			return err
		}
	}
	state, err := WSLStatus(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		if !state.Exists {
			return nil
		}
		stage("Stopping Ubuntu; keeping its disk and your files…")
		_, autoErr := command(ctx, true, "limactl", "autostart", "disable", "ubuntu")
		var stopErr error
		if state.Running {
			_, stopErr = command(ctx, true, "limactl", "stop", "--tty=false", "ubuntu")
		}
		return errors.Join(autoErr, stopErr)
	}
	stage("Starting Ubuntu LTS; the first image download is about 600 MB…")
	if !state.Exists {
		_, err = command(ctx, true, "limactl", "start", "--name", "ubuntu", "--mount-writable", "--tty=false", "template:ubuntu-lts")
	} else if !state.Running {
		_, err = command(ctx, true, "limactl", "start", "--tty=false", "ubuntu")
	}
	if err != nil {
		return err
	}
	if _, err := command(ctx, true, "limactl", "autostart", "enable", "ubuntu"); err != nil {
		return err
	}
	stage("Checking the Ubuntu shell…")
	_, err = command(ctx, false, "limactl", "shell", "ubuntu", "true")
	return err
}

func wireSSH() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "config")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	const include = "Include ~/.lima/*/ssh.config"
	found := false
	for line := range strings.SplitSeq(string(b), "\n") {
		if strings.TrimSpace(line) == include {
			found = true
			break
		}
	}
	if !found {
		b = append([]byte(include+"\n\n"), b...)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

type DockerState struct {
	Available bool
	Active    bool
	Ready     bool
}

// DockerStatus reads the system daemon independently of the optional CLI.
// Ready requires an accessible local socket that answers Docker's ping.
func DockerStatus(ctx context.Context) (DockerState, error) {
	return dockerStatus(ctx, "/var/run/docker.sock")
}

func dockerStatus(ctx context.Context, socket string) (DockerState, error) {
	output, err := command(ctx, false, "systemctl", "show", "docker.service", "--property=LoadState,ActiveState")
	if err != nil {
		return DockerState{}, err
	}
	state := DockerState{}
	loadSeen, activeSeen := false, false
	for line := range strings.SplitSeq(output, "\n") {
		loadSeen = loadSeen || strings.HasPrefix(line, "LoadState=")
		activeSeen = activeSeen || strings.HasPrefix(line, "ActiveState=")
		if line == "LoadState=loaded" {
			state.Available = true
		}
		if line == "ActiveState=active" {
			state.Active = true
		}
	}
	if !loadSeen || !activeSeen {
		return DockerState{}, errors.New("docker daemon state was not reported by systemd")
	}
	if !state.Active {
		return state, nil
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return state, err
	}
	response, err := client.Do(request)
	if err != nil {
		return state, nil
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16))
	state.Ready = err == nil && response.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == "OK"
	return state, nil
}

func SetDocker(ctx context.Context, enabled bool, progress func(string)) error {
	if enabled {
		state, err := DockerStatus(ctx)
		if err != nil {
			return err
		}
		if !state.Available {
			return errors.New("the base image does not provide Docker's daemon; installing its CLI cannot enable containers")
		}
		for _, pkg := range []string{"docker", "docker-compose", "lazydocker", "dive"} {
			if progress != nil {
				progress("Installing " + pkg + "…")
			}
			if err := homebrew.Install(pkg, false); err != nil {
				return err
			}
		}
	}
	if progress != nil {
		progress("Changing the Docker daemon…")
	}
	return ublue.SetDocker(ctx, enabled)
}
