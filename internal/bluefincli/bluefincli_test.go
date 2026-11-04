package bluefincli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
)

// A real executable boundary keeps the lifecycle, manifest parsing, and Bash
// startup real without installing packages or touching the developer's home.
type testHost struct {
	home      string
	data      string
	prefix    string
	installed string
}

func newHost(t *testing.T) testHost {
	t.Helper()
	root := t.TempDir()
	h := testHost{home: filepath.Join(root, "personal home's directory"), data: filepath.Join(root, "shared data's directory"), prefix: filepath.Join(root, "brew prefix's directory"), installed: filepath.Join(root, "packages-installed")}
	for _, dir := range []string{h.home, h.data, filepath.Join(h.prefix, "bin")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", h.home)
	t.Setenv("XDG_DATA_HOME", h.data)
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("TEST_BREW_PREFIX", h.prefix)
	t.Setenv("TEST_INSTALLED", h.installed)
	t.Setenv("TEST_INSTALL_FAIL", "")
	t.Setenv("TEST_CHECK_FAIL", "")
	t.Setenv("TEST_UPDATE_AVAILABLE", "")
	t.Setenv("TEST_TLDR_CACHE", filepath.Join(root, "tldr-pages"))
	t.Setenv("TEST_TLDR_UPDATE_FAIL", "")
	t.Setenv("BLING_SOURCED", "")
	t.Setenv("_CHAIRLIFT_BLUEFIN_CLI_SOURCED", "")
	t.Setenv("PATH", filepath.Join(h.prefix, "bin")+":/usr/bin:/bin")
	previous := dryrun.Enabled()
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(previous) })
	writeTestFile(t, filepath.Join(h.prefix, "bin", "brew"), `#!/bin/bash
case "$1 $2" in
  '--prefix ') printf '%s\n' "$TEST_BREW_PREFIX" ;;
  'bundle check')
    [[ -z "$TEST_CHECK_FAIL" ]] || { echo 'check unavailable' >&2; exit 2; }
	[[ -f "$TEST_INSTALLED" ]] || exit 1
	[[ -z "$TEST_UPDATE_AVAILABLE" || "$*" == *--no-upgrade* ]] ;;
  'bundle install')
    [[ -z "$TEST_INSTALL_FAIL" ]] || { echo 'installation failed' >&2; exit 2; }
    for arg in "$@"; do
      case "$arg" in --file=*) cp -- "${arg#--file=}" "$TEST_INSTALLED" ;; esac
    done ;;
  *) echo "unexpected brew command: $*" >&2; exit 3 ;;
esac
`, 0o755)
	for _, name := range []string{"direnv", "starship", "zoxide", "mise"} {
		writeTestFile(t, filepath.Join(h.prefix, "bin", name), "#!/bin/bash\nexit 0\n", 0o755)
	}
	writeTestFile(t, filepath.Join(h.prefix, "bin", "tldr"), `#!/bin/bash
case "$1" in
  --list) [[ -s "$TEST_TLDR_CACHE" ]] && cat "$TEST_TLDR_CACHE" ;;
  --update)
    [[ -z "$TEST_TLDR_UPDATE_FAIL" ]] || { echo 'cache download failed' >&2; exit 1; }
    printf 'bash\ntar\n' > "$TEST_TLDR_CACHE" ;;
  *) exit 2 ;;
esac
`, 0o755)
	return h
}

func writeTestFile(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or cannot be inspected: %v", path, err)
	}
}

func (h testHost) rc() string     { return filepath.Join(h.home, ".bashrc") }
func (h testHost) assets() string { return filepath.Join(h.data, "chairlift", "bluefin-cli") }

func runBash(t *testing.T, h testHost, script string) string {
	t.Helper()
	cmd := exec.Command("bash", "--noprofile", "--norc", "-c", script)
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin", "HOMEBREW_PREFIX=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Bash startup failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestSupportedUsesOnlyExplicitBashShell(t *testing.T) {
	for _, test := range []struct {
		shell string
		want  bool
	}{{"", false}, {"/usr/bin/bash", true}, {"bash", true}, {"/bin/sh", false}} {
		t.Run(test.shell, func(t *testing.T) {
			t.Setenv("SHELL", test.shell)
			if got := Supported(); got != test.want {
				t.Fatalf("Supported() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestEnableInstallsManifestBeforeActivatingAndPreservesPersonalConfiguration(t *testing.T) {
	h := newHost(t)
	personal := "# My terminal\nexport PERSONAL_SETTING=kept\nalias mine='printf personal'\n"
	writeTestFile(t, h.rc(), personal, 0o600)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(h.rc())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("personal mode lost: %v, %v", info, err)
	}
	if !strings.HasPrefix(readTestFile(t, h.rc()), personal) {
		t.Fatal("personal configuration changed")
	}
	installed, err := homebrew.BundleContents(h.installed)
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 16 {
		t.Fatalf("installed %d formulae, want curated 16", len(installed))
	}
	if !reflect.DeepEqual(installed, Items()) {
		t.Fatalf("display and installed formulae disagree: %v / %v", Items(), installed)
	}
	for _, item := range installed {
		if item.Kind != homebrew.BundleItemBrew {
			t.Fatalf("non-formula item installed: %+v", item)
		}
	}
	foundUGrep := false
	for _, item := range installed {
		foundUGrep = foundUGrep || item.Name == "ugrep"
	}
	if !foundUGrep {
		t.Fatal("grep replacement was not installed")
	}
	state, err := Status()
	if err != nil || !state.Enabled || !state.Configured || !state.PackagesInstalled || state.Legacy || state.Disabled {
		t.Fatalf("unexpected enabled state: %+v, %v", state, err)
	}
	if out := runBash(t, h, `source "$HOME/.bashrc"; printf '%s\n' "$PERSONAL_SETTING" "$HOMEBREW_PREFIX"; command -v brew`); !strings.Contains(out, "kept\n"+h.prefix+"\n"+filepath.Join(h.prefix, "bin", "brew")) {
		t.Fatalf("fresh Bash did not reach actual Homebrew: %s", out)
	}
	items := Items()
	items[0].Name = "user-modified"
	if Items()[0].Name == "user-modified" {
		t.Fatal("caller changed future install inventory")
	}
}

func TestFailedInstallNeverEnablesShell(t *testing.T) {
	h := newHost(t)
	personal := "export PERSONAL_SETTING=kept\n"
	writeTestFile(t, h.rc(), personal, 0o640)
	t.Setenv("TEST_INSTALL_FAIL", "1")
	if err := Enable(); err == nil {
		t.Fatal("failed package installation reported success")
	}
	if got := readTestFile(t, h.rc()); got != personal {
		t.Fatalf("failed install edited rc: %q", got)
	}
	state, err := Status()
	if err != nil || state.Enabled || state.Configured {
		t.Fatalf("failed installation became enabled: %+v, %v", state, err)
	}
}

func TestStatusNeverEnablesMissingPayloadOrFailedPackageCheck(t *testing.T) {
	h := newHost(t)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.assets(), "bluefin-cli.bash")); err != nil {
		t.Fatal(err)
	}
	state, err := Status()
	if err == nil || state.Enabled || !state.Configured || !state.PackagesInstalled {
		t.Fatalf("missing payload readiness: %+v, %v", state, err)
	}
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_CHECK_FAIL", "1")
	state, err = Status()
	if err == nil || state.Enabled || !state.Configured || state.PackagesInstalled {
		t.Fatalf("failed check was masked: %+v, %v", state, err)
	}
	// Broken readiness must not prevent removing the active managed source.
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyMigrationAndReenableAreIdempotent(t *testing.T) {
	h := newHost(t)
	personal := "# personal\nsource \"$HOME/personal.bash\"\n"
	writeTestFile(t, filepath.Join(h.home, "personal.bash"), "export PERSONAL_SETTING=kept\n", 0o600)
	legacy := "### bling.sh source start\ntest -f /usr/share/ublue-os/bling/bling.sh && source /usr/share/ublue-os/bling/bling.sh\n### bling.sh source end\n"
	writeTestFile(t, h.rc(), personal+legacy, 0o600)
	state, err := Status()
	if err != nil || !state.Legacy || state.Configured || state.Enabled {
		t.Fatalf("legacy status: %+v %v", state, err)
	}
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	first := readTestFile(t, h.rc())
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, h.rc()); got != first {
		t.Fatal("repeated enable duplicated shell configuration")
	}
	state, err = Status()
	if err != nil || state.Legacy || !state.Enabled {
		t.Fatalf("migration status: %+v %v", state, err)
	}
	// Re-enabling upgrades files owned by ChairLift, rather than trusting their presence.
	writeTestFile(t, filepath.Join(h.assets(), "bluefin-cli.bash"), "outdated payload\n", 0o644)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if out := runBash(t, h, `source "$HOME/.bashrc"; printf '%s' "$HOMEBREW_PREFIX"`); !strings.HasSuffix(out, h.prefix) {
		t.Fatalf("owned payload was not refreshed: %s", out)
	}
	// Disable targets installed Bash configuration even from another current shell.
	t.Setenv("SHELL", "/bin/sh")
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, h.rc()); got != personal {
		t.Fatalf("disable changed personal text: %q", got)
	}
	if _, err := os.Stat(h.installed); err != nil {
		t.Fatal("disable removed packages", err)
	}
	state, err = Status()
	if err != nil || state.Enabled || state.Configured || !state.PackagesInstalled || !state.Disabled {
		t.Fatalf("disabled status: %+v %v", state, err)
	}
	if err := Enable(); err == nil {
		t.Fatal("unsupported shell enabled Bash configuration")
	}
	t.Setenv("SHELL", "/bin/bash")
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	state, err = Status()
	if err != nil || !state.Enabled || state.Disabled {
		t.Fatalf("reenable status: %+v %v", state, err)
	}
}

func TestLegacyPathsPreservePersonalPrecedence(t *testing.T) {
	for _, path := range []string{"/usr/share/ublue-os/bling/bling.sh", "/usr/share/ublue-os/bluefin-cli/bling.sh"} {
		t.Run(path, func(t *testing.T) {
			h := newHost(t)
			before := "# personal before\n"
			after := "PERSONAL_AFTER=${HOMEBREW_PREFIX:-missing}\nalias ls='printf personal'\nPROMPT_COMMAND='printf personal-prompt'"
			block := legacyStart + "\ntest -f " + path + " && source " + path + "\n" + legacyEnd + "\n"
			writeTestFile(t, h.rc(), before+block+after, 0o600)
			if err := Disable(); err != nil {
				t.Fatal(err)
			}
			if got := readTestFile(t, h.rc()); got != before+after {
				t.Fatalf("legacy removal changed personal commands: %q", got)
			}
			writeTestFile(t, h.rc(), before+block+after, 0o600)
			for range 2 {
				if err := Enable(); err != nil {
					t.Fatal(err)
				}
				if got := readTestFile(t, h.rc()); !strings.HasPrefix(got, before) || !strings.HasSuffix(got, after) {
					t.Fatalf("migration moved personal commands: %q", got)
				}
				out := runBash(t, h, `source "$HOME/.bashrc"; printf 'after:%s\nprompt:%s\n' "$PERSONAL_AFTER" "$PROMPT_COMMAND"; alias ls`)
				if !strings.Contains(out, "after:"+h.prefix+"\nprompt:printf personal-prompt\n") || !strings.Contains(out, "alias ls='printf personal'") {
					t.Fatalf("personal commands no longer override owned hooks: %s", out)
				}
			}
		})
	}
}

func TestChangedDataHomeKeepsOwnedHookManageable(t *testing.T) {
	for _, action := range []string{"enable", "disable"} {
		t.Run(action, func(t *testing.T) {
			h := newHost(t)
			before, after := "# before\n", "# after without newline"
			writeTestFile(t, h.rc(), before, 0o600)
			if err := Enable(); err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, h.rc(), readTestFile(t, h.rc())+after, 0o600)
			oldPayload := filepath.Join(h.assets(), "bluefin-cli.bash")
			h.data = filepath.Join(t.TempDir(), "new data's home")
			t.Setenv("XDG_DATA_HOME", h.data)
			if action == "disable" {
				if err := Disable(); err != nil {
					t.Fatal(err)
				}
				if got := readTestFile(t, h.rc()); got != before+after {
					t.Fatalf("old hook was not removed cleanly: %q", got)
				}
				return
			}
			if err := Enable(); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(oldPayload); err != nil {
				t.Fatal(err)
			}
			if got := readTestFile(t, h.rc()); !strings.HasPrefix(got, before) || !strings.HasSuffix(got, after) {
				t.Fatalf("repointing moved personal commands: %q", got)
			}
			if out := runBash(t, h, `source "$HOME/.bashrc"; printf '%s' "$HOMEBREW_PREFIX"`); out != h.prefix {
				t.Fatalf("new data home did not initialize Bash: %q", out)
			}
		})
	}
}

func TestUnsafeOrAmbiguousSourceLinesAreNeverRewritten(t *testing.T) {
	for _, change := range []string{"command", "mismatch", "expansion", "unquoted", "duplicate", "mixed"} {
		t.Run(change, func(t *testing.T) {
			h := newHost(t)
			payload := filepath.Join(h.assets(), "bluefin-cli.bash")
			source := "test -f " + quoteBash(payload) + " && source " + quoteBash(payload)
			switch change {
			case "command":
				source += "; touch " + quoteBash(filepath.Join(h.home, "injected"))
			case "mismatch":
				source = "test -f " + quoteBash(payload) + " && source '/other/chairlift/bluefin-cli/bluefin-cli.bash'"
			case "expansion":
				source = strings.ReplaceAll(source, "'", "\"")
			case "unquoted":
				source = "test -f " + payload + " && source " + payload
			}
			block := startMarker + "\n" + source + "\n" + endMarker + "\n"
			if change == "duplicate" {
				block += block
			}
			if change == "mixed" {
				block += legacyStart + "\n" + legacySource + "\n" + legacyEnd + "\n"
			}
			writeTestFile(t, h.rc(), block, 0o600)
			if err := Enable(); err == nil {
				t.Fatal("unsafe enable succeeded")
			}
			if err := Disable(); err == nil {
				t.Fatal("unsafe disable succeeded")
			}
			if got := readTestFile(t, h.rc()); got != block {
				t.Fatal("unsafe block was changed")
			}
			assertAbsent(t, filepath.Join(h.home, "injected"))
			assertAbsent(t, h.installed)
		})
	}
}

func TestDisableRecordsOptOutWithoutExistingConfiguration(t *testing.T) {
	h := newHost(t)
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, h.rc())
	state, err := Status()
	if err != nil || !state.Disabled || state.Enabled {
		t.Fatalf("opt-out was forgotten: %+v %v", state, err)
	}
	t.Setenv("TEST_INSTALL_FAIL", "1")
	if err := Enable(); err == nil {
		t.Fatal("failed install succeeded")
	}
	state, err = Status()
	if err != nil || !state.Disabled {
		t.Fatalf("failed enable cleared explicit opt-out: %+v %v", state, err)
	}
}

func TestMalformedAndAmbiguousManagedBlocksAreNeverRewritten(t *testing.T) {
	for _, block := range []string{
		"### bling.sh source start\nexport KEEP_ME=1\n### bling.sh source end\n",
		"### bling.sh source start\nexport KEEP_ME=1\n",
		endMarker + "\nexport KEEP_ME=1\n",
		" ### bling.sh source start\nexport KEEP_ME=1\n### bling.sh source end\n",
		"### bling.sh source start\r\nexport KEEP_ME=1\r\n### bling.sh source end\r\n",
		"### bling.sh source start\ntest -f /usr/share/ublue-os/bling/bling.sh && source /usr/share/ublue-os/bling/bling.sh\n### bling.sh source end\n### bling.sh source start\ntest -f /usr/share/ublue-os/bling/bling.sh && source /usr/share/ublue-os/bling/bling.sh\n### bling.sh source end\n",
	} {
		t.Run(block, func(t *testing.T) {
			h := newHost(t)
			writeTestFile(t, h.rc(), block, 0o600)
			if err := Enable(); err == nil {
				t.Fatal("malformed enable succeeded")
			}
			if err := Disable(); err == nil {
				t.Fatal("malformed disable succeeded")
			}
			if got := readTestFile(t, h.rc()); got != block {
				t.Fatal("unrecognized managed content was removed")
			}
			assertAbsent(t, h.installed)
		})
	}
}

func TestSymlinkedConfigurationKeepsTheLinkAndTargetMode(t *testing.T) {
	h := newHost(t)
	target := filepath.Join(h.home, "dotfiles bashrc")
	personal := "export PERSONAL_SETTING=kept\n"
	writeTestFile(t, target, personal, 0o640)
	if err := os.Symlink(target, h.rc()); err != nil {
		t.Fatal(err)
	}
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if link, err := os.Readlink(h.rc()); err != nil || link != target {
		t.Fatalf("user symlink replaced: %q, %v", link, err)
	}
	if got := readTestFile(t, target); got != personal {
		t.Fatalf("symlink target personal text changed: %q", got)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("symlink target mode changed: %v %v", info, err)
	}
}

func TestBrokenConfigurationSymlinkIsNotFollowedOrReplaced(t *testing.T) {
	h := newHost(t)
	target := filepath.Join(h.home, "missing-target")
	if err := os.Symlink(target, h.rc()); err != nil {
		t.Fatal(err)
	}
	if err := Enable(); err == nil {
		t.Fatal("broken symlink enable succeeded")
	}
	if err := Disable(); err == nil {
		t.Fatal("broken symlink disable succeeded")
	}
	if link, err := os.Readlink(h.rc()); err != nil || link != target {
		t.Fatalf("broken symlink replaced: %q, %v", link, err)
	}
	assertAbsent(t, target)
	assertAbsent(t, h.installed)
}

func TestDryRunCreatesNoConfigurationPayloadOrPackageMutation(t *testing.T) {
	h := newHost(t)
	dryrun.Set(true)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, h.rc())
	assertAbsent(t, h.assets())
	assertAbsent(t, h.installed)
	assertAbsent(t, os.Getenv("TEST_TLDR_CACHE"))
}

func TestBashPayloadInitializesHooksOnlyOnceAndHonorsMiseOptOut(t *testing.T) {
	for _, mise := range []string{"", "0"} {
		t.Run("mise="+mise, func(t *testing.T) {
			h := newHost(t)
			for _, name := range []string{"direnv", "starship", "zoxide", "mise"} {
				writeTestFile(t, filepath.Join(h.prefix, "bin", name), "#!/bin/bash\n"+`name=${0##*/}
case "$name:$*" in
  direnv:'hook bash'|starship:'init bash'|zoxide:'init bash'|mise:'activate bash')
    printf '%s\n' "${name}_count=\$((\${${name}_count:-0}+1))" ;;
  *) echo 'wrong shell argument' >&2; exit 50 ;;
esac
`, 0o755)
			}
			for _, name := range []string{"eza", "bat", "ug"} {
				writeTestFile(t, filepath.Join(h.prefix, "bin", name), "#!/bin/bash\nprintf '%s\\n' \"${0##*/}:$*\"\n", 0o755)
			}
			writeTestFile(t, filepath.Join(h.prefix, "bin", "xdg-open"), "#!/bin/bash\nprintf '%s' \"$*\" > \"$HOME/opened-url\"\nprintf 'opener output must be suppressed'\n", 0o755)
			if err := Enable(); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MISE_BASH_AUTO_ACTIVATE", mise)
			out := runBash(t, h, `source "$HOME/.bashrc"; source "$HOME/.bashrc"; printf 'hooks:%s,%s,%s,%s\n' "${direnv_count:-0}" "${starship_count:-0}" "${zoxide_count:-0}" "${mise_count:-0}"; shopt -s expand_aliases; eval 'ls; ll; l.; l1; cat file; grep term; egrep term; fgrep term; xzgrep term; xzegrep term; xzfgrep term; open https://example.test/'`)
			if got := readTestFile(t, filepath.Join(h.home, "opened-url")); got != "https://example.test/" {
				t.Fatalf("open alias did not invoke xdg-open: %q", got)
			}
			if strings.Contains(out, "opener output") {
				t.Fatal("open alias did not silence desktop opener output")
			}
			wantHooks := "hooks:1,1,1,1"
			if mise == "0" {
				wantHooks = "hooks:1,1,1,0"
			}
			if !strings.Contains(out, wantHooks) {
				t.Fatalf("duplicate or missing hooks: %s", out)
			}
			for _, expected := range []string{"eza:\n", "eza:-l --icons=auto --group-directories-first", "eza:-d .", "eza:-1", "bat:--style=plain --pager=never file", "ug:term", "ug:-E term", "ug:-F term", "ug:-z term", "ug:-zE term", "ug:-zF term"} {
				if !strings.Contains(out, expected) {
					t.Errorf("missing alias behavior %q in %s", expected, out)
				}
			}
		})
	}
}

func TestBashPreexecRearmPreservesPersonalPromptAndHistoryHooks(t *testing.T) {
	h := newHost(t)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	out := runBash(t, h, `PROMPT_COMMAND=('printf personal-prompt'); personal_history() { :; }; __bp_preexec_invoke_exec() { :; }; preexec_functions=(personal_history); source "$HOME/.bashrc"; source "$HOME/.bashrc"; printf 'history:%s\n' "${preexec_functions[*]}"; printf 'prompts:%s\n' "${#PROMPT_COMMAND[@]}"; for hook in "${PROMPT_COMMAND[@]}"; do eval "$hook"; done; printf '\n'; trap -p DEBUG`)
	for _, expected := range []string{"history:personal_history", "prompts:2", "personal-prompt", "__bp_preexec_invoke_exec"} {
		if !strings.Contains(out, expected) {
			t.Errorf("lost preexec/prompt behavior %q in %s", expected, out)
		}
	}
}

func TestStatusDistinguishesUpdatesFromMissingPackages(t *testing.T) {
	h := newHost(t)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_UPDATE_AVAILABLE", "1")
	state, err := Status()
	if err != nil || !state.PackagesInstalled || !state.Enabled {
		t.Fatalf("available updates hid installed packages: %+v %v", state, err)
	}
	if err := os.Remove(h.installed); err != nil {
		t.Fatal(err)
	}
	state, err = Status()
	if err != nil || state.PackagesInstalled || state.Enabled || !state.Configured {
		t.Fatalf("missing packages still ready: %+v %v", state, err)
	}
}

func TestDefaultDataHomeSuppliesDurableBashPayload(t *testing.T) {
	h := newHost(t)
	t.Setenv("XDG_DATA_HOME", "")
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.home, ".local", "share", "chairlift", "bluefin-cli", "bluefin-cli.bash")); err != nil {
		t.Fatal(err)
	}
	if out := runBash(t, h, `source "$HOME/.bashrc"; command -v brew`); strings.TrimSpace(out) != filepath.Join(h.prefix, "bin", "brew") {
		t.Fatalf("default payload did not initialize Homebrew: %s", out)
	}
}

func TestEnableSeedsTldrPagesAndReusesPopulatedCache(t *testing.T) {
	newHost(t)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	if pages := readTestFile(t, os.Getenv("TEST_TLDR_CACHE")); !strings.Contains(pages, "bash\n") {
		t.Fatalf("setup left tldr without usable pages: %q", pages)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_TLDR_UPDATE_FAIL", "1")
	if err := Enable(); err != nil {
		t.Fatalf("re-enabling required a network download despite populated pages: %v", err)
	}
	if state, err := Status(); err != nil || !state.Enabled {
		t.Fatalf("re-enabled state: %+v, %v", state, err)
	}
}

func TestTldrSeedFailureDoesNotActivateBashAndCanRetry(t *testing.T) {
	h := newHost(t)
	personal := "export PERSONAL_SETTING=kept\n"
	writeTestFile(t, h.rc(), personal, 0o600)
	t.Setenv("TEST_TLDR_UPDATE_FAIL", "1")
	if err := Enable(); err == nil {
		t.Fatal("setup succeeded without downloading tldr pages")
	}
	if got := readTestFile(t, h.rc()); got != personal {
		t.Fatalf("failed setup activated Bash or changed personal settings: %q", got)
	}
	t.Setenv("TEST_TLDR_UPDATE_FAIL", "")
	if err := Enable(); err != nil {
		t.Fatalf("cache seed was not retryable: %v", err)
	}
}

func TestBashPrefersCuratedToolsOverSystemDefaults(t *testing.T) {
	h := newHost(t)
	if err := Enable(); err != nil {
		t.Fatal(err)
	}
	systemBin := t.TempDir()
	// An older image-provided client rejects options the curated client supports.
	writeTestFile(t, filepath.Join(systemBin, "tldr"), "#!/bin/bash\nexit 86\n", 0o755)
	path := systemBin + ":/usr/bin:/bin:" + filepath.Join(h.prefix, "bin") + ":" + filepath.Join(h.prefix, "sbin")
	out := runBash(t, h, "export PATH="+quoteBash(path)+`; source "$HOME/.bashrc"; tldr --list --no-auto-update`)
	if out != "bash\ntar\n" {
		t.Fatalf("curated tldr could not read setup's cached pages: %q", out)
	}
}
