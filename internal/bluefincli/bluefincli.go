// Package bluefincli owns the curated Bluefin CLI packages and opt-in Bash
// configuration. Disabling removes managed source blocks, never packages.
package bluefincli

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
)

var (
	//go:embed cli.Brewfile
	manifest []byte
	//go:embed bluefin-cli.bash
	bashPayload []byte
	//go:embed bash-preexec-rearm.bash
	rearmPayload []byte
)

const (
	startMarker  = "### bluefin-cli source start"
	endMarker    = "### bluefin-cli source end"
	legacyStart  = "### bling.sh source start"
	legacyEnd    = "### bling.sh source end"
	legacySource = "test -f /usr/share/ublue-os/bling/bling.sh && source /usr/share/ublue-os/bling/bling.sh"
)

// State reports configuration, package readiness, and explicit prior opt-out
// independently, so callers can offer removal even when readiness is broken.
type State struct {
	Enabled           bool
	Configured        bool
	PackagesInstalled bool
	Legacy            bool
	Disabled          bool
}

// Supported reports whether the explicitly configured login shell is Bash.
func Supported() bool {
	return filepath.Base(os.Getenv("SHELL")) == "bash"
}

// Items returns the installable formulae from the embedded curated manifest.
// A fresh slice keeps caller presentation changes out of future installs.
func Items() []homebrew.BundleItem {
	var items []homebrew.BundleItem
	for _, line := range strings.Split(string(manifest), "\n") {
		if name, ok := strings.CutPrefix(line, "brew "); ok {
			items = append(items, homebrew.BundleItem{Name: strings.Trim(name, "\""), Kind: homebrew.BundleItemBrew})
		}
	}
	return items
}

type locations struct {
	rc  string
	dir string
}

func paths() (locations, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return locations{}, fmt.Errorf("locate Bash home: %w", err)
	}
	if !filepath.IsAbs(home) {
		return locations{}, errors.New("bash home must be an absolute path")
	}
	data := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(data) {
		data = filepath.Join(home, ".local", "share")
	}
	if strings.ContainsAny(home+data, "\r\n") {
		return locations{}, errors.New("bluefin CLI configuration paths must not contain newlines")
	}
	return locations{rc: filepath.Join(home, ".bashrc"), dir: filepath.Join(data, "chairlift", "bluefin-cli")}, nil
}

func quoteBash(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (p locations) sourceLine() string {
	payload := quoteBash(filepath.Join(p.dir, "bluefin-cli.bash"))
	return "test -f " + payload + " && source " + payload
}

// Status returns partial facts alongside errors; a failing Homebrew check or
// missing owned payload can never turn into an enabled switch. The ephemeral
// manifest checks the current embedded inventory, not an outdated disk copy.
func Status() (State, error) {
	var state State
	p, err := paths()
	if err != nil {
		return state, err
	}
	var problems []error
	rc, err := readRC(p.rc)
	if err != nil {
		problems = append(problems, err)
	} else {
		_, state.Configured, state.Legacy, err = rewriteBlocks(rc.content, "")
		if err != nil {
			problems = append(problems, err)
		}
	}
	marker, err := regularFile(filepath.Join(p.dir, "disabled"))
	if err != nil {
		problems = append(problems, err)
	}
	state.Disabled = marker != nil
	if state.Configured {
		for _, name := range []string{"bluefin-cli.bash", "homebrew.bash", "bash-preexec-rearm.bash", "cli.Brewfile"} {
			info, err := regularFile(filepath.Join(p.dir, name))
			if err != nil {
				problems = append(problems, err)
			} else if info == nil || info.Size() == 0 {
				problems = append(problems, fmt.Errorf("bluefin CLI payload %s is missing; enable Bluefin CLI again", name))
			}
		}
	}
	status, err := checkEmbedded()
	if err != nil {
		problems = append(problems, fmt.Errorf("check Bluefin CLI packages: %w", err))
	} else {
		state.PackagesInstalled = installed(status)
	}
	err = errors.Join(problems...)
	state.Enabled = state.Configured && state.PackagesInstalled && err == nil
	return state, err
}

func installed(status homebrew.BundleStatus) bool {
	return status == homebrew.BundleInstalled || status == homebrew.BundleUpdateAvailable
}

func checkEmbedded() (homebrew.BundleStatus, error) {
	dir, err := os.MkdirTemp("", "bluefin-cli-*")
	if err != nil {
		return homebrew.BundleIndeterminate, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "cli.Brewfile")
	if err := os.WriteFile(path, manifest, 0o600); err != nil {
		return homebrew.BundleIndeterminate, err
	}
	return homebrew.BundleCheck(path)
}

// Enable installs the curated formulae before enabling the Bash source. Every
// enable refreshes owned assets, including after an application upgrade.
func Enable() error {
	if !Supported() {
		return errors.New("bluefin CLI supports Bash only; set your login shell to Bash before enabling")
	}
	p, err := paths()
	if err != nil {
		return err
	}
	rc, err := readRC(p.rc)
	if err != nil {
		return err
	}
	content, configured, legacy, err := rewriteBlocks(rc.content, startMarker+"\n"+p.sourceLine()+"\n"+endMarker+"\n")
	if err != nil {
		return err
	}
	if _, err := regularFile(filepath.Join(p.dir, "disabled")); err != nil {
		return err
	}
	prefix, err := brewPrefix()
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Println("[DRY-RUN] Would enable Bluefin CLI for Bash")
		return nil
	}
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return fmt.Errorf("create Bluefin CLI data directory: %w", err)
	}
	assets := []struct {
		name string
		data []byte
	}{
		{"cli.Brewfile", manifest},
		{"homebrew.bash", homebrewEnvironment(prefix)},
		{"bash-preexec-rearm.bash", rearmPayload},
		{"bluefin-cli.bash", bashPayload},
	}
	for _, asset := range assets {
		if err := writeOwned(filepath.Join(p.dir, asset.name), asset.data); err != nil {
			return err
		}
	}
	manifestPath := filepath.Join(p.dir, "cli.Brewfile")
	status, err := homebrew.BundleCheck(manifestPath)
	if err != nil {
		return fmt.Errorf("check Bluefin CLI packages: %w", err)
	}
	if !installed(status) {
		if err := homebrew.BundleInstall(manifestPath); err != nil {
			return fmt.Errorf("install Bluefin CLI packages: %w", err)
		}
		status, err = homebrew.BundleCheck(manifestPath)
		if err != nil {
			return fmt.Errorf("verify Bluefin CLI packages: %w", err)
		}
		if !installed(status) {
			return errors.New("bluefin CLI packages are still missing after installation; Bash configuration was not enabled")
		}
	}
	if err := seedTldrCache(prefix); err != nil {
		return err
	}
	if !configured && !legacy {
		if len(content) != 0 && content[len(content)-1] != '\n' {
			content = append(content, '\n')
		}
		content = append(content, []byte(startMarker+"\n"+p.sourceLine()+"\n"+endMarker+"\n")...)
	}
	if err := rc.replace(content); err != nil {
		return err
	}
	marker := filepath.Join(p.dir, "disabled")
	if _, err := regularFile(marker); err != nil {
		return err
	}
	if err := os.Remove(marker); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("clear Bluefin CLI opt-out: %w", err)
	}
	return nil
}

// Disable removes only exact recognized Bash source blocks and remembers the
// explicit opt-out. It works regardless of the current shell or package health.
func Disable() error {
	p, err := paths()
	if err != nil {
		return err
	}
	rc, err := readRC(p.rc)
	if err != nil {
		return err
	}
	personal, configured, legacy, err := rewriteBlocks(rc.content, "")
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Println("[DRY-RUN] Would disable Bluefin CLI for Bash")
		return nil
	}
	if configured || legacy {
		if err := rc.replace(personal); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return fmt.Errorf("create Bluefin CLI data directory: %w", err)
	}
	return writeOwned(filepath.Join(p.dir, "disabled"), []byte("disabled\n"))
}

func brewPrefix() (string, error) {
	brew := homebrew.ExecutablePath()
	if brew == "" {
		return "", errors.New("homebrew is required to enable Bluefin CLI; install Homebrew first")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, brew, "--prefix")
	cmd.Env = homebrew.WithBrewPath(os.Environ(), brew)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("locate Homebrew prefix: %w", err)
	}
	prefix := strings.TrimSpace(string(out))
	if !filepath.IsAbs(prefix) || strings.ContainsAny(prefix, "\r\n") {
		return "", errors.New("homebrew returned an invalid installation prefix")
	}
	return prefix, nil
}

// seedTldrCache makes tldr useful offline after setup, without re-downloading
// an existing user's database on every enable or changing their configuration.
func seedTldrCache(prefix string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	binary := filepath.Join(prefix, "bin", "tldr")
	env := homebrew.WithBrewPath(os.Environ(), filepath.Join(prefix, "bin", "brew"))
	populated := func() bool {
		cmd := exec.CommandContext(ctx, binary, "--list", "--no-auto-update")
		cmd.Env = env
		out, err := cmd.Output()
		return err == nil && len(bytes.TrimSpace(out)) != 0
	}
	if populated() {
		return nil
	}
	cmd := exec.CommandContext(ctx, binary, "--update")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("seed tldr database: %w: %s", err, bytes.TrimSpace(out))
	}
	if !populated() {
		return errors.New("tldr database is empty after downloading; Bash configuration was not enabled")
	}
	return nil
}

func homebrewEnvironment(prefix string) []byte {
	return []byte("# Generated terminal environment for this Homebrew installation.\nexport HOMEBREW_PREFIX=" + quoteBash(prefix) + "\n" + `case ":${PATH-}:" in
    ":$HOMEBREW_PREFIX/bin:$HOMEBREW_PREFIX/sbin:"*) ;;
    *) export PATH="$HOMEBREW_PREFIX/bin:$HOMEBREW_PREFIX/sbin${PATH:+:$PATH}" ;;
esac
`)
}

// managedSource accepts only the canonical single-quoted command generated by
// Chairlift, including escaped apostrophes. No shell expansion is evaluated.
func managedSource(line string) bool {
	quoted, ok := strings.CutPrefix(line, "test -f ")
	if !ok || !strings.HasPrefix(quoted, "'") {
		return false
	}
	for i := 1; i < len(quoted); i++ {
		if quoted[i] != '\'' {
			continue
		}
		if strings.HasPrefix(quoted[i:], "'\\''") {
			i += 3
			continue
		}
		token := quoted[:i+1]
		path := strings.ReplaceAll(quoted[1:i], "'\\''", "'")
		return quoted[i+1:] == " && source "+token && quoteBash(path) == token &&
			filepath.IsAbs(path) && filepath.Clean(path) == path &&
			!strings.ContainsAny(path, "\x00\r\n") &&
			strings.HasSuffix(path, "/chairlift/bluefin-cli/bluefin-cli.bash")
	}
	return false
}

// rewriteBlocks replaces a single exact three-line block in place. Unknown
// contents, duplicate blocks, and unmatched markers fail closed.
func rewriteBlocks(content []byte, replacement string) ([]byte, bool, bool, error) {
	lines := strings.SplitAfter(string(content), "\n")
	var personal strings.Builder
	configured, legacy := false, false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSuffix(lines[i], "\n")
		var end string
		switch line {
		case startMarker, legacyStart:
			if configured || legacy {
				return nil, configured, legacy, errors.New("multiple Bluefin CLI Bash source blocks")
			}
			end = endMarker
			if line == legacyStart {
				end = legacyEnd
			}
		case endMarker, legacyEnd:
			return nil, configured, legacy, errors.New("unmatched Bluefin CLI Bash source end marker")
		default:
			for _, marker := range []string{startMarker, endMarker, legacyStart, legacyEnd} {
				if strings.HasPrefix(strings.TrimSpace(line), marker) {
					return nil, configured, legacy, errors.New("ambiguous Bluefin CLI Bash source marker; repair it before changing Bluefin CLI")
				}
			}
			personal.WriteString(lines[i])
			continue
		}
		if i+2 >= len(lines) || strings.TrimSuffix(lines[i+2], "\n") != end {
			return nil, configured, legacy, errors.New("unrecognized or incomplete Bluefin CLI Bash source block; preserve it and repair the markers before changing Bluefin CLI")
		}
		body := strings.TrimSuffix(lines[i+1], "\n")
		valid := managedSource(body)
		if line == legacyStart {
			valid = body == legacySource || body == "test -f /usr/share/ublue-os/bluefin-cli/bling.sh && source /usr/share/ublue-os/bluefin-cli/bling.sh"
		}
		if !valid {
			return nil, configured, legacy, errors.New("unrecognized or incomplete Bluefin CLI Bash source block; preserve it and repair the markers before changing Bluefin CLI")
		}
		if line == startMarker {
			configured = true
		} else {
			legacy = true
		}
		if replacement != "" {
			personal.WriteString(strings.TrimSuffix(replacement, "\n"))
			if strings.HasSuffix(lines[i+2], "\n") {
				personal.WriteByte('\n')
			}
		}
		i += 2
	}
	return []byte(personal.String()), configured, legacy, nil
}

type rcFile struct {
	entryPath string
	path      string
	entry     fs.FileInfo
	info      fs.FileInfo
	content   []byte
}

func readRC(path string) (rcFile, error) {
	rc := rcFile{entryPath: path, path: path}
	entry, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return rc, nil
	}
	if err != nil {
		return rc, fmt.Errorf("inspect Bash configuration: %w", err)
	}
	rc.entry = entry
	if entry.Mode()&os.ModeSymlink != 0 {
		rc.path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return rc, fmt.Errorf("resolve Bash configuration symlink (it must have an existing target): %w", err)
		}
	}
	rc.info, err = regularFile(rc.path)
	if err != nil {
		return rc, err
	}
	if rc.info == nil {
		return rc, errors.New("bash configuration disappeared while inspecting it")
	}
	rc.content, err = os.ReadFile(rc.path)
	if err != nil {
		return rc, fmt.Errorf("read Bash configuration: %w", err)
	}
	return rc, nil
}

func regularFile(path string) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must be a regular file, not a symlink or special file", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Uid != uint32(os.Getuid()) {
		return nil, fmt.Errorf("refuse to change %s: file is not owned by the current user", path)
	}
	return info, nil
}

func (rc rcFile) replace(content []byte) error {
	if bytes.Equal(content, rc.content) {
		return nil
	}
	entry, err := os.Lstat(rc.entryPath)
	if rc.entry == nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return errors.New("bash configuration changed while applying Bluefin CLI")
		}
	} else if err != nil || !os.SameFile(entry, rc.entry) {
		return errors.New("bash configuration or its symlink changed while applying Bluefin CLI")
	}
	if rc.info != nil {
		current, err := os.ReadFile(rc.path)
		if err != nil || !bytes.Equal(current, rc.content) {
			return errors.New("bash configuration contents changed while applying Bluefin CLI")
		}
	}
	mode := fs.FileMode(0o644)
	if rc.info != nil {
		mode = rc.info.Mode().Perm()
	}
	return replaceRegular(rc.path, content, mode, rc.info)
}

func writeOwned(path string, content []byte) error {
	info, err := regularFile(path)
	if err != nil {
		return err
	}
	return replaceRegular(path, content, 0o644, info)
}

// Write next to the resolved regular target and rename that target, never the
// user's .bashrc symlink. Recheck identity before replacing to avoid clobbering
// a link or file switched by a concurrent dotfile editor.
func replaceRegular(path string, content []byte, mode fs.FileMode, original fs.FileInfo) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".bluefin-cli-*")
	if err != nil {
		return fmt.Errorf("prepare %s: %w", path, err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	current, err := regularFile(path)
	if err != nil {
		return err
	}
	if (original == nil && current != nil) || (original != nil && (current == nil || !os.SameFile(original, current))) {
		return fmt.Errorf("%s changed while applying Bluefin CLI", path)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
