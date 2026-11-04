package dockerbrew

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// installEnv is a loopback Homebrew serving one signed bottle set, with the
// install seams pointed at the test.
type installEnv struct {
	t      *testing.T
	reg    *registry
	client Client
	arch   string
	root   string
	smokes int
	// smokeErr, when set, fails the staged smoke test.
	smokeErr error
}

func newInstallEnv(t *testing.T) *installEnv {
	t.Helper()
	arch, err := Arch()
	if err != nil {
		t.Skip(err)
	}
	useTestKey(t)
	reg, client := newRegistry(t)
	e := &installEnv{t: t, reg: reg, client: client, arch: arch, root: filepath.Join(t.TempDir(), "docker")}

	oldOwner, oldSmoke, oldPoll := ownerUID, smokeTest, lockPoll
	ownerUID = uint32(os.Getuid())
	lockPoll = 10 * time.Millisecond
	smokeTest = func(_ context.Context, dir string) error {
		e.smokes++
		// The smoke test must see the set already patched.
		facts := readELFFacts(t, filepath.Join(dir, "bin/runc"))
		if facts.interp != Loader(arch) || !slices.Equal(facts.rpath, []string{originLib}) {
			t.Errorf("smoke test ran on an unpatched runc: %+v", facts)
		}
		return e.smokeErr
	}
	t.Cleanup(func() { ownerUID, smokeTest, lockPoll = oldOwner, oldSmoke, oldPoll })
	return e
}

// publish serves a bottle set whose contents carry tag and signs an index
// naming it.
func (e *installEnv) publish(tag string) []Bottle {
	e.t.Helper()
	fs := indexFormulae()
	for _, name := range Formulae() {
		f := find(fs, name)
		sha := e.reg.addBlob(name, bottleTgz(e.t, name, fixtureVersions[name], e.arch, tag))
		setFile(f, e.arch, bottleURL(name, sha), sha)
	}
	payload := marshalPayload(e.t, fs)
	e.reg.mu.Lock()
	e.reg.index = signJWS(e.t, signingKey(e.t), string(payload), jwsOpts{pad: true})
	e.reg.mu.Unlock()
	bottles, err := SelectBottles(payload, e.arch)
	if err != nil {
		e.t.Fatal(err)
	}
	return bottles
}

func (e *installEnv) currentTarget() string {
	e.t.Helper()
	target, err := os.Readlink(filepath.Join(e.root, currentName))
	if err != nil {
		e.t.Fatal(err)
	}
	return target
}

func (e *installEnv) entries() []string {
	e.t.Helper()
	des, err := os.ReadDir(e.root)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, d := range des {
		out = append(out, d.Name())
	}
	return out
}

func sortedNames(names ...string) []string {
	slices.Sort(names)
	return names
}

func TestRuntimeInstallBuildsPatchedSetAndIsIdempotent(t *testing.T) {
	e := newInstallEnv(t)
	bottles := e.publish("one")

	m, changed, err := Install(t.Context(), e.client, e.root)
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v; want changed", changed, err)
	}
	if !slices.Equal(m.Bottles, bottles) || m.Arch != e.arch {
		t.Fatalf("manifest = %+v", m)
	}
	if e.currentTarget() != m.SetID() {
		t.Fatalf("current -> %q, want %q", e.currentTarget(), m.SetID())
	}
	read, err := ReadManifest(e.root)
	if err != nil || read.SetID() != m.SetID() || !slices.Equal(read.Bottles, m.Bottles) {
		t.Fatalf("ReadManifest = %+v, %v", read, err)
	}
	cur := filepath.Join(e.root, currentName)
	for _, f := range []string{"bin/dockerd", "bin/docker-proxy", "bin/runc", "lib/libpathrs.so.0", "lib/libseccomp.so.2"} {
		facts := readELFFacts(t, filepath.Join(cur, f))
		if len(facts.rpath) != 1 || facts.rpath[0] != originLib {
			t.Errorf("%s rpath = %q", f, facts.rpath)
		}
		if strings.HasPrefix(f, "bin/") && facts.interp != Loader(e.arch) {
			t.Errorf("%s interpreter = %q", f, facts.interp)
		}
	}
	for _, f := range []string{"bin/containerd", "bin/containerd-shim-runc-v2", "bin/docker-init"} {
		if _, err := os.Stat(filepath.Join(cur, f)); err != nil {
			t.Error(err)
		}
	}
	if fi, err := os.Stat(filepath.Join(cur, manifestName)); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("manifest mode: %v, %v", fi, err)
	}
	if got := e.entries(); !slices.Equal(got, sortedNames(lockName, restartName, m.SetID(), currentName)) {
		t.Errorf("root holds %v", got)
	}
	if err := ClearRestartPending(t.Context(), e.root, m.SetID()); err != nil {
		t.Fatal(err)
	}

	blobsBefore := e.reg.count("/v2/")
	m2, changed, err := Install(t.Context(), e.client, e.root)
	if err != nil || changed {
		t.Fatalf("second Install = %v, %v; want unchanged", changed, err)
	}
	if m2.SetID() != m.SetID() || e.smokes != 1 || e.reg.count("/v2/") != blobsBefore {
		t.Errorf("unchanged install re-downloaded or re-smoked: smokes=%d blobs %d->%d", e.smokes, blobsBefore, e.reg.count("/v2/"))
	}
}

func TestRuntimeInstallSwapsToANewSetAndPrunesTheOld(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	first, _, err := Install(t.Context(), e.client, e.root)
	if err != nil {
		t.Fatal(err)
	}
	// Leftovers from an interrupted run, and a file that is not ours to prune.
	if err := os.Mkdir(filepath.Join(e.root, stagingPre+"123"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, downloadPre+"9"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, "README"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	e.publish("two")
	second, changed, err := Install(t.Context(), e.client, e.root)
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v", changed, err)
	}
	if second.SetID() == first.SetID() {
		t.Fatal("new bottles produced the same set id")
	}
	if e.currentTarget() != second.SetID() {
		t.Fatalf("current -> %q, want %q", e.currentTarget(), second.SetID())
	}
	want := sortedNames(lockName, restartName, second.SetID(), "README", currentName)
	if got := e.entries(); !slices.Equal(got, want) {
		t.Errorf("root holds %v, want %v", got, want)
	}
	if !Outdated(first, second.Bottles) || Outdated(second, second.Bottles) {
		t.Error("Outdated disagrees with the installed sets")
	}
}

func TestRuntimeInstallKeepsTheCurrentSetWhenANewOneFails(t *testing.T) {
	for name, breakIt := range map[string]func(e *installEnv){
		"smoke test fails": func(e *installEnv) { e.smokeErr = errors.New("dockerd --version: exit 127") },
		"bottle substituted": func(e *installEnv) {
			bottles := e.publish("two")
			e.reg.serve("runc", bottles[2].SHA256, bottleTgz(e.t, "runc", "1.5.2", e.arch, "evil"))
		},
		"index signature invalid": func(e *installEnv) {
			e.publish("two")
			e.reg.mu.Lock()
			e.reg.index = []byte(strings.Replace(string(e.reg.index), "0.19.0", "0.19.1", 1))
			e.reg.mu.Unlock()
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newInstallEnv(t)
			e.publish("one")
			first, _, err := Install(t.Context(), e.client, e.root)
			if err != nil {
				t.Fatal(err)
			}
			if err := ClearRestartPending(t.Context(), e.root, first.SetID()); err != nil {
				t.Fatal(err)
			}
			e.publish("two")
			breakIt(e)
			if _, _, err := Install(t.Context(), e.client, e.root); err == nil {
				t.Fatal("Install succeeded")
			}
			if e.currentTarget() != first.SetID() {
				t.Errorf("current moved to %q", e.currentTarget())
			}
			if got := e.entries(); !slices.Equal(got, sortedNames(lockName, first.SetID(), currentName)) {
				t.Errorf("failed install left %v", got)
			}
		})
	}
}

func TestRuntimeInstallRefusesAnUntrustworthyRoot(t *testing.T) {
	for name, setup := range map[string]func(e *installEnv){
		"root is a symlink": func(e *installEnv) {
			real := filepath.Join(filepath.Dir(e.root), "elsewhere")
			if err := os.Mkdir(real, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, e.root); err != nil {
				t.Fatal(err)
			}
		},
		"root owned by someone else": func(e *installEnv) {
			if err := os.Mkdir(e.root, 0o755); err != nil {
				t.Fatal(err)
			}
			ownerUID++
		},
		"parent writable by others": func(e *installEnv) {
			// Whoever can write the parent can rename root out from under it.
			if err := os.Chmod(filepath.Dir(e.root), 0o777); err != nil {
				t.Fatal(err)
			}
		},
		"root group-writable": func(e *installEnv) {
			if err := os.Mkdir(e.root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(e.root, 0o775); err != nil {
				t.Fatal(err)
			}
		},
		"current points outside root": func(e *installEnv) {
			if err := os.Mkdir(e.root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/usr/bin", filepath.Join(e.root, currentName)); err != nil {
				t.Fatal(err)
			}
		},
		"current climbs out of root": func(e *installEnv) {
			// The target exists and is ours, so only the name check refuses it.
			if err := os.Mkdir(e.root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(filepath.Dir(e.root), "0123456789abcdef"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../0123456789abcdef", filepath.Join(e.root, currentName)); err != nil {
				t.Fatal(err)
			}
		},
		"current is a directory": func(e *installEnv) {
			if err := os.MkdirAll(filepath.Join(e.root, currentName, "bin"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"current set is a symlink": func(e *installEnv) {
			if err := os.Mkdir(e.root, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), filepath.Join(e.root, "0123456789abcdef")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("0123456789abcdef", filepath.Join(e.root, currentName)); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newInstallEnv(t)
			e.publish("one")
			setup(e)
			if _, _, err := Install(t.Context(), e.client, e.root); err == nil {
				t.Fatal("Install accepted the root")
			}
			if n := e.reg.count(""); n != 0 {
				t.Errorf("made %d requests before refusing", n)
			}
		})
	}
}

func TestRuntimeInstallCreatesAMissingRootAndParent(t *testing.T) {
	e := newInstallEnv(t)
	e.root = filepath.Join(filepath.Dir(e.root), "chairlift", "docker")
	e.publish("one")
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	if _, _, err := Install(t.Context(), e.client, e.root); err != nil {
		t.Fatal(err)
	}
	// The parent stays traversable so the unprivileged GUI can read the manifest.
	for _, p := range []string{filepath.Dir(e.root), e.root, filepath.Join(e.root, e.currentTarget()), filepath.Join(e.root, currentName, "bin")} {
		fi, err := os.Stat(p)
		if err != nil || fi.Mode().Perm() != 0o755 {
			t.Errorf("%s: %v %v; want 0755 regardless of umask", p, fi, err)
		}
	}
}

func TestRuntimeInstallWaitsForTheLock(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	if err := os.Mkdir(e.root, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(e.root, lockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := Install(ctx, e.client, e.root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Install under a held lock: err = %v", err)
	}
	if e.reg.count("") != 0 {
		t.Error("Install fetched while another install held the lock")
	}
}

func TestReadManifestReportsNotInstalled(t *testing.T) {
	if _, err := ReadManifest(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "abc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("abc", filepath.Join(root, currentName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "abc", manifestName), []byte(`{"arch":"x86_64_linux","bottles":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifest(root); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty manifest: err = %v", err)
	}
}

func TestRunSmokeRequiresBothBinariesToRun(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, script string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("runc", `[ "$1" = --version ] && echo "runc version 1"`)
	write("dockerd", `[ "$1" = --version ] && [ "$PATH" = "`+bin+`:/usr/sbin:/usr/bin:/sbin:/bin" ]`)
	if err := runSmoke(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	write("dockerd", `echo "error while loading shared libraries: libseccomp.so.2" >&2; exit 127`)
	err := runSmoke(t.Context(), dir)
	if err == nil || !strings.Contains(err.Error(), "libseccomp.so.2") {
		t.Fatalf("err = %v, want the loader's message", err)
	}
	if err := os.Remove(filepath.Join(bin, "runc")); err != nil {
		t.Fatal(err)
	}
	if err := runSmoke(t.Context(), dir); err == nil {
		t.Fatal("a missing runc passed")
	}
}

// A run interrupted after the swap but before the daemon restart must still
// report a change next time, or the new engine is never restarted onto.
func TestRuntimeInstallReportsAnUnfinishedRestart(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	m, changed, err := Install(t.Context(), e.client, e.root)
	if err != nil || !changed {
		t.Fatalf("Install = %v, %v", changed, err)
	}
	// Interrupted: the restart never ran, so the marker is still there.
	if _, changed, err := Install(t.Context(), e.client, e.root); err != nil || !changed {
		t.Fatalf("rerun after interrupted restart = %v, %v; want changed", changed, err)
	}
	if e.smokes != 1 {
		t.Errorf("rerun reinstalled: smokes = %d", e.smokes)
	}
	if err := ClearRestartPending(t.Context(), e.root, m.SetID()); err != nil {
		t.Fatal(err)
	}
	if err := ClearRestartPending(t.Context(), e.root, m.SetID()); err != nil {
		t.Fatalf("clearing an absent marker: %v", err)
	}
	if _, changed, err := Install(t.Context(), e.client, e.root); err != nil || changed {
		t.Fatalf("after restart = %v, %v; want unchanged", changed, err)
	}
}

// Two helper runs overlap: run A restarts onto its set, run B then swaps a
// newer set in, and only afterwards does A record its restart. A must not
// clear B's marker, or B's set is never restarted onto (#697).
func TestRuntimeRestartMarkerSurvivesAnOlderRunsClear(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	a, _, err := Install(t.Context(), e.client, e.root)
	if err != nil {
		t.Fatal(err)
	}
	e.publish("two")
	b, changed, err := Install(t.Context(), e.client, e.root)
	if err != nil || !changed || b.SetID() == a.SetID() {
		t.Fatalf("second Install = %v, %v, %v", b.SetID(), changed, err)
	}
	if err := ClearRestartPending(t.Context(), e.root, a.SetID()); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := Install(t.Context(), e.client, e.root); err != nil || !changed {
		t.Fatalf("after the older run's clear, Install = %v, %v; want B's restart still pending", changed, err)
	}
	if err := ClearRestartPending(t.Context(), e.root, b.SetID()); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := Install(t.Context(), e.client, e.root); err != nil || changed {
		t.Fatalf("after B's own clear, Install = %v, %v; want unchanged", changed, err)
	}
}

// A marker written by a release that recorded no set id is empty; it still
// means a restart is pending, and a successful restart clears it.
func TestRuntimeRestartMarkerAcceptsALegacyEmptyMarker(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	m, _, err := Install(t.Context(), e.client, e.root)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(e.root, restartName)
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := Install(t.Context(), e.client, e.root); err != nil || !changed {
		t.Fatalf("legacy marker: Install = %v, %v; want changed", changed, err)
	}
	if err := ClearRestartPending(t.Context(), e.root, m.SetID()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("legacy marker survived a restart: %v", err)
	}
}

// A marker naming a set that never became current is rewritten to name
// current, so a restart onto current can clear it rather than leaving the
// runtime reported as changed on every run.
func TestRuntimeRestartMarkerForANonCurrentSetIsRetargeted(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	m, _, err := Install(t.Context(), e.client, e.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.root, restartName), []byte("0123456789abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := Install(t.Context(), e.client, e.root); err != nil || !changed {
		t.Fatalf("Install = %v, %v; want changed", changed, err)
	}
	if err := ClearRestartPending(t.Context(), e.root, m.SetID()); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := Install(t.Context(), e.client, e.root); err != nil || changed {
		t.Fatalf("after restart onto current, Install = %v, %v; want unchanged", changed, err)
	}
}

// Clearing waits for the install lock, so it cannot interleave with another
// run's swap.
func TestRuntimeRestartMarkerClearWaitsForTheLock(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	m, _, err := Install(t.Context(), e.client, e.root)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lockRoot(t.Context(), e.root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := ClearRestartPending(ctx, e.root, m.SetID()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("clear under a held lock = %v, want the context deadline", err)
	}
	if _, err := os.Lstat(filepath.Join(e.root, restartName)); err != nil {
		t.Fatalf("marker removed without the lock: %v", err)
	}
}

// A current link left dangling by an interrupted reinstall reads as nothing
// installed, so the next run rebuilds instead of failing forever.
func TestRuntimeInstallRecoversADanglingCurrent(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	m, _, err := Install(t.Context(), e.client, e.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(e.root, m.SetID())); err != nil {
		t.Fatal(err)
	}
	m2, changed, err := Install(t.Context(), e.client, e.root)
	if err != nil || !changed || m2.SetID() != m.SetID() {
		t.Fatalf("Install over dangling current = %v, %v", changed, err)
	}
	if _, err := ReadManifest(e.root); err != nil {
		t.Fatal(err)
	}
}

// Reinstalling the same id (its manifest damaged) moves the old copy aside
// and prunes it rather than leaving a stray directory.
func TestRuntimeInstallReplacesADamagedSameSet(t *testing.T) {
	e := newInstallEnv(t)
	e.publish("one")
	m, _, err := Install(t.Context(), e.client, e.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(e.root, m.SetID(), manifestName)); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := Install(t.Context(), e.client, e.root); err != nil || !changed {
		t.Fatalf("reinstall = %v, %v", changed, err)
	}
	if _, err := ReadManifest(e.root); err != nil {
		t.Fatal(err)
	}
	if got, want := e.entries(), sortedNames(lockName, restartName, m.SetID(), currentName); !slices.Equal(got, want) {
		t.Errorf("root holds %v, want %v", got, want)
	}
}
