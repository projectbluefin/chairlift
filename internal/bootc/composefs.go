package bootc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/registrytags"
)

// Reading bootc state without root.
//
// bootc 1.16 refuses `bootc status` and `bootc upgrade --check` to an
// unprivileged caller ("Querying root privilege: This command must be
// executed as the root user"), even though both only read. On a composefs
// host — Dakota, verified 2026-09-26 — everything they report is on disk and
// world-readable:
//
//   - the booted deployment is the `composefs=<id>` kernel argument;
//   - the staged deployment is /run/composefs/staged-deployment's depl_id;
//   - each deployment's image reference and manifest digest are in
//     /sysroot/state/deploy/<id>/<id>.origin;
//   - the booted version is /usr/lib/os-release's IMAGE_VERSION.
//
// So ChairLift reads that instead of asking for a password to learn the
// version it is running. Other hosts still get `bootc status`.

// errNotComposefs means the host has no composefs boot; the caller falls back
// to `bootc status`.
var errNotComposefs = errors.New("bootc: not a composefs-booted host")

const (
	cmdlinePath       = "proc/cmdline"
	stagedMarkerPath  = "run/composefs/staged-deployment"
	deployDir         = "sysroot/state/deploy"
	osReleasePath     = "usr/lib/os-release"
	composefsArgument = "composefs="
)

// readComposefsStatus builds a Status from a composefs host's on-disk state.
// fsys is rooted at "/". It returns errNotComposefs when the kernel was not
// booted from a composefs deployment.
func readComposefsStatus(fsys fs.FS) (*Status, error) {
	bootedID, err := composefsBootedID(fsys)
	if err != nil {
		return nil, err
	}

	booted, err := readDeployment(fsys, bootedID)
	if err != nil {
		return nil, fmt.Errorf("bootc: reading the booted deployment: %w", err)
	}
	if booted.Image != nil {
		booted.Image.Version = imageVersion(fsys)
	}

	status := &Status{Status: StatusInfo{Booted: booted}}
	if booted.Image != nil {
		spec := booted.Image.Image
		status.Spec.Image = &spec
	}

	stagedID, err := composefsStagedID(fsys)
	if err != nil {
		return nil, err
	}
	if stagedID != "" {
		staged, err := readDeployment(fsys, stagedID)
		if err != nil {
			return nil, fmt.Errorf("bootc: reading the staged deployment: %w", err)
		}
		status.Status.Staged = staged
	}

	rollbackID, err := composefsRollbackID(fsys, bootedID, stagedID)
	if err != nil {
		return nil, err
	}
	if rollbackID != "" {
		if rollback, err := readDeployment(fsys, rollbackID); err == nil {
			// Its version label and image creation time are only readable as
			// root. The origin's mtime is the deploy day, not a release date,
			// so Timestamp stays empty rather than mislabel it (#521).
			status.Status.Rollback = rollback
		}
	}
	return status, nil
}

func composefsBootedID(fsys fs.FS) (string, error) {
	cmdline, err := fs.ReadFile(fsys, cmdlinePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", errNotComposefs
		}
		return "", fmt.Errorf("bootc: reading the kernel command line: %w", err)
	}
	for _, arg := range strings.Fields(string(cmdline)) {
		if id, ok := strings.CutPrefix(arg, composefsArgument); ok {
			// composefs=?<id> marks an insecure (unverified) mount; the
			// deployment ID is the same.
			id = strings.TrimPrefix(id, "?")
			if validDeploymentID(id) {
				return id, nil
			}
		}
	}
	return "", errNotComposefs
}

func composefsStagedID(fsys fs.FS) (string, error) {
	data, err := fs.ReadFile(fsys, stagedMarkerPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("bootc: reading the staged deployment marker: %w", err)
	}
	var marker struct {
		DeploymentID string `json:"depl_id"`
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		return "", fmt.Errorf("bootc: unreadable staged deployment marker: %w", err)
	}
	if !validDeploymentID(marker.DeploymentID) {
		return "", fmt.Errorf("bootc: staged deployment marker names %q", marker.DeploymentID)
	}
	return marker.DeploymentID, nil
}

// composefsRollbackID is the most recently written deployment that is
// neither booted nor staged: bootc's rollback target is the previous
// deployment, and a composefs host keeps at most one besides those two.
func composefsRollbackID(fsys fs.FS, bootedID, stagedID string) (string, error) {
	entries, err := fs.ReadDir(fsys, deployDir)
	if err != nil {
		return "", fmt.Errorf("bootc: listing deployments: %w", err)
	}
	var (
		rollbackID string
		newest     time.Time
	)
	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || id == bootedID || id == stagedID || !validDeploymentID(id) {
			continue
		}
		info, err := fs.Stat(fsys, originPath(id))
		if err != nil {
			continue
		}
		if rollbackID == "" || info.ModTime().After(newest) {
			rollbackID, newest = id, info.ModTime()
		}
	}
	return rollbackID, nil
}

func originPath(id string) string {
	return path.Join(deployDir, id, id+".origin")
}

// validDeploymentID accepts the lowercase hex IDs bootc writes, so nothing
// read from the command line or a marker can walk outside deployDir.
func validDeploymentID(id string) bool {
	if len(id) < 32 {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func readDeployment(fsys fs.FS, id string) (*Deployment, error) {
	data, err := fs.ReadFile(fsys, originPath(id))
	if err != nil {
		return nil, err
	}
	values := parseOrigin(data)
	// "ostree-unverified-image:docker://ghcr.io/…:tag", "ostree-image-signed:
	// docker://…", or plain "docker://…": the image is what follows docker://.
	ref := values["origin.container-image-reference"]
	i := strings.LastIndex(ref, "docker://")
	if i < 0 || i+len("docker://") == len(ref) {
		return nil, fmt.Errorf("deployment %s: unsupported image reference %q", id[:12], ref)
	}
	return &Deployment{Image: &ImageStatus{
		Image:       ImageReference{Image: ref[i+len("docker://"):], Transport: "registry"},
		ImageDigest: values["image.manifest_digest"],
	}}, nil
}

// parseOrigin reads an origin file's "key = value" lines into
// "section.key" entries.
func parseOrigin(data []byte) map[string]string {
	values := make(map[string]string)
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = strings.TrimSpace(line[1 : len(line)-1])
		default:
			if key, value, ok := strings.Cut(line, "="); ok {
				values[section+"."+strings.TrimSpace(key)] = strings.TrimSpace(value)
			}
		}
	}
	return values
}

// imageVersion is the booted image's version from os-release, the same
// value `bootc status` reports from the image's version label.
func imageVersion(fsys fs.FS) string {
	data, err := fs.ReadFile(fsys, osReleasePath)
	if err != nil {
		return ""
	}
	fields := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			fields[key] = strings.Trim(value, `"'`)
		}
	}
	if v := fields["IMAGE_VERSION"]; v != "" {
		return v
	}
	return fields["VERSION_ID"]
}

// splitImageReference splits "registry/repo:tag" into the repository and the
// tag. A digest-pinned or untagged reference has no stream to check.
func splitImageReference(image string) (repository, tag string, ok bool) {
	if strings.Contains(image, "@") {
		return "", "", false
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash || colon == len(image)-1 {
		return "", "", false
	}
	return image[:colon], image[colon+1:], true
}

// tagResolver resolves a tag to its registry digest; production is a
// registrytags.Client.
type tagResolver func(ctx context.Context, repository, tag string) (registrytags.Tag, error)

// checkFromRegistry answers `bootc upgrade --check` without root: an update
// is available when the booted image's tag now resolves to a digest that is
// neither booted nor already staged. The version is the build date, which is
// how these images are versioned (IMAGE_VERSION=20260921).
func checkFromRegistry(ctx context.Context, status *Status, resolve tagResolver) (AvailableUpdate, error) {
	if !status.Booted() {
		return AvailableUpdate{}, &Error{Message: "bootc update check: no booted deployment"}
	}
	repository, tag, ok := splitImageReference(status.Status.Booted.ImageRef())
	if !ok {
		return AvailableUpdate{}, &Error{Message: fmt.Sprintf("bootc update check: %q does not follow a tag", status.Status.Booted.ImageRef())}
	}
	latest, err := resolve(ctx, repository, tag)
	if err != nil {
		return AvailableUpdate{}, &Error{Message: fmt.Sprintf("bootc update check failed: %v", err), Err: err}
	}
	digest := latest.Digest
	if latest.Platforms != nil {
		// An index: the host deployed this platform's child manifest.
		digest = latest.Platforms["linux/"+runtime.GOARCH]
	}
	if digest == "" {
		return AvailableUpdate{}, &Error{Message: "bootc update check: registry returned no digest for this platform"}
	}
	if digest == status.Status.Booted.Digest() || digest == status.Status.Staged.Digest() {
		return AvailableUpdate{}, nil
	}
	update := AvailableUpdate{Available: true, Digest: digest}
	if !latest.Created.IsZero() {
		update.Version = latest.Created.UTC().Format("20060102")
	}
	return update, nil
}

// ComposefsBooted reports whether the kernel booted a composefs deployment.
// It reads only the kernel command line, so it is safe on the GTK thread.
func ComposefsBooted() bool {
	_, err := composefsBootedID(hostRoot)
	return err == nil
}
