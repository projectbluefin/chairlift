package e2e

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updexhelper"
)

// The rejection table in TestInstalledBundleAndHelperBoundary proves the
// installed helpers refuse argv shapes nobody should be able to authenticate.
// It cannot prove the opposite half: that an argv shape the parser *accepts*
// still reaches an operation. `main` dispatches on a `switch` with no
// `default`, so a command whose arm is deleted, renamed, or never added
// parses cleanly, matches nothing, falls out of the switch and exits 0 having
// done nothing at all. The command packages under `cmd/` are also outside the
// enforced unit gate, which runs `./internal/...`, so no test in the
// repository executes that switch except the ones here.
//
// Each case below runs one accepted command through the shipped binary with
// `--dry-run`, which is the only non-mutating way to reach the far side of
// the dispatch: every arm prints the exact argv it would hand to bootc,
// systemctl, usermod or gpasswd and returns before running it. Asserting that
// command-specific line is what separates "the arm ran" from "the switch fell
// through", which a bare exit status cannot distinguish.
//
// Two commands cannot dry-run to completion off a Bluefin-family host:
// `channel-switch` and `driver-switch` resolve their target from the
// root-owned descriptor at imageinfo.DescriptorPath before the dry-run check,
// by design — the reference must never arrive as an argument. Their
// expectation is therefore chosen from the descriptor's presence rather than
// loosened: on a host without it the refusal naming that path is itself proof
// the arm was entered, and on a host with it the dry-run line is required.

// acceptedCommand is one accepted invocation of a shipped helper and the
// output that proves its dispatch arm executed.
type acceptedCommand struct {
	name   string
	helper string
	// command is the first argv word, cross-checked against the helper
	// package's SupportedCommands so a new command cannot skip this gate.
	command string
	args    []string
	// wantStdout are substrings every successful run must print. Empty when
	// the case expects a refusal instead.
	wantStdout []string
	// wantStderr are substrings a refusing run must print. A refusal is only
	// accepted when the case declares one.
	wantStderr []string
	// allowRefusal permits the command to exit 1 if its stderr contains one
	// of the allowed substrings (used when dry-running on a host whose
	// running image or configuration refuses the switch).
	allowRefusal []string
	// wantJSONStdout requires stdout to decode as JSON, for the helper that
	// reports its result as a JSON document rather than a dry-run line.
	wantJSONStdout bool
	// needsInvokingUser fills PKEXEC_UID with a resolvable unprivileged uid,
	// as pkexec would.
	needsInvokingUser bool
}

// absentUpdexFeature is a name no updex configuration defines, so
// enable-feature and disable-feature reach updex and are refused by it
// deterministically instead of changing the host.
const absentUpdexFeature = "chairlift-e2e-absent-feature"

func acceptedHelperCommands() []acceptedCommand {
	const ublue = "chairlift-ublue-helper"
	const updex = "chairlift-updex-helper"

	// A missing descriptor is the normal state on a CI runner; a real
	// Bluefin-family host has one. Both are exact expectations.
	descriptorPresent := descriptorExists()

	switchCases := func(name, command, argument string, refusalPrefixes ...string) acceptedCommand {
		this := acceptedCommand{
			name:    name,
			helper:  ublue,
			command: command,
			args:    []string{command, argument, "--dry-run"},
		}
		if descriptorPresent {
			this.wantStdout = []string{"[DRY-RUN] would execute: bootc [switch"}
			this.allowRefusal = refusalPrefixes
			return this
		}
		this.wantStderr = []string{"reading " + imageinfo.DescriptorPath}
		return this
	}

	return []acceptedCommand{
		switchCases("channel switch to stable", ubluehelper.CommandChannelSwitch, ubluehelper.ChannelStable, "no stable image is defined for the running tag"),
		switchCases("driver switch to standard", ubluehelper.CommandDriverSwitch, string(imageinfo.DriverStandard), "no standard image is published for"),
		{
			name:              "developer mode enable",
			helper:            ublue,
			command:           ubluehelper.CommandDXEnable,
			args:              []string{ubluehelper.CommandDXEnable, "--dry-run"},
			needsInvokingUser: true,
			wantStdout: []string{
				"[DRY-RUN] would execute: usermod [-aG",
				"log out and back in to take effect",
			},
		},
		{
			name:              "developer mode disable",
			helper:            ublue,
			command:           ubluehelper.CommandDXDisable,
			args:              []string{ubluehelper.CommandDXDisable, "--dry-run"},
			needsInvokingUser: true,
			wantStdout: []string{
				"[DRY-RUN] would execute: gpasswd [-d",
				"log out and back in to take effect",
			},
		},
		{
			name:       "restart",
			helper:     ublue,
			command:    ubluehelper.CommandRestart,
			args:       []string{ubluehelper.CommandRestart, "--dry-run"},
			wantStdout: []string{"[DRY-RUN] would execute: systemctl [reboot]"},
		},
		{
			name:       "rollback",
			helper:     ublue,
			command:    ubluehelper.CommandRollback,
			args:       []string{ubluehelper.CommandRollback, "--dry-run"},
			wantStdout: []string{"[DRY-RUN] would execute: bootc [rollback]"},
		},
		{
			name:       "factory reset",
			helper:     ublue,
			command:    ubluehelper.CommandFactoryReset,
			args:       []string{ubluehelper.CommandFactoryReset, "--dry-run"},
			wantStdout: []string{"[DRY-RUN] would execute: bootc [install reset --experimental --apply]"},
		},
		{
			name:    "automatic updates enable",
			helper:  ublue,
			command: ubluehelper.CommandAutoEnable,
			args:    []string{ubluehelper.CommandAutoEnable, "--dry-run"},
			// Both steps must appear: unmasking without enabling leaves the
			// timer in a state the user was never shown.
			wantStdout: []string{
				"[DRY-RUN] would execute: systemctl [unmask",
				"[DRY-RUN] would execute: systemctl [enable --now",
			},
		},
		{
			name:    "automatic updates disable",
			helper:  ublue,
			command: ubluehelper.CommandAutoDisable,
			args:    []string{ubluehelper.CommandAutoDisable, "--dry-run"},
			wantStdout: []string{
				"[DRY-RUN] would execute: systemctl [disable --now",
				"[DRY-RUN] would execute: systemctl [mask",
			},
		},
		{
			name:           "updex update",
			helper:         updex,
			command:        updexhelper.CommandUpdate,
			args:           []string{updexhelper.CommandUpdate, "--dry-run"},
			wantJSONStdout: true,
		},
		{
			name:       "updex enable feature",
			helper:     updex,
			command:    updexhelper.CommandEnableFeature,
			args:       []string{updexhelper.CommandEnableFeature, absentUpdexFeature, "--dry-run"},
			wantStderr: []string{absentUpdexFeature, "not found"},
		},
		{
			name:       "updex disable feature",
			helper:     updex,
			command:    updexhelper.CommandDisableFeature,
			args:       []string{updexhelper.CommandDisableFeature, absentUpdexFeature, "--dry-run"},
			wantStderr: []string{absentUpdexFeature, "not found"},
		},
	}
}

func TestAcceptedHelperCommandsReachTheirDispatchArm(t *testing.T) {
	stage := stageInstall(t)
	invokingUID := unprivilegedUID(t)

	for _, test := range acceptedHelperCommands() {
		t.Run(test.name, func(t *testing.T) {
			helper := filepath.Join(stage, "usr/bin", test.helper)
			requireExecutable(t, helper)

			cmd := exec.Command(helper, test.args...)
			pkexecUID := ""
			if test.needsInvokingUser {
				if invokingUID == "" {
					t.Skip("no resolvable unprivileged uid is available to stand in for PKEXEC_UID")
				}
				pkexecUID = invokingUID
			}
			cmd.Env = append(os.Environ(), "PKEXEC_UID="+pkexecUID, "LANG=C", "LC_ALL=C")

			var stdout, stderr strings.Builder
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			runErr := cmd.Run()

			invocation := test.helper + " " + strings.Join(test.args, " ")
			exitCode := 0
			if runErr != nil {
				var exitErr *exec.ExitError
				if !errors.As(runErr, &exitErr) {
					t.Fatalf("%s did not run: %v", invocation, runErr)
				}
				exitCode = exitErr.ExitCode()
			}

			// The parser accepted this shape in internal/ubluehelper's and
			// internal/updexhelper's own tests. If the shipped binary answers
			// with a usage line or an unknown-command line instead, the
			// installed helper and the parser have diverged.
			for _, rejection := range []string{"usage: ", "unknown command: "} {
				if strings.Contains(stderr.String(), rejection) {
					t.Fatalf("%s was rejected with %q; accepted shapes must not print a %q line\nstderr:\n%s",
						invocation, strings.TrimSpace(stderr.String()), rejection, stderr.String())
				}
			}

			if len(test.wantStderr) > 0 {
				if exitCode != 1 {
					t.Fatalf("%s exit status = %d, want 1\nstdout:\n%s\nstderr:\n%s",
						invocation, exitCode, stdout.String(), stderr.String())
				}
				for _, want := range test.wantStderr {
					if !strings.Contains(stderr.String(), want) {
						t.Errorf("%s stderr = %q, want substring %q", invocation, stderr.String(), want)
					}
				}
				return
			}

			if exitCode != 0 {
				if len(test.allowRefusal) > 0 && exitCode == 1 {
					for _, refusal := range test.allowRefusal {
						if strings.Contains(stderr.String(), refusal) {
							// The arm was reached and refused deterministically.
							return
						}
					}
				}
				t.Fatalf("%s exit status = %d, want 0\nstdout:\n%s\nstderr:\n%s",
					invocation, exitCode, stdout.String(), stderr.String())
			}

			// The failure this whole file exists for: a dispatch arm that is
			// absent produces exactly this — a clean exit and no output.
			if strings.TrimSpace(stdout.String()) == "" {
				t.Fatalf("%s exited 0 without writing anything to stdout; the accepted command reached no dispatch arm",
					invocation)
			}

			for _, want := range test.wantStdout {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("%s stdout = %q, want substring %q", invocation, stdout.String(), want)
				}
			}

			if test.wantJSONStdout {
				var decoded any
				if err := json.Unmarshal([]byte(stdout.String()), &decoded); err != nil {
					t.Errorf("%s stdout is not JSON (%v): %q", invocation, err, stdout.String())
				}
			}
		})
	}
}

// TestEveryHelperCommandHasAnAcceptedCase is the drift half of the gate. The
// table above is hand-written, but its completeness is derived from the
// parsers' own command sets, so a tenth ublue subcommand or a fourth updex
// command cannot be added with only a rejection case for company.
func TestEveryHelperCommandHasAnAcceptedCase(t *testing.T) {
	covered := map[string]map[string]bool{}
	for _, test := range acceptedHelperCommands() {
		if covered[test.helper] == nil {
			covered[test.helper] = map[string]bool{}
		}
		if test.args[0] != test.command {
			t.Errorf("case %q declares command %q but passes %q as argv[1]",
				test.name, test.command, test.args[0])
		}
		covered[test.helper][test.command] = true
	}

	for helper, supported := range map[string][]string{
		"chairlift-ublue-helper": ubluehelper.SupportedCommands(),
		"chairlift-updex-helper": updexhelper.SupportedCommands(),
	} {
		var missing []string
		for _, command := range supported {
			if !covered[helper][command] {
				missing = append(missing, command)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s accepts %v with no accepted-command case; add one to acceptedHelperCommands",
				helper, missing)
		}

		for command := range covered[helper] {
			if !slicesContains(supported, command) {
				t.Errorf("%s has an accepted-command case for %q, which its parser no longer accepts",
					helper, command)
			}
		}
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, candidate := range haystack {
		if candidate == needle {
			return true
		}
	}
	return false
}

func descriptorExists() bool {
	info, err := os.Stat(imageinfo.DescriptorPath)
	return err == nil && info.Mode().IsRegular()
}

// unprivilegedUID returns a uid pkexec could plausibly have exported: nonzero
// and resolvable to an account. It returns an empty string when the host has
// neither, which only happens when the suite runs as root on an image with no
// unprivileged account at all.
func unprivilegedUID(t *testing.T) string {
	t.Helper()

	candidates := []int{os.Getuid()}
	for _, name := range []string{"nobody", "nfsnobody"} {
		if account, err := user.Lookup(name); err == nil {
			if uid, convErr := strconv.Atoi(account.Uid); convErr == nil {
				candidates = append(candidates, uid)
			}
		}
	}

	for _, uid := range candidates {
		if uid <= 0 {
			continue
		}
		if _, err := user.LookupId(strconv.Itoa(uid)); err != nil {
			continue
		}
		return strconv.Itoa(uid)
	}
	return ""
}

// stageInstall runs the repository's own `make install` into a temporary
// root, so every assertion here is made against the files the package ships
// rather than against a build-directory artifact.
func stageInstall(t *testing.T) string {
	t.Helper()

	root := repoRoot(t)
	buildDir := e2eBuildDir(t)
	stage := t.TempDir()

	cmd := exec.Command(
		"make",
		"install",
		"DESTDIR="+stage,
		"PREFIX=/usr",
		"BUILD_DIR="+buildDir,
	)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("staged make install failed: %v\noutput:\n%s", err, output)
	}
	return stage
}
