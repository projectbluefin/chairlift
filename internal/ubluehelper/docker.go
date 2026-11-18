package ubluehelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// DockerGroup is the group whose members may use the system Docker socket.
const DockerGroup = "docker"

// NoDockerRuntimeLine is what docker-update prints when this host has no
// Homebrew Docker runtime for the helper to update: the image ships Docker
// itself, or docker-enable never installed one.
const NoDockerRuntimeLine = "no Homebrew Docker runtime to update"

// DockerRuntimePrefix starts the line carrying the installed runtime's
// manifest JSON after docker-enable or docker-update installed it.
const DockerRuntimePrefix = "chairlift-helper: docker runtime "

// DockerRestartDeferredLine is printed when a changed runtime is installed
// but the daemon is not restarted because containers are running (or that
// could not be ruled out). The restart-pending marker stays, so the next
// docker-enable or docker-update retries, and a reboot starts the new runtime.
const DockerRestartDeferredLine = "docker runtime restart deferred: containers are running"

// DockerHost is everything docker-enable and docker-update consult or change
// besides the fixed argv this package spells. cmd/chairlift-helper wires the
// fields to internal/dockerbrew and the real host; tests substitute fakes.
// The paths are reported only in dry-run lines: no caller-supplied value
// reaches them, so nothing in this struct crosses the pkexec boundary.
type DockerHost struct {
	// ImageUnits reports that the booted image ships docker.service itself
	// (Bluefin DX). Then the image's Docker is enabled and nothing is
	// installed from Homebrew.
	ImageUnits bool
	// SELinuxEnforcing means the installed runtime must be labelled before
	// systemd can execute it. container-selinux labels /usr/libexec/docker
	// container_runtime_exec_t but never names Root, so a local equivalence
	// rule (semanage) maps Root onto it before restorecon applies the label.
	// Restorecon and Semanage report whether those tools are installed; an
	// enforcing host without both is refused before anything is downloaded.
	SELinuxEnforcing bool
	Restorecon       bool
	Semanage         bool
	// HasEquivalence reports whether the Root equivalence rule is already
	// defined; it only reads. It needs a policy store semanage can open and
	// returns ErrSELinuxStoreUnmanaged when the store cannot be opened.
	HasEquivalence func(ctx context.Context) (bool, error)
	// LabelsResolve reports whether the loaded file contexts already give
	// Root the labels SELinuxSource has (matchpathcon, which reads the
	// compiled contexts and needs no policy store). When they do, the
	// equivalence rule would change nothing and is not added. Nil, or an
	// error, falls back to HasEquivalence.
	LabelsResolve func(ctx context.Context) (bool, error)

	// Root is the root-owned install root, UnitDir where managed units are
	// written, IndexURL the signed index fetched, and Formulae the daemon
	// formula set. They appear only in dry-run output.
	Root     string
	UnitDir  string
	IndexURL string
	Formulae []string

	Run         Runner
	EnsureGroup GroupEnsurer
	// GroupExists reports whether the local group database defines group.
	GroupExists func(group string) bool
	// Install verifies and activates only after calling admit under its lock.
	Install      func(ctx context.Context, admit func(context.Context, string) error) (manifest []byte, changed bool, err error)
	RuntimeProbe DockerRuntimeProbe
	// RuntimeInstalled reports whether a runtime manifest exists. A missing
	// manifest is (false, nil); any other read failure is an error.
	RuntimeInstalled func() (bool, error)
	// WriteUnits writes the managed docker.service and docker.socket,
	// reporting whether either changed on disk.
	WriteUnits func() (bool, error)
	// RemoveManagedUnits deletes managed units that would shadow the image's
	// own, reporting whether any was removed.
	RemoveManagedUnits func() (bool, error)
	// CheckUnits refuses unmanaged docker.service/docker.socket files before
	// anything is downloaded; it only reads.
	CheckUnits func() error
	// RestartDone clears the install's restart-pending marker once the
	// daemon has been restarted onto a changed runtime. It clears only the
	// marker for the set this run's Install made current, under the install
	// lock, so a concurrent run's newer swap keeps its pending restart.
	RestartDone func(ctx context.Context) error
	// ContainersRunning reports whether the running daemon has any running
	// container. No daemon serving is (false, nil). A true result or an
	// error defers the restart onto a changed runtime.
	ContainersRunning func(ctx context.Context) (bool, error)
}

// DockerEnableAccessArgs returns the usermod argv that grants username the
// Docker socket. The account comes from PKEXEC_UID, never argv.
func DockerEnableAccessArgs(username string) (string, []string, bool) {
	return AccessArgs(CommandDockerEnable, username)
}

// GroupAddArgs creates the docker group as a system group. -f makes an
// existing group a success, so a group created between the check and the
// command is not a failure.
func GroupAddArgs() []string {
	return []string{"-f", "-r", DockerGroup}
}

// RestoreconArgs relabels the install root recursively, forcing the
// policy's labels over whatever the extraction inherited.
func RestoreconArgs(root string) []string {
	return []string{"-RF", root}
}

// SELinuxSource is the path whose container-selinux file contexts Root
// receives through the equivalence rule.
const SELinuxSource = "/usr/libexec/docker"

// SemanageArgs defines Root as equivalent to SELinuxSource.
func SemanageArgs(root string) []string {
	return []string{"fcontext", "-a", "-e", SELinuxSource, root}
}

// labelProbes are paths under Root, and under SELinuxSource, whose labels
// stand for the whole runtime: the install root itself, a daemon binary, and
// a bundled library inside a set directory (a set id is 16 hex digits).
var labelProbes = []string{"", "/0123456789abcdef/bin/dockerd", "/0123456789abcdef/lib/libseccomp.so.2"}

// EquivalentLabels reports whether lookup, the default file context the
// loaded policy assigns a path, already labels each probe under root exactly
// as under SELinuxSource, which is all the equivalence rule would achieve.
func EquivalentLabels(root string, lookup func(path string) (string, error)) (bool, error) {
	for _, probe := range labelProbes {
		got, err := lookup(root + probe)
		if err != nil {
			return false, err
		}
		want, err := lookup(SELinuxSource + probe)
		if err != nil {
			return false, err
		}
		if got != want {
			return false, nil
		}
	}
	return true, nil
}

// DockerSELinuxStorePrefix survives the helper's process boundary so the GUI
// can say why Docker cannot be turned on instead of offering an ineffective
// Retry.
const DockerSELinuxStorePrefix = "docker runtime cannot be labelled:"

// ErrSELinuxStoreUnmanaged means semanage cannot open the SELinux policy
// store, so the equivalence rule cannot be added: for example an image whose
// semanage.conf store-root is on the read-only /usr. Retrying cannot help;
// the image has to make its store manageable or ship the rule itself.
var ErrSELinuxStoreUnmanaged = errors.New(DockerSELinuxStorePrefix + " semanage cannot open this image's SELinux policy store, so the file-context rule Docker needs cannot be added")

// semanageUnmanagedStore is what semanage prints when it cannot open the
// policy store. The helper runs semanage as root, so it means the store is
// missing, unreadable, or not writable, not that the caller lacks privilege.
const semanageUnmanagedStore = "SELinux policy is not managed or store cannot be accessed"

// SemanageStoreUnmanaged reports whether semanage's stderr is its refusal to
// open the policy store.
func SemanageStoreUnmanaged(stderr []byte) bool {
	return strings.Contains(string(stderr), semanageUnmanagedStore)
}

// DefaultSemanageStoreRoot is libsemanage's store-root when semanage.conf
// sets none.
const DefaultSemanageStoreRoot = "/var/lib/selinux"

// SemanageStoreRoot returns the store-root semanage.conf configures, or
// DefaultSemanageStoreRoot. Only the top-level key is read; sections such
// as [sefcontext_compile] never set it.
func SemanageStoreRoot(conf []byte) string {
	root := DefaultSemanageStoreRoot
	for line := range strings.SplitSeq(string(conf), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && strings.TrimSpace(key) == "store-root" {
			if value = strings.TrimSpace(value); value != "" {
				root = value
			}
		}
	}
	return root
}

// relabelReady refuses an enforcing host that cannot label the runtime:
// installing it anyway would leave a daemon systemd cannot execute.
func (h DockerHost) relabelReady() error {
	if h.SELinuxEnforcing && (!h.Restorecon || !h.Semanage) {
		return errors.New("SELinux is enforcing, but restorecon or semanage is not installed, so the Docker runtime cannot be labelled to run")
	}
	return nil
}

// needsEquivalence reports whether the equivalence rule must be added. It
// only reads. Labels the loaded file contexts already resolve need no rule;
// otherwise the store is asked, and a store semanage cannot open is refused
// with ErrSELinuxStoreUnmanaged, since the runtime could not be labelled.
func (h DockerHost) needsEquivalence(steps dockerSteps) (bool, error) {
	if h.LabelsResolve != nil {
		if resolved, err := h.LabelsResolve(steps.ctx); err == nil && resolved {
			steps.say("SELinux file contexts already label %s like %s; no equivalence rule needed\n", h.Root, SELinuxSource)
			return false, nil
		}
	}
	defined, err := h.HasEquivalence(steps.ctx)
	if errors.Is(err, ErrSELinuxStoreUnmanaged) {
		return false, err
	}
	if err != nil {
		return false, fmt.Errorf("reading SELinux file contexts: %w", err)
	}
	return !defined, nil
}

// relabel defines the equivalence rule when addRule, then applies it. A dry
// run previews both; ifChanged previews them as conditional on the install.
func (h DockerHost) relabel(steps dockerSteps, ifChanged, addRule bool) error {
	if !h.SELinuxEnforcing {
		return nil
	}
	if steps.dryRun {
		steps.say("[DRY-RUN] would execute if not yet defined: semanage %v\n", SemanageArgs(h.Root))
	} else if addRule {
		if err := steps.exec("semanage", SemanageArgs(h.Root)...); err != nil {
			return fmt.Errorf("labelling Docker runtime: %w", err)
		}
	}
	run := steps.exec
	if ifChanged {
		run = steps.execIfChanged
	}
	if err := run("restorecon", RestoreconArgs(h.Root)...); err != nil {
		return fmt.Errorf("relabelling Docker runtime: %w", err)
	}
	return nil
}

// EnableDocker turns on rootful system Docker for username.
//
// When the image ships docker.service, any unit ChairLift wrote earlier is
// removed so it cannot shadow the image's, and the image's Docker is
// enabled. Otherwise the daemon runtime is installed from verified Homebrew
// bottles into the root-owned install root, the docker group is created if
// neither /etc/group nor the image defines it, the managed units are written,
// and socket activation is enabled. A changed runtime restarts an already
// running daemon with `systemctl try-restart`, which does nothing when it is
// not running — but only when no container is running; otherwise the
// restart is deferred to the next update or a reboot.
//
// Unmanaged units are refused before anything is downloaded. A completed
// restart clears the install's restart-pending marker, so a run interrupted
// before it reports the runtime changed again next time.
//
// A dry run reads only local state, fetches nothing, writes nothing, and
// prints every step it would take.
func EnableDocker(ctx context.Context, h DockerHost, username string, dryRun bool, stdout io.Writer) error {
	name, accessArgs, ok := DockerEnableAccessArgs(username)
	if !ok {
		return errors.New("unsupported Docker access request")
	}
	steps := dockerSteps{ctx: ctx, run: h.Run, dryRun: dryRun, stdout: stdout}

	if h.ImageUnits {
		if dryRun {
			steps.say("[DRY-RUN] would remove managed docker.service and docker.socket from %s\n", h.UnitDir)
		} else {
			removed, err := h.RemoveManagedUnits()
			if err != nil {
				return fmt.Errorf("removing managed Docker units: %w", err)
			}
			if removed {
				if err := steps.exec("systemctl", "daemon-reload"); err != nil {
					return err
				}
			}
		}
		if err := h.EnsureGroup(DockerGroup, dryRun); err != nil {
			return fmt.Errorf("granting Docker access: %w", err)
		}
		if err := steps.exec(name, accessArgs...); err != nil {
			return fmt.Errorf("granting Docker access: %w", err)
		}
		args, _ := DockerArgs(CommandDockerEnable)
		if err := steps.exec("systemctl", args...); err != nil {
			return fmt.Errorf("changing Docker daemon: %w", err)
		}
		return nil
	}

	if err := h.relabelReady(); err != nil {
		return err
	}
	// Decided before anything is downloaded: a host whose store cannot take
	// the rule is refused before it gains a runtime it cannot execute.
	addRule := false
	if h.SELinuxEnforcing && !dryRun {
		var err error
		if addRule, err = h.needsEquivalence(steps); err != nil {
			return err
		}
	}
	if err := h.CheckUnits(); err != nil {
		return fmt.Errorf("writing Docker units: %w", err)
	}
	prepared := false
	if !dryRun {
		installed, err := h.RuntimeInstalled()
		if err != nil {
			return fmt.Errorf("reading Docker runtime: %w", err)
		}
		if installed {
			if err := h.prepareRuntime(steps); err != nil {
				return err
			}
			prepared = true
		}
	}
	changed := true
	if dryRun {
		steps.sayInstall(h)
	} else {
		manifest, installChanged, err := h.install(ctx)
		if err != nil {
			return fmt.Errorf("installing Docker runtime: %w", err)
		}
		changed = installChanged
		steps.sayManifest(manifest, changed)
	}

	if !h.GroupExists(DockerGroup) {
		if err := h.EnsureGroup(DockerGroup, dryRun); err != nil {
			return fmt.Errorf("preparing Docker group: %w", err)
		}
		// In a dry run the copy above did not happen, so the group still
		// reads as missing; -f keeps the previewed groupadd harmless either way.
		if dryRun || !h.GroupExists(DockerGroup) {
			if err := steps.exec("groupadd", GroupAddArgs()...); err != nil {
				return fmt.Errorf("creating Docker group: %w", err)
			}
		}
	}

	if !prepared {
		if dryRun {
			steps.say("[DRY-RUN] would write %s/docker.service and %s/docker.socket\n", h.UnitDir, h.UnitDir)
		} else if _, err := h.WriteUnits(); err != nil {
			return fmt.Errorf("writing Docker units: %w", err)
		}
		if err := steps.exec("systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if dryRun {
		steps.say("[DRY-RUN] would execute if Docker failed: systemctl [reset-failed docker.service docker.socket]\n")
	}
	if err := h.relabel(steps, false, addRule); err != nil {
		return err
	}
	if changed {
		if dryRun {
			if err := steps.execIfChanged("systemctl", "try-restart", "docker.service"); err != nil {
				return err
			}
		} else if err := restartRuntime(ctx, h, steps); err != nil {
			return err
		}
	}
	if err := steps.exec(name, accessArgs...); err != nil {
		return fmt.Errorf("granting Docker access: %w", err)
	}
	if err := steps.exec("systemctl", "enable", "--now", "docker.socket"); err != nil {
		return fmt.Errorf("changing Docker daemon: %w", err)
	}
	if err := steps.exec("systemctl", "start", "docker.service"); err != nil {
		return fmt.Errorf("changing Docker daemon: %w", err)
	}
	return nil
}

// UpdateDocker replaces the Homebrew Docker runtime with the latest verified
// bottles. A host whose image ships Docker, or that has no installed runtime,
// has nothing to update and succeeds after saying so. A changed runtime
// restarts a running daemon; an unchanged one touches nothing but the units,
// which are rewritten so a damaged or outdated managed unit is repaired.
//
// A dry run reads only local state and fetches nothing.
func UpdateDocker(ctx context.Context, h DockerHost, dryRun bool, stdout io.Writer) error {
	steps := dockerSteps{ctx: ctx, run: h.Run, dryRun: dryRun, stdout: stdout}
	if h.ImageUnits {
		steps.say("%s\n", NoDockerRuntimeLine)
		return nil
	}
	installed, err := h.RuntimeInstalled()
	if err != nil {
		return fmt.Errorf("reading Docker runtime: %w", err)
	}
	if !installed {
		steps.say("%s\n", NoDockerRuntimeLine)
		return nil
	}
	if err := h.relabelReady(); err != nil {
		return err
	}
	if err := h.CheckUnits(); err != nil {
		return fmt.Errorf("writing Docker units: %w", err)
	}

	if dryRun {
		steps.sayInstall(h)
		steps.say("[DRY-RUN] would write %s/docker.service and %s/docker.socket\n", h.UnitDir, h.UnitDir)
		if err := steps.execIfChanged("systemctl", "daemon-reload"); err != nil {
			return err
		}
		steps.say("[DRY-RUN] would execute if Docker failed: systemctl [reset-failed docker.service docker.socket]\n")
		if err := h.relabel(steps, true, false); err != nil {
			return err
		}
		return steps.execIfChanged("systemctl", "try-restart", "docker.service")
	}

	if err := h.prepareRuntime(steps); err != nil {
		return err
	}
	manifest, changed, err := h.install(ctx)
	if err != nil {
		return fmt.Errorf("installing Docker runtime: %w", err)
	}
	steps.sayManifest(manifest, changed)
	if changed {
		// The new set inherited its parent's label; apply the policy's
		// before the daemon restarts onto it. An unchanged runtime reads
		// nothing, so an update that installs nothing still succeeds.
		addRule := false
		if h.SELinuxEnforcing {
			if addRule, err = h.needsEquivalence(steps); err != nil {
				return err
			}
		}
		if err := h.relabel(steps, false, addRule); err != nil {
			return err
		}
		if err := restartRuntime(ctx, h, steps); err != nil {
			return err
		}
	}
	return nil
}

// prepareRuntime loads the locked-start unit before Install can change an
// existing runtime. NeedDaemonReload retries a previous failed reload even
// when the file on disk already matches; failed starts clear only Docker's
// rate limits so a repaired daemon can be retried immediately.
func (h DockerHost) prepareRuntime(steps dockerSteps) error {
	changed, err := h.WriteUnits()
	if err != nil {
		return fmt.Errorf("writing Docker units: %w", err)
	}
	state, err := h.RuntimeProbe.Service(steps.ctx)
	if err != nil {
		return fmt.Errorf("reading Docker service: %w", err)
	}
	if changed || state.NeedDaemonReload != "no" {
		if err := steps.exec("systemctl", "daemon-reload"); err != nil {
			return err
		}
	}
	if state.ActiveState == "failed" {
		return steps.exec("systemctl", "reset-failed", "docker.service", "docker.socket")
	}
	return nil
}

func (h DockerHost) install(ctx context.Context) ([]byte, bool, error) {
	return h.Install(ctx, func(ctx context.Context, active string) error {
		return CheckDockerRuntime(ctx, h.Root, active, h.RuntimeProbe)
	})
}

// restartRuntime restarts the daemon onto a changed runtime and clears the
// restart-pending marker, unless containers are running or the probe fails:
// a restart without live-restore would stop every container.
func restartRuntime(ctx context.Context, h DockerHost, steps dockerSteps) error {
	running, err := h.ContainersRunning(ctx)
	if running || err != nil {
		steps.say("%s\n", DockerRestartDeferredLine)
		return nil
	}
	if err := steps.exec("systemctl", "try-restart", "docker.service"); err != nil {
		return fmt.Errorf("restarting Docker daemon: %w", err)
	}
	if err := h.RestartDone(ctx); err != nil {
		return fmt.Errorf("recording Docker restart: %w", err)
	}
	return nil
}

// dockerSteps runs or previews each fixed argv in order, so the live and
// dry-run paths cannot drift apart.
type dockerSteps struct {
	ctx    context.Context
	run    Runner
	dryRun bool
	stdout io.Writer
}

func (s dockerSteps) say(format string, args ...any) {
	_, _ = fmt.Fprintf(s.stdout, format, args...)
}

func (s dockerSteps) exec(name string, args ...string) error {
	if s.dryRun {
		s.say("[DRY-RUN] would execute: %s %v\n", name, args)
		return nil
	}
	if err := s.run(s.ctx, nil, name, args...); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// execIfChanged previews a step that runs only when the install changes the
// runtime, which a dry run cannot know without fetching.
func (s dockerSteps) execIfChanged(name string, args ...string) error {
	if s.dryRun {
		s.say("[DRY-RUN] would execute if the runtime changed: %s %v\n", name, args)
		return nil
	}
	return s.exec(name, args...)
}

func (s dockerSteps) sayInstall(h DockerHost) {
	s.say("[DRY-RUN] would fetch and verify %s\n", h.IndexURL)
	s.say("[DRY-RUN] would install Homebrew formulae %s into %s\n", strings.Join(h.Formulae, " "), h.Root)
}

func (s dockerSteps) sayManifest(manifest []byte, changed bool) {
	s.say("%s%s\n", DockerRuntimePrefix, manifest)
	s.say("docker runtime changed=%t\n", changed)
}
