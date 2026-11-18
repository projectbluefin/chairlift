package bootc

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// Reading an ostree-backend bootc host without root.
//
// bootc 1.16+ refuses `bootc status` and `bootc upgrade --check` to an
// unprivileged caller on both backends. An ostree host keeps the same state
// on disk, world-readable:
//
//   - the booted deployment is named by the kernel command line's
//     ostree=/ostree/boot.N/<os>/<bootcsum>/<serial> argument, whose boot
//     entry is a symlink to /ostree/deploy/<os>/deploy/<csum>.<serial>;
//   - that deployment's .origin keyfile names the image in
//     [origin] container-image-reference;
//   - the booted version is /usr/lib/os-release's IMAGE_VERSION;
//   - /run/ostree/staged-deployment exists while a deployment is staged.
//
// The staged marker is a binary GVariant, so its presence is the signal and
// the staged deployment is the most recently written deployment that is not
// the booted one; without the marker that newest deployment is the rollback.
// Other hosts still get `bootc status`.

// errNotOstree means the host has no ostree boot; the caller falls back to
// `bootc status`.
var errNotOstree = errors.New("bootc: not an ostree-booted host")

const (
	ostreeStagedMarkerPath = "run/ostree/staged-deployment"
	ostreeDeployRoot       = "ostree/deploy"
)

// readOstreeStatus builds a Status from an ostree host's on-disk state. fsys
// is rooted at "/". It returns errNotOstree when the kernel was not booted
// from an ostree deployment.
func readOstreeStatus(fsys fs.FS) (*Status, error) {
	bootedPath, osName, err := ostreeBootedDeploymentPath(fsys)
	if err != nil {
		return nil, err
	}

	booted, err := readOstreeDeployment(fsys, bootedPath)
	if err != nil {
		return nil, fmt.Errorf("bootc: reading the booted deployment: %w", err)
	}
	booted.Image.Version = imageVersion(fsys)

	status := &Status{Status: StatusInfo{Booted: booted}}
	if booted.Image != nil {
		spec := booted.Image.Image
		status.Spec.Image = &spec
	}

	stagedName, rollbackName, err := ostreeStagedAndRollback(fsys, osName, path.Base(bootedPath))
	if err != nil {
		return nil, err
	}
	if stagedName != "" {
		staged, err := readOstreeDeployment(fsys, ostreeDeploymentPath(osName, stagedName))
		if err != nil {
			return nil, fmt.Errorf("bootc: reading the staged deployment: %w", err)
		}
		status.Status.Staged = staged
	}
	if rollbackName != "" {
		if rollback, err := readOstreeDeployment(fsys, ostreeDeploymentPath(osName, rollbackName)); err == nil {
			status.Status.Rollback = rollback
		}
	}
	return status, nil
}

// ostreeBootedDeploymentPath returns the booted deployment's directory and
// stateroot from the kernel command line. The boot entry is a symlink to the
// deployment directory; when the filesystem exposes it, that target is used.
// Otherwise the standard /ostree/deploy/<os>/deploy/<bootcsum>.<serial> path
// is derived. A host whose bootcsum differs from the deployment csum then
// fails the origin read rather than fabricating a status.
func ostreeBootedDeploymentPath(fsys fs.FS) (deployPath, osName string, err error) {
	cmdline, err := fs.ReadFile(fsys, cmdlinePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", "", errNotOstree
		}
		return "", "", fmt.Errorf("bootc: reading the kernel command line: %w", err)
	}
	var bootPath string
	for _, arg := range strings.Fields(string(cmdline)) {
		if value, ok := strings.CutPrefix(arg, "ostree="); ok {
			bootPath = value
			break
		}
	}
	if bootPath == "" {
		return "", "", errNotOstree
	}

	parts := strings.Split(strings.TrimPrefix(bootPath, "/"), "/")
	if len(parts) != 5 || parts[0] != "ostree" || !validBootVersion(parts[1]) {
		return "", "", errNotOstree
	}
	osName, bootcsum, serial := parts[2], parts[3], parts[4]
	if !validStateroot(osName) || !validDeploymentID(bootcsum) || !validSerial(serial) {
		return "", "", errNotOstree
	}

	relative := strings.TrimPrefix(bootPath, "/")
	if target, err := fs.ReadLink(fsys, relative); err == nil {
		resolved := path.Clean(path.Join(path.Dir(relative), target))
		if strings.HasPrefix(target, "/") {
			resolved = path.Clean(strings.TrimPrefix(target, "/"))
		}
		if strings.HasPrefix(resolved, path.Join(ostreeDeployRoot, osName, "deploy")+"/") {
			return resolved, osName, nil
		}
	}
	return ostreeDeploymentPath(osName, bootcsum+"."+serial), osName, nil
}

func ostreeDeploymentPath(osName, name string) string {
	return path.Join(ostreeDeployRoot, osName, "deploy", name)
}

func validBootVersion(value string) bool {
	digits, ok := strings.CutPrefix(value, "boot.")
	if !ok || digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func validStateroot(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func validSerial(serial string) bool {
	if serial == "" {
		return false
	}
	for _, r := range serial {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validOstreeDeploymentName accepts the <csum>.<serial> directory names
// ostree writes, so nothing read from the command line or a marker can walk
// outside the deploy directory.
func validOstreeDeploymentName(name string) bool {
	csum, serial, ok := strings.Cut(name, ".")
	return ok && validDeploymentID(csum) && validSerial(serial)
}

func readOstreeDeployment(fsys fs.FS, deployPath string) (*Deployment, error) {
	data, err := fs.ReadFile(fsys, deployPath+".origin")
	if err != nil {
		return nil, err
	}
	values := parseOrigin(data)
	image, err := ostreeImageReference(values["origin.container-image-reference"])
	if err != nil {
		return nil, fmt.Errorf("deployment %s: %w", path.Base(deployPath), err)
	}
	return &Deployment{Image: &ImageStatus{
		Image:       ImageReference{Image: image, Transport: "registry"},
		ImageDigest: values["image.manifest_digest"],
	}}, nil
}

// ostreeImageReference strips the ostree transport prefix from an origin's
// container-image-reference, leaving the registry reference. A local
// containers-storage reference names no registry image and is rejected.
func ostreeImageReference(ref string) (string, error) {
	original := ref
	for _, prefix := range []string{"ostree-remote-registry:", "ostree-remote-image:"} {
		if rest, ok := strings.CutPrefix(ref, prefix); ok {
			_, image, ok := strings.Cut(rest, ":")
			if !ok {
				return "", fmt.Errorf("unsupported image reference %q", original)
			}
			ref = image
			break
		}
	}
	for _, prefix := range []string{
		"ostree-unverified-registry:",
		"ostree-unverified-image:",
		"ostree-image-signed:",
		"docker://",
	} {
		ref = strings.TrimPrefix(ref, prefix)
	}
	if ref == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "containers-storage:") || strings.HasPrefix(ref, "ostree-") {
		return "", fmt.Errorf("unsupported image reference %q", original)
	}
	return ref, nil
}

// ostreeStagedAndRollback names the staged and rollback deployments. The
// staged marker is a binary GVariant, so its presence is the signal; the
// staged deployment is the most recently written deployment that is not the
// booted one, and the next newest is the rollback target. With nothing staged
// the newest non-booted deployment is the rollback, so the Recovery page keeps
// offering it in the steady state (composefs does the same).
func ostreeStagedAndRollback(fsys fs.FS, osName, bootedName string) (staged, rollback string, err error) {
	stagedPresent := true
	if _, err := fs.Stat(fsys, ostreeStagedMarkerPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return "", "", fmt.Errorf("bootc: reading the staged deployment marker: %w", err)
		}
		stagedPresent = false
	}

	entries, err := fs.ReadDir(fsys, path.Join(ostreeDeployRoot, osName, "deploy"))
	if err != nil {
		return "", "", fmt.Errorf("bootc: listing ostree deployments: %w", err)
	}
	type candidate struct {
		name string
		mod  time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == bootedName || !validOstreeDeploymentName(name) {
			continue
		}
		info, err := fs.Stat(fsys, ostreeDeploymentPath(osName, name)+".origin")
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{name: name, mod: info.ModTime()})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mod.After(candidates[j].mod) })
	if !stagedPresent {
		if len(candidates) > 0 {
			rollback = candidates[0].name
		}
		return "", rollback, nil
	}
	if len(candidates) == 0 {
		return "", "", fmt.Errorf("bootc: staged deployment marker present but no staged deployment found")
	}
	staged = candidates[0].name
	if len(candidates) > 1 {
		rollback = candidates[1].name
	}
	return staged, rollback, nil
}
