package ubluehelper

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fakeDockerHost records every operation EnableDocker and UpdateDocker
// perform, in order, so a test can assert the exact privileged sequence.
type fakeDockerHost struct {
	calls []string

	imageUnits     bool
	selinux        bool
	tools          bool // restorecon and semanage installed
	equivalence    bool // the Root equivalence rule already exists
	localGroup     bool // docker already in /etc/group
	imageGroup     bool // ensure copies docker from the image database
	installed      bool
	installedErr   error
	installChanged bool
	installErr     error
	unitsChanged   bool
	removed        bool
	failRun        string // argv (space-joined) whose run fails
	unitsErr       error  // CheckUnits refusal
	restartDoneErr error
	running        bool  // containers running on the live daemon
	runningErr     error // container probe failure
	probes         int   // ContainersRunning calls
}

func (f *fakeDockerHost) host() DockerHost {
	return DockerHost{
		ImageUnits:       f.imageUnits,
		SELinuxEnforcing: f.selinux,
		Restorecon:       f.tools,
		Semanage:         f.tools,
		HasEquivalence: func(context.Context) (bool, error) {
			f.calls = append(f.calls, "read equivalence")
			return f.equivalence, nil
		},
		Root:     "/var/lib/chairlift/docker",
		UnitDir:  "/etc/systemd/system",
		IndexURL: "https://formulae.brew.sh/api/formula.jws.json",
		Formulae: []string{"docker-engine", "containerd"},
		Run: func(_ context.Context, _ io.Writer, name string, args ...string) error {
			argv := strings.Join(append([]string{name}, args...), " ")
			f.calls = append(f.calls, "run "+argv)
			if argv == f.failRun {
				return errors.New("exit status 1")
			}
			if argv == "groupadd -f -r docker" {
				f.localGroup = true
			}
			return nil
		},
		EnsureGroup: func(group string, dryRun bool) error {
			f.calls = append(f.calls, "ensure "+group)
			if f.imageGroup && !dryRun {
				f.localGroup = true
			}
			return nil
		},
		GroupExists: func(string) bool { return f.localGroup },
		Install: func(context.Context) ([]byte, bool, error) {
			f.calls = append(f.calls, "install")
			if f.installErr != nil {
				return nil, false, f.installErr
			}
			return []byte(`{"Arch":"x86_64_linux"}`), f.installChanged, nil
		},
		RuntimeInstalled: func() (bool, error) {
			f.calls = append(f.calls, "read manifest")
			return f.installed, f.installedErr
		},
		WriteUnits: func() (bool, error) {
			f.calls = append(f.calls, "write units")
			return f.unitsChanged, nil
		},
		RemoveManagedUnits: func() (bool, error) {
			f.calls = append(f.calls, "remove managed units")
			return f.removed, nil
		},
		CheckUnits: func() error {
			f.calls = append(f.calls, "check units")
			return f.unitsErr
		},
		RestartDone: func() error {
			f.calls = append(f.calls, "restart done")
			return f.restartDoneErr
		},
		ContainersRunning: func(context.Context) (bool, error) {
			f.probes++
			return f.running, f.runningErr
		},
	}
}

func assertCalls(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestEnableDockerUsesTheImageUnitsWithoutInstalling(t *testing.T) {
	for _, removed := range []bool{false, true} {
		fake := &fakeDockerHost{imageUnits: true, removed: removed, localGroup: true}
		if err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard); err != nil {
			t.Fatal(err)
		}
		want := []string{"remove managed units"}
		if removed {
			// A unit ChairLift wrote before the image shipped Docker would
			// shadow the image's own; systemd must forget it.
			want = append(want, "run systemctl daemon-reload")
		}
		want = append(want,
			"ensure docker",
			"run usermod -aG docker dev",
			"run systemctl enable --now docker.socket docker.service")
		assertCalls(t, fake.calls, want)
	}
}

func TestEnableDockerInstallsAndActivatesOnAFreshHost(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeDockerHost{installChanged: true}
	if err := EnableDocker(context.Background(), fake.host(), "dev", false, &out); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, fake.calls, []string{
		"check units",
		"install",
		"ensure docker",
		"run groupadd -f -r docker",
		"write units",
		"run systemctl daemon-reload",
		"run systemctl try-restart docker.service",
		"restart done",
		"run usermod -aG docker dev",
		"run systemctl enable --now docker.socket",
		"run systemctl start docker.service",
	})
	if !strings.Contains(out.String(), DockerRuntimePrefix+`{"Arch":"x86_64_linux"}`+"\n") ||
		!strings.Contains(out.String(), "docker runtime changed=true\n") {
		t.Fatalf("output = %q, want the manifest line and changed=true", out.String())
	}
}

func TestEnableDockerCreatesTheGroupOnlyWhenNoDatabaseDefinesIt(t *testing.T) {
	tests := []struct {
		name               string
		local, image       bool
		wantEnsure, wantGA bool
	}{
		{name: "already local", local: true},
		{name: "copied from the image", image: true, wantEnsure: true},
		{name: "defined nowhere", wantEnsure: true, wantGA: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeDockerHost{localGroup: test.local, imageGroup: test.image}
			if err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(fake.calls, "\n")
			if got := strings.Contains(joined, "ensure docker"); got != test.wantEnsure {
				t.Errorf("ensure called = %t, want %t", got, test.wantEnsure)
			}
			if got := strings.Contains(joined, "groupadd"); got != test.wantGA {
				t.Errorf("groupadd run = %t, want %t", got, test.wantGA)
			}
		})
	}
}

func TestEnableDockerLabelsTheRuntimeUnderSELinuxBeforeTheRestart(t *testing.T) {
	const add = "run semanage fcontext -a -e /usr/libexec/docker /var/lib/chairlift/docker"
	const relabel = "run restorecon -RF /var/lib/chairlift/docker"
	for _, test := range []struct {
		name                 string
		selinux, equivalence bool
		want                 []string
	}{
		{name: "not enforcing", want: nil},
		{name: "rule missing", selinux: true, want: []string{"read equivalence", add, relabel}},
		{name: "rule present", selinux: true, equivalence: true, want: []string{"read equivalence", relabel}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeDockerHost{selinux: test.selinux, tools: true, equivalence: test.equivalence, localGroup: true, installChanged: true}
			if err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, call := range fake.calls {
				if call == "read equivalence" || strings.Contains(call, "semanage") || strings.Contains(call, "restorecon") {
					got = append(got, call)
				}
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("labelling = %v, want %v", got, test.want)
			}
			// The label must land before the daemon can start from the new files.
			if i := slices.Index(fake.calls, relabel); i >= 0 && fake.calls[i+1] != "run systemctl try-restart docker.service" {
				t.Fatalf("restorecon not followed by the restart: %v", fake.calls)
			}
		})
	}
}

func TestDockerRefusesAnEnforcingHostThatCannotLabelTheRuntime(t *testing.T) {
	fake := &fakeDockerHost{selinux: true, localGroup: true, installed: true}
	if err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard); err == nil {
		t.Fatal("enable accepted an unlabellable runtime")
	}
	assertCalls(t, fake.calls, nil) // Nothing downloaded or written.
	if err := UpdateDocker(context.Background(), fake.host(), false, io.Discard); err == nil {
		t.Fatal("update accepted an unlabellable runtime")
	}
	assertCalls(t, fake.calls, []string{"read manifest"})
}

func TestEnableDockerLeavesARunningDaemonAloneWhenTheRuntimeIsUnchanged(t *testing.T) {
	fake := &fakeDockerHost{localGroup: true}
	if err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.calls {
		if strings.Contains(call, "try-restart") {
			t.Fatalf("unchanged runtime restarted the daemon: %v", fake.calls)
		}
	}
}

func TestEnableDockerStopsAtTheFirstFailure(t *testing.T) {
	t.Run("install", func(t *testing.T) {
		fake := &fakeDockerHost{installErr: errors.New("signature mismatch")}
		err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "signature mismatch") {
			t.Fatalf("error = %v, want the install failure", err)
		}
		// A runtime that failed verification must not gain units, a group,
		// or a running daemon.
		assertCalls(t, fake.calls, []string{"check units", "install"})
	})
	t.Run("daemon-reload", func(t *testing.T) {
		fake := &fakeDockerHost{localGroup: true, failRun: "systemctl daemon-reload"}
		err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "systemctl daemon-reload") {
			t.Fatalf("error = %v, want the failing command named", err)
		}
		assertCalls(t, fake.calls, []string{"check units", "install", "write units", "run systemctl daemon-reload"})
	})
	t.Run("socket", func(t *testing.T) {
		fake := &fakeDockerHost{localGroup: true, failRun: "systemctl enable --now docker.socket"}
		if err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard); err == nil {
			t.Fatal("socket failure reported success")
		}
		if last := fake.calls[len(fake.calls)-1]; last != "run systemctl enable --now docker.socket" {
			t.Fatalf("continued after the socket failed: %v", fake.calls)
		}
	})
}

func TestEnableDockerRefusesAnEmptyAccount(t *testing.T) {
	fake := &fakeDockerHost{}
	if err := EnableDocker(context.Background(), fake.host(), "", false, io.Discard); err == nil {
		t.Fatal("empty username accepted")
	}
	assertCalls(t, fake.calls, nil)
}

func TestEnableDockerDryRunIsOfflineAndWritesNothing(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeDockerHost{selinux: true, tools: true}
	if err := EnableDocker(context.Background(), fake.host(), "dev", true, &out); err != nil {
		t.Fatal(err)
	}
	// Only the read-only unit check and the dry-run group check may run.
	assertCalls(t, fake.calls, []string{"check units", "ensure docker"})
	want := "[DRY-RUN] would fetch and verify https://formulae.brew.sh/api/formula.jws.json\n" +
		"[DRY-RUN] would install Homebrew formulae docker-engine containerd into /var/lib/chairlift/docker\n" +
		"[DRY-RUN] would execute: groupadd [-f -r docker]\n" +
		"[DRY-RUN] would write /etc/systemd/system/docker.service and /etc/systemd/system/docker.socket\n" +
		"[DRY-RUN] would execute: systemctl [daemon-reload]\n" +
		"[DRY-RUN] would execute if not yet defined: semanage [fcontext -a -e /usr/libexec/docker /var/lib/chairlift/docker]\n" +
		"[DRY-RUN] would execute: restorecon [-RF /var/lib/chairlift/docker]\n" +
		"[DRY-RUN] would execute if the runtime changed: systemctl [try-restart docker.service]\n" +
		"[DRY-RUN] would execute: usermod [-aG docker dev]\n" +
		"[DRY-RUN] would execute: systemctl [enable --now docker.socket]\n" +
		"[DRY-RUN] would execute: systemctl [start docker.service]\n"
	if out.String() != want {
		t.Fatalf("dry-run output =\n%s\nwant\n%s", out.String(), want)
	}
}

func TestEnableDockerDryRunOnTheImageUnitsRemovesNothing(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeDockerHost{imageUnits: true, localGroup: true}
	if err := EnableDocker(context.Background(), fake.host(), "dev", true, &out); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, fake.calls, []string{"ensure docker"})
	if !strings.Contains(out.String(), "[DRY-RUN] would execute: systemctl [enable --now docker.socket docker.service]\n") {
		t.Fatalf("dry-run output = %q", out.String())
	}
}

func TestUpdateDockerHasNothingToUpdateWithoutAHomebrewRuntime(t *testing.T) {
	for _, fake := range []*fakeDockerHost{{imageUnits: true, installed: true}, {}} {
		for _, dryRun := range []bool{false, true} {
			var out bytes.Buffer
			fake.calls = nil
			if err := UpdateDocker(context.Background(), fake.host(), dryRun, &out); err != nil {
				t.Fatal(err)
			}
			if out.String() != NoDockerRuntimeLine+"\n" {
				t.Fatalf("output = %q, want %q", out.String(), NoDockerRuntimeLine)
			}
			for _, call := range fake.calls {
				if call != "read manifest" {
					t.Fatalf("nothing to update, but ran %v", fake.calls)
				}
			}
		}
	}
}

func TestUpdateDockerReportsAnUnreadableManifest(t *testing.T) {
	fake := &fakeDockerHost{installedErr: errors.New("permission denied")}
	if err := UpdateDocker(context.Background(), fake.host(), false, io.Discard); err == nil {
		t.Fatal("unreadable manifest reported success")
	}
	assertCalls(t, fake.calls, []string{"read manifest"})
}

func TestUpdateDockerRestartsOnlyAChangedRuntime(t *testing.T) {
	tests := []struct {
		name                  string
		changed, unitsChanged bool
		selinux               bool
		wantRuns              []string
	}{
		{name: "current", wantRuns: nil},
		{name: "units repaired", unitsChanged: true, wantRuns: []string{"run systemctl daemon-reload"}},
		{name: "current under SELinux", selinux: true, wantRuns: nil},
		{name: "runtime changed", changed: true, wantRuns: []string{"run systemctl daemon-reload", "run systemctl try-restart docker.service", "restart done"}},
		{name: "runtime changed under SELinux", changed: true, selinux: true, wantRuns: []string{
			"run systemctl daemon-reload", "read equivalence", "run semanage fcontext -a -e /usr/libexec/docker /var/lib/chairlift/docker",
			"run restorecon -RF /var/lib/chairlift/docker", "run systemctl try-restart docker.service", "restart done"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			fake := &fakeDockerHost{installed: true, installChanged: test.changed, unitsChanged: test.unitsChanged, selinux: test.selinux, tools: test.selinux}
			if err := UpdateDocker(context.Background(), fake.host(), false, &out); err != nil {
				t.Fatal(err)
			}
			assertCalls(t, fake.calls, append([]string{"read manifest", "check units", "install", "write units"}, test.wantRuns...))
			if want := "docker runtime changed=" + map[bool]string{false: "false", true: "true"}[test.changed] + "\n"; !strings.Contains(out.String(), want) {
				t.Fatalf("output = %q, want %q", out.String(), want)
			}
		})
	}
}

func TestDockerDefersTheRestartWhileContainersMayBeRunning(t *testing.T) {
	tests := []struct {
		name       string
		running    bool
		runningErr error
		deferred   bool
	}{
		{name: "containers running", running: true, deferred: true},
		{name: "probe failed", runningErr: errors.New("timeout"), deferred: true},
		{name: "no containers", deferred: false},
	}
	for _, test := range tests {
		for _, command := range []string{"enable", "update"} {
			t.Run(test.name+"/"+command, func(t *testing.T) {
				var out bytes.Buffer
				fake := &fakeDockerHost{installed: true, localGroup: true, installChanged: true, running: test.running, runningErr: test.runningErr}
				var err error
				if command == "enable" {
					err = EnableDocker(context.Background(), fake.host(), "alice", false, &out)
				} else {
					err = UpdateDocker(context.Background(), fake.host(), false, &out)
				}
				if err != nil {
					t.Fatal(err)
				}
				if fake.probes != 1 {
					t.Fatalf("probes = %d, want 1", fake.probes)
				}
				restarted := slices.Contains(fake.calls, "run systemctl try-restart docker.service")
				done := slices.Contains(fake.calls, "restart done")
				said := strings.Contains(out.String(), DockerRestartDeferredLine+"\n")
				if restarted == test.deferred || done == test.deferred || said != test.deferred {
					t.Fatalf("restarted=%t done=%t deferred line=%t, want deferred=%t; calls %v", restarted, done, said, test.deferred, fake.calls)
				}
				if !slices.Contains(fake.calls, "run systemctl daemon-reload") {
					t.Fatalf("daemon-reload skipped: %v", fake.calls)
				}
			})
		}
	}
}

func TestDockerDoesNotProbeContainersForAnUnchangedRuntime(t *testing.T) {
	fake := &fakeDockerHost{installed: true, localGroup: true, running: true}
	if err := EnableDocker(context.Background(), fake.host(), "alice", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := UpdateDocker(context.Background(), fake.host(), false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := UpdateDocker(context.Background(), fake.host(), true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if fake.probes != 0 {
		t.Fatalf("probes = %d, want 0", fake.probes)
	}
}

func TestUpdateDockerDryRunIsOffline(t *testing.T) {
	var out bytes.Buffer
	fake := &fakeDockerHost{installed: true}
	if err := UpdateDocker(context.Background(), fake.host(), true, &out); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, fake.calls, []string{"read manifest", "check units"})
	if !strings.Contains(out.String(), "[DRY-RUN] would fetch and verify ") ||
		!strings.Contains(out.String(), "[DRY-RUN] would execute if the runtime changed: systemctl [try-restart docker.service]\n") {
		t.Fatalf("dry-run output = %q", out.String())
	}
}

func TestDockerTimeoutsCoverTheDownload(t *testing.T) {
	for _, command := range []string{CommandDockerEnable, CommandDockerUpdate} {
		if got := Timeout(command); got != ImageSwitchTimeout {
			t.Errorf("Timeout(%s) = %v, want %v", command, got, ImageSwitchTimeout)
		}
	}
	// Disabling only stops units; it keeps the default budget.
	if got := Timeout(CommandDockerDisable); got != DefaultTimeout {
		t.Errorf("Timeout(docker-disable) = %v, want %v", got, DefaultTimeout)
	}
	if _, ok := DockerArgs(CommandDockerUpdate); ok {
		t.Error("docker-update must not map to a bare systemctl lifecycle argv")
	}
	if _, ok := AccessGroup(CommandDockerUpdate); ok {
		t.Error("docker-update must not grant any group")
	}
}

// An administrator's unit is refused before anything is downloaded, in every
// path that would otherwise install.
func TestDockerRefusesUnmanagedUnitsBeforeInstalling(t *testing.T) {
	refusal := errors.New("unmanaged unit")
	for _, dryRun := range []bool{false, true} {
		fake := &fakeDockerHost{unitsErr: refusal}
		if err := EnableDocker(context.Background(), fake.host(), "dev", dryRun, io.Discard); !errors.Is(err, refusal) {
			t.Fatalf("enable dryRun=%t: err = %v", dryRun, err)
		}
		assertCalls(t, fake.calls, []string{"check units"})

		fake = &fakeDockerHost{installed: true, unitsErr: refusal}
		if err := UpdateDocker(context.Background(), fake.host(), dryRun, io.Discard); !errors.Is(err, refusal) {
			t.Fatalf("update dryRun=%t: err = %v", dryRun, err)
		}
		assertCalls(t, fake.calls, []string{"read manifest", "check units"})
	}
}

// The restart-pending marker is cleared only after the restart succeeds, so
// a failed restart is retried by the next run.
func TestDockerRestartDoneFollowsOnlyASuccessfulRestart(t *testing.T) {
	restart := "systemctl try-restart docker.service"
	fake := &fakeDockerHost{localGroup: true, installChanged: true, failRun: restart}
	if err := EnableDocker(context.Background(), fake.host(), "dev", false, io.Discard); err == nil {
		t.Fatal("failed restart reported success")
	}
	fake2 := &fakeDockerHost{installed: true, installChanged: true, failRun: restart}
	if err := UpdateDocker(context.Background(), fake2.host(), false, io.Discard); err == nil {
		t.Fatal("failed restart reported success")
	}
	for _, calls := range [][]string{fake.calls, fake2.calls} {
		if slices.Contains(calls, "restart done") {
			t.Fatalf("marker cleared after a failed restart: %v", calls)
		}
	}
	fake3 := &fakeDockerHost{installed: true, installChanged: true, restartDoneErr: errors.New("read-only")}
	if err := UpdateDocker(context.Background(), fake3.host(), false, io.Discard); err == nil {
		t.Fatal("unclearable marker reported success")
	}
}
