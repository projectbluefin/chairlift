package dockerbrew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	currentName = "current"
	// activeName is the link the managed docker.service runs from. Its
	// ExecStartPre repoints it at current whenever the daemon starts, so it
	// names the set the running daemon spawns from; prune keeps that set.
	activeName   = "active"
	manifestName = "manifest.json"
	lockName     = ".lock"
	// restartName marks a set swapped in whose daemon has not yet been
	// restarted onto it; it survives an interrupted helper run. It holds the
	// set id it was written for, so a run that restarted onto an older set
	// cannot clear the marker a later swap wrote. A marker written before
	// ids were recorded is empty and names no set.
	restartName = ".restart-pending"
	// maxMarker bounds the marker read; a set id and newline fit easily.
	maxMarker   = 64
	stagingPre  = ".staging-"
	downloadPre = ".download-"
	linkPre     = ".current-"
)

var setIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Seams replaced by tests, which cannot run as root.
var (
	// ownerUID is the owner Root, Current, and set directories must have.
	ownerUID uint32 = 0
	// smokeTest runs the staged engine before it is swapped in.
	smokeTest = runSmoke
	// lockPoll is how often a held install lock is retried.
	lockPoll = 200 * time.Millisecond
)

// Install fetches and verifies the fixed bottle set and makes it the
// current engine under root. It returns changed=false, touching nothing but
// stale leftovers, when root/current already names the same set and no
// earlier swap is still awaiting its daemon restart (the restart-pending
// marker, cleared by ClearRestartPending).
func Install(ctx context.Context, c Client, root string) (m Manifest, changed bool, err error) {
	if err := ensureRoot(root); err != nil {
		return Manifest{}, false, err
	}
	unlock, err := lockRoot(ctx, root)
	if err != nil {
		return Manifest{}, false, err
	}
	defer unlock()

	cur, err := currentSet(root)
	if err != nil {
		return Manifest{}, false, err
	}
	arch, err := Arch()
	if err != nil {
		return Manifest{}, false, err
	}
	payload, err := c.Index(ctx)
	if err != nil {
		return Manifest{}, false, err
	}
	bottles, err := SelectBottles(payload, arch)
	if err != nil {
		return Manifest{}, false, err
	}
	m = Manifest{Arch: arch, Bottles: bottles}
	id := m.SetID()
	if cur == id {
		if installed, err := readManifestFile(filepath.Join(root, id, manifestName)); err == nil && installed.SetID() == id {
			pending, err := restartPending(root)
			if err != nil {
				return Manifest{}, false, err
			}
			if pending {
				// A marker naming another set (a swap that failed after the
				// marker was written, or an index that went back) would
				// never be cleared by a restart onto this one; what is
				// pending now is a restart onto current.
				if named, err := readMarker(filepath.Join(root, restartName)); err != nil || named != id {
					if err := writeFileAtomic(filepath.Join(root, restartName), []byte(id+"\n"), 0o644); err != nil {
						return Manifest{}, false, err
					}
				}
			}
			return m, pending, prune(root, id)
		}
	}

	staging, err := os.MkdirTemp(root, stagingPre)
	if err != nil {
		return Manifest{}, false, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := os.Chmod(staging, 0o755); err != nil {
		return Manifest{}, false, err
	}
	for _, b := range bottles {
		if err := fetchAndExtract(ctx, c, root, b, staging); err != nil {
			return Manifest{}, false, err
		}
	}
	if err := patchTree(staging, Loader(arch)); err != nil {
		return Manifest{}, false, err
	}
	if err := smokeTest(ctx, staging); err != nil {
		return Manifest{}, false, err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Manifest{}, false, err
	}
	if err := writeFileAtomic(filepath.Join(staging, manifestName), append(data, '\n'), 0o644); err != nil {
		return Manifest{}, false, err
	}
	if err := writeFileAtomic(filepath.Join(root, restartName), []byte(id+"\n"), 0o644); err != nil {
		return Manifest{}, false, err
	}
	setDir := filepath.Join(root, id)
	if _, err := os.Lstat(setDir); err == nil {
		// Reinstalling the same id: move the old copy aside for prune rather
		// than deleting it under a live current link.
		// Reserve a unique name (the lock is held), then free it for Rename.
		aside, err := os.MkdirTemp(root, stagingPre)
		if err != nil {
			return Manifest{}, false, err
		}
		if err := os.Remove(aside); err != nil {
			return Manifest{}, false, err
		}
		if err := os.Rename(setDir, aside); err != nil {
			return Manifest{}, false, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, false, err
	}
	if err := os.Rename(staging, setDir); err != nil {
		return Manifest{}, false, err
	}
	if err := swapCurrent(root, id); err != nil {
		return Manifest{}, false, err
	}
	// The engine is current; leftovers are best-effort and retried next run.
	_ = prune(root, id)
	return m, true, nil
}

func restartPending(root string) (bool, error) {
	_, err := os.Lstat(filepath.Join(root, restartName))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// ClearRestartPending removes the marker Install writes before swapping a
// new set in; call it once the daemon has restarted onto set id, the set the
// caller's own Install made current.
//
// It takes the install lock, so it cannot interleave with another run's
// swap, and it removes the marker only when it names id (or is an empty
// marker from a release that recorded no id). Otherwise a later run swapped
// in a newer set after this caller's restart, and that set still awaits its
// own restart: removing the marker would make every following Install
// report the runtime unchanged and never restart onto it.
func ClearRestartPending(ctx context.Context, root, id string) error {
	if !setIDPattern.MatchString(id) {
		return fmt.Errorf("dockerbrew: clearing restart marker: invalid set id %q", id)
	}
	unlock, err := lockRoot(ctx, root)
	if err != nil {
		return err
	}
	defer unlock()
	p := filepath.Join(root, restartName)
	named, err := readMarker(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if named != "" && named != id {
		return nil
	}
	err = os.Remove(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// readMarker returns the set id a restart marker names, "" for an empty
// marker. Anything but a small regular file is refused.
func readMarker(p string) (string, error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("dockerbrew: %s is not a regular file", p)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMarker+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxMarker {
		return "", fmt.Errorf("dockerbrew: %s is too large", p)
	}
	named := strings.TrimSpace(string(data))
	if named != "" && !setIDPattern.MatchString(named) {
		return "", fmt.Errorf("dockerbrew: %s names no set (%q)", p, named)
	}
	return named, nil
}

// ensureRoot creates root and its parent 0755 when missing and refuses either
// that is a symlink, not a directory, not owned by root, or group/world-
// writable: whoever can write the parent can swap root out from under it.
func ensureRoot(root string) error {
	for _, dir := range []string{filepath.Dir(root), root} {
		if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
				return err
			}
			if err := os.Mkdir(dir, 0o755); err != nil {
				return err
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				return err
			}
		}
		if err := checkOwnedDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func checkOwnedDir(p string) error {
	fi, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("dockerbrew: %s is not a directory (symlinks are refused)", p)
	}
	return checkOwner(p, fi)
}

func checkOwner(p string, fi fs.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != ownerUID {
		return fmt.Errorf("dockerbrew: %s is not owned by uid %d", p, ownerUID)
	}
	if fi.Mode()&fs.ModeSymlink == 0 && fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("dockerbrew: %s is writable by group or others", p)
	}
	return nil
}

// lockRoot takes an exclusive flock on root/.lock, giving up when ctx ends.
func lockRoot(ctx context.Context, root string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(root, lockName), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, fmt.Errorf("dockerbrew: lock %s: %w", root, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}

// currentSet returns the set id root/current names, or "" when there is
// none. Anything but a root-owned symlink to a root-owned set directory
// under root is refused.
func currentSet(root string) (string, error) {
	p := filepath.Join(root, currentName)
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if fi.Mode()&fs.ModeSymlink == 0 {
		return "", fmt.Errorf("dockerbrew: %s is not a symlink", p)
	}
	if err := checkOwner(p, fi); err != nil {
		return "", err
	}
	target, err := os.Readlink(p)
	if err != nil {
		return "", err
	}
	if !setIDPattern.MatchString(target) {
		return "", fmt.Errorf("dockerbrew: %s points outside %s (%q)", p, root, target)
	}
	dir := filepath.Join(root, target)
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		// A dangling link (an interrupted reinstall): nothing is installed.
		return "", nil
	}
	if err := checkOwnedDir(dir); err != nil {
		return "", err
	}
	return target, nil
}

func fetchAndExtract(ctx context.Context, c Client, root string, b Bottle, staging string) error {
	f, err := os.CreateTemp(root, downloadPre)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	defer func() { _ = f.Close() }()
	if err := c.Bottle(ctx, b, f); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	return Extract(f, b, staging)
}

func patchTree(staging, interp string) error {
	for _, sub := range []string{"bin", "lib"} {
		entries, err := os.ReadDir(filepath.Join(staging, sub))
		if err != nil {
			return err
		}
		for _, e := range entries {
			if _, err := PatchELF(filepath.Join(staging, sub, e.Name()), interp); err != nil {
				return err
			}
		}
	}
	return nil
}

// runSmoke runs the staged runc and dockerd with a minimal environment, so a
// set whose loader or libraries do not resolve never becomes current.
func runSmoke(ctx context.Context, dir string) error {
	bin := filepath.Join(dir, "bin")
	for _, name := range []string{"runc", "dockerd"} {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		cmd := exec.CommandContext(cctx, filepath.Join(bin, name), "--version")
		cmd.Env = []string{"PATH=" + bin + ":/usr/sbin:/usr/bin:/sbin:/bin"}
		cmd.Dir = "/"
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("dockerbrew: staged %s --version failed: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// swapCurrent atomically points root/current at id.
func swapCurrent(root, id string) error {
	tmp := filepath.Join(root, linkPre+id)
	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Symlink(id, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(root, currentName)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	d, err := os.Open(root)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

// prune removes every set directory except keep and the set root/active
// names, plus any leftovers of an interrupted install. Callers hold the lock.
//
// The active set is the one a running daemon started on. When its restart
// onto keep was deferred because containers are running, that daemon still
// spawns shims, runc, and docker-proxy from it, so deleting it would break
// every container start until the restart happens (#696).
func prune(root, keep string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	active := activeSet(root)
	var errs []error
	for _, e := range entries {
		name := e.Name()
		stale := (setIDPattern.MatchString(name) && name != keep && name != active) ||
			strings.HasPrefix(name, stagingPre) ||
			strings.HasPrefix(name, downloadPre) ||
			strings.HasPrefix(name, linkPre)
		if stale {
			errs = append(errs, os.RemoveAll(filepath.Join(root, name)))
		}
	}
	return errors.Join(errs...)
}

// activeSet returns the set id root/active names, or "" when there is no
// such link or it names anything but a set id. Only prune reads it, and only
// to keep a directory, so an unreadable or foreign link just protects
// nothing.
func activeSet(root string) string {
	target, err := os.Readlink(filepath.Join(root, activeName))
	if err != nil || !setIDPattern.MatchString(target) {
		return ""
	}
	return target
}

// ReadManifest reads the installed set's manifest; the error wraps
// os.ErrNotExist when no engine is installed.
func ReadManifest(root string) (Manifest, error) {
	return readManifestFile(filepath.Join(root, currentName, manifestName))
}

func readManifestFile(p string) (Manifest, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("dockerbrew: parse %s: %w", p, err)
	}
	if m.Arch == "" || len(m.Bottles) == 0 {
		return Manifest{}, fmt.Errorf("dockerbrew: %s names no bottles", p)
	}
	return m, nil
}
