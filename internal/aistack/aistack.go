// Package aistack implements Agent Mode's runtime: llmman served as a
// systemd user unit on loopback, installed through Homebrew.
//
// ChairLift owns exactly three things here (ADR-0015): the Brewfile it hands
// to `brew bundle`, the user unit ServiceName, and the environment.d fragment
// EnvFragmentName that publishes OLLAMA_HOST to future sessions. llmman owns
// model storage, engine selection and inference; Homebrew owns the binary.
// Disabling removes only the unit and the fragment — the binary, Jan, and
// every downloaded model stay, because deleting gigabytes is a disk-space
// decision nobody made by turning a switch off.
//
// Nothing here is privileged. The unit lives in the user's
// ~/.config/systemd/user and is driven with `systemctl --user`, so there is
// no pkexec route and there must not become one.
package aistack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
)

const commandTimeout = 30 * time.Minute

// Address is where the daemon listens. Loopback only: llmman serves without
// authentication on a loopback bind, and refuses to start on a reachable one
// without keys, which ChairLift does not provision.
const Address = "127.0.0.1:17434"

// ServiceName is the systemd user unit ChairLift writes and owns. It is not
// llmman's own name so `brew services` and ChairLift can never manage the
// same file.
const ServiceName = "chairlift-llmman.service"

// EnvFragmentName is the environment.d fragment that publishes OLLAMA_HOST
// to sessions started after Agent Mode is enabled.
const EnvFragmentName = "10-chairlift-llmman.conf"

// Formula is the Homebrew formula that provides llmman.
const Formula = "llmmanorg/tap/llmman"

// JanFlatpak is the chat client installed alongside llmman. Its Flathub
// build is x86_64-only, so Brewfile omits it elsewhere.
const JanFlatpak = "ai.jan.Jan"

// State is Agent Mode's lifecycle state, as ADR-0015 defines it.
type State int

const (
	// StateUnavailable: this host cannot run Agent Mode (no Homebrew).
	StateUnavailable State = iota
	// StateUnconfigured: never set up — no llmman and no unit.
	StateUnconfigured
	// StateProvisioning: an enable is in flight, or the unit is installed
	// and readiness has not been checked yet.
	StateProvisioning
	// StateReady: the unit is installed and /llmman/node answers.
	StateReady
	// StateDegraded: the unit is installed but /llmman/node does not answer.
	StateDegraded
	// StateDisabled: llmman is installed but ChairLift's unit is not — the
	// user turned Agent Mode off, and its models were kept.
	StateDisabled
)

// Facts are the observations Resolve derives a State from.
type Facts struct {
	// Capable is the host capability floor (Homebrew present).
	Capable bool
	// Installed reports whether an llmman executable resolves.
	Installed bool
	// UnitPresent reports whether ChairLift's unit file exists.
	UnitPresent bool
	// Working is true while an enable or disable is in flight.
	Working bool
	// Checked is true once a readiness probe has run.
	Checked bool
	// Healthy is the readiness probe's answer; meaningful only if Checked.
	Healthy bool
}

// Resolve maps observations to a State. The unit file's presence decides
// whether Agent Mode is on; health decides whether "on" means ready.
func Resolve(f Facts) State {
	switch {
	case !f.Capable:
		return StateUnavailable
	case f.Working:
		return StateProvisioning
	case f.UnitPresent && !f.Checked:
		return StateProvisioning
	case f.UnitPresent && f.Healthy:
		return StateReady
	case f.UnitPresent:
		return StateDegraded
	case f.Installed:
		return StateDisabled
	default:
		return StateUnconfigured
	}
}

// On reports whether the switch should read on in this state.
func (s State) On() bool {
	return s == StateProvisioning || s == StateReady || s == StateDegraded
}

// Brewfile returns the bundle ChairLift installs for goarch. The tap comes
// first so the formula resolves. The formula is omitted when an llmman is
// already present however it was installed; `brew bundle` itself skips
// anything it already manages. An empty result means nothing to install.
func Brewfile(goarch string, haveLLMMan bool) string {
	var b string
	if !haveLLMMan {
		b = "tap \"llmmanorg/tap\"\nbrew \"" + Formula + "\"\n"
	}
	if goarch == "amd64" {
		b += "flatpak \"" + JanFlatpak + "\"\n"
	}
	return b
}

// RenderUnit returns the systemd user unit for the llmman executable at exe.
// exe must be the absolute path resolved at setup time; no Homebrew prefix is
// assumed.
func RenderUnit(exe string) (string, error) {
	if !filepath.IsAbs(exe) || strings.ContainsAny(exe, " \t\n\"'\\%$;") {
		return "", fmt.Errorf("llmman path %q is not a plain absolute path", exe)
	}
	return "[Unit]\n" +
		"Description=Agent Mode local model server (llmman)\n" +
		"Documentation=https://github.com/llmmanorg/llmman\n\n" +
		"[Service]\n" +
		"ExecStart=" + exe + " serve\n" +
		"Environment=LLMMAN_HOST=" + Address + "\n" +
		// The web UI's Shell tab is a terminal as this user; never serve it.
		"Environment=LLMMAN_SHELL=off\n" +
		// Troubleshooting prompts carry system details; do not keep them.
		"Environment=LLMMAN_NOHISTORY=1\n" +
		"Restart=on-failure\n" +
		"RestartSec=10\n\n" +
		"[Install]\n" +
		"WantedBy=default.target\n", nil
}

// EnvFragment is the environment.d fragment's content. It carries only
// OLLAMA_HOST: redirecting OpenAI SDK users session-wide is not a default.
func EnvFragment() string {
	return "OLLAMA_HOST=" + Address + "\n"
}

// Seams, replaced by tests.
var (
	configDir     = os.UserConfigDir
	lookPath      = exec.LookPath
	brewPath      = homebrew.ExecutablePath
	installBundle = homebrew.BundleInstall
	run           = execCommand
	nodeURL       = "http://" + Address + "/llmman/node"
)

func execCommand(ctx context.Context, name string, args ...string) (string, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		return trimmed, fmt.Errorf("%s %s: %w: %s", filepath.Base(name), strings.Join(args, " "), err, lastLine(trimmed))
	}
	return trimmed, nil
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func systemctl(ctx context.Context, args ...string) (string, error) {
	return run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

// UnitPath returns the absolute path of ChairLift's user unit.
func UnitPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", ServiceName), nil
}

// EnvFragmentPath returns the absolute path of the environment.d fragment.
func EnvFragmentPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "environment.d", EnvFragmentName), nil
}

// Executable resolves llmman: $PATH first, then beside the brew that
// internal/homebrew resolves, which is where the formula links it. It
// returns "" when neither exists.
func Executable() string {
	if path, err := lookPath("llmman"); err == nil && filepath.IsAbs(path) {
		return path
	}
	if brew := brewPath(); brew != "" {
		candidate := filepath.Join(filepath.Dir(brew), "llmman")
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

// Observe returns the non-blocking facts (no health probe), safe on the
// GTK main thread.
func Observe(capable bool) Facts {
	f := Facts{Capable: capable, Installed: Executable() != ""}
	if path, err := UnitPath(); err == nil {
		_, statErr := os.Stat(path)
		f.UnitPresent = statErr == nil
	}
	return f
}

// Healthy performs one bounded GET of /llmman/node and reports whether it
// answered 200 with a JSON object. systemctl is-active alone is not
// readiness: the process can be up and still be fetching its engine.
func Healthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodeURL, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	var node map[string]json.RawMessage
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(&node) == nil
}

// WaitHealthy polls Healthy until it succeeds or within elapses.
func WaitHealthy(ctx context.Context, within time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, within)
	defer cancel()
	for {
		if Healthy(ctx) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}
}

// Enable installs llmman (and Jan on x86_64) through `brew bundle`,
// validates the runtime with `llmman serve --pull-only`, writes the unit and
// the environment fragment atomically, and starts the service. A failure
// after the files were written removes what this call created, unless the
// unit already existed before it ran.
func Enable(ctx context.Context) error {
	unit, err := UnitPath()
	if err != nil {
		return err
	}
	fragment, err := EnvFragmentPath()
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would install %s, run llmman serve --pull-only, write %s and %s, and start %s",
			Formula, unit, fragment, ServiceName)
		return nil
	}

	if err := installRuntime(); err != nil {
		return err
	}
	exe := Executable()
	if exe == "" {
		return fmt.Errorf("%s installed but no llmman executable resolves", Formula)
	}
	content, err := RenderUnit(exe)
	if err != nil {
		return err
	}
	// Fetch the engine in the foreground so a runtime that cannot be
	// obtained is reported here, before anything claims to be ready.
	out, err := run(ctx, exe, "serve", "--pull-only")
	if err != nil {
		return fmt.Errorf("llmman runtime check: %w", err)
	}
	log.Printf("aistack: llmman serve --pull-only: %s", out)

	_, statErr := os.Stat(unit)
	existed := statErr == nil
	rollback := func() {
		if existed {
			return
		}
		_ = os.Remove(unit)
		_ = os.Remove(fragment)
		_, _ = systemctl(ctx, "daemon-reload")
	}

	if err := writeAtomic(unit, content); err != nil {
		return err
	}
	if err := writeAtomic(fragment, EnvFragment()); err != nil {
		rollback()
		return err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", ServiceName}, {"restart", ServiceName}} {
		if _, err := systemctl(ctx, args...); err != nil {
			rollback()
			return err
		}
	}
	// Best effort: processes started from now on in this session see it.
	// Nothing already running changes, and the UI says so.
	if _, err := run(ctx, "dbus-update-activation-environment", "--systemd", "OLLAMA_HOST="+Address); err != nil {
		log.Printf("aistack: publishing OLLAMA_HOST to the session: %v", err)
	}
	return nil
}

func installRuntime() error {
	bundle := Brewfile(runtime.GOARCH, Executable() != "")
	if bundle == "" {
		return nil
	}
	file, err := os.CreateTemp("", "agent-mode-*.Brewfile")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.WriteString(bundle); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return installBundle(file.Name())
}

// Disable stops the service and removes ChairLift's unit and fragment. If
// the stop fails, both stay unless systemd confirms the service is no longer
// active: removing the unit while it may still run would make the switch lie
// and drop the user's handle on the process.
func Disable(ctx context.Context) error {
	unit, err := UnitPath()
	if err != nil {
		return err
	}
	fragment, err := EnvFragmentPath()
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would stop %s and remove %s and %s", ServiceName, unit, fragment)
		return nil
	}

	if _, err := systemctl(ctx, "disable", "--now", ServiceName); err != nil {
		if verifyErr := verifyStopped(ctx, err); verifyErr != nil {
			return verifyErr
		}
	}
	for _, path := range []string{unit, fragment} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if _, err := systemctl(ctx, "unset-environment", "OLLAMA_HOST"); err != nil {
		log.Printf("aistack: clearing OLLAMA_HOST from the session: %v", err)
	}
	_, err = systemctl(ctx, "daemon-reload")
	return err
}

func verifyStopped(ctx context.Context, stopErr error) error {
	state, err := systemctl(ctx, "is-active", ServiceName)
	switch state = strings.TrimSpace(state); state {
	case "inactive", "failed", "unknown":
		return nil
	case "":
		return fmt.Errorf("%w; could not verify %s stopped: %v", stopErr, ServiceName, err)
	default:
		return fmt.Errorf("%w; %s is %s", stopErr, ServiceName, state)
	}
}

func writeAtomic(dest, content string) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(dest)+".tmp.*")
	if err != nil {
		return err
	}
	// A no-op after the rename succeeds; cleans the temp file otherwise.
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// DefaultContext returns a context bounded for the whole enable: a first
// `brew bundle` plus the engine fetch can take many minutes.
func DefaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), commandTimeout)
}
