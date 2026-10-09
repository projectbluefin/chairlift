// Package printerapp implements ChairLift's printer support: rootless quadlets
// for the projectbluefin Printer Applications, one unit per printer.
//
// It is the printer port of internal/aistack. aistack writes one quadlet for
// the local-AI stack and drives it with `systemctl --user`; printerapp writes
// one quadlet per printer application and drives each the same way. Nothing
// here is privileged. Quadlet units live under the user's
// ~/.config/containers/systemd and are started with `systemctl --user`, so the
// container runs rootless in the invoking account — the same reasoning that
// keeps the AI stack and gaming mode off the pkexec path. On a bootc host that
// also means nothing is layered onto the image.
//
// A printer application is a Family (a published image, one per driver family:
// Ghostscript, HPLIP, Gutenprint, PostScript) plus a unique app name. A family
// can host several printers, and each App gets its own unit, host port, and
// state volume, so one logical device has exactly one owner and one
// advertisement rather than two units fighting over the same port and volume.
package printerapp

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

const commandTimeout = 5 * time.Minute

const unitPrefix = "chairlift-printer-"

// portBase and portRange scope the ports a printer application may publish
// into when allocating dynamically.
const (
	portBase  = 18000
	portRange = 1000
)

// Family identifies one Printer Application family. Each publishes a
// digest-pinned image reference at GHCR (the projectbluefin
// *-printer-app repositories). The image, pinned by an immutable
// application-version tag or manifest digest, is what we run — not a mutable
// `:latest` or `:build` tag, which a re-pull could change under us.
type Family struct {
	// ID is the lowercase identifier used in unit names and volume paths.
	ID string
	// DisplayName is the human-readable family name.
	DisplayName string
	// Repo is the GHCR repository path for the family's image.
	Repo string
	// Version is the immutable application-version tag pinned for this family.
	Version string
	// Digest is the immutable image manifest or index digest.
	Digest string
	// DefaultPort is the contracted host port for this family (ADR-0016).
	DefaultPort int
}

// Image returns the digest-pinned index for this family.
func (f Family) Image() string {
	if f.Digest != "" {
		return f.Repo + "@" + f.Digest
	}
	return f.Repo + ":" + f.Version
}

// App is one namespaced printer application: a Family plus a unique app name.
// Multiple apps may share a family's image, but each has its own unit, port,
// and volume, so one logical device has exactly one owner.
type App struct {
	Family Family
	// Name uniquely identifies this printer within its family.
	Name string
}

// UnitName is the quadlet file ChairLift writes. It is namespaced with both the
// family and the app so two printers on the same family never collide, and it
// carries the `chairlift-` prefix so it never overwrites a unit from another
// tool.
func (a App) UnitName() string {
	if a.Name == a.Family.ID {
		return unitPrefix + sanitize(a.Family.ID) + ".container"
	}
	return unitPrefix + sanitize(a.Family.ID) + "-" + sanitize(a.Name) + ".container"
}

// ServiceName is the systemd unit quadlet generates from UnitName.
func (a App) ServiceName() string {
	return strings.TrimSuffix(a.UnitName(), ".container") + ".service"
}

// ContainerName is the running container's name, matched to the unit so
// `podman ps` output is recognizable.
func (a App) ContainerName() string {
	if a.Name == a.Family.ID {
		return unitPrefix + sanitize(a.Family.ID)
	}
	return unitPrefix + sanitize(a.Family.ID) + "-" + sanitize(a.Name)
}

// Port is the host port the printer's IPP service is published on. It is
// derived deterministically from the family and app name, so the same printer
// always publishes the same port across restarts — one owner, one address —
// while distinct printers get distinct ports.
//
// ponytail: the port is a hash of the identity, not a registry, so a very
// large number of printers could in principle collide. A host runs a handful
// of printers at most; if that ever changes, replace the hash with a persistent
// port allocation keyed by unit name.
func (a App) Port() int {
	if a.Name == a.Family.ID && a.Family.DefaultPort > 0 {
		return a.Family.DefaultPort
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(a.Family.ID + "\x00" + a.Name))
	return portBase + int(h.Sum32()%portRange)
}

// Volume is the per-app state volume, using the systemd home escape `%h`
// that quadlet expands. It mounts to the container's state directory
// /var/lib/<family>-printer-app.
func (a App) Volume() string {
	return fmt.Sprintf("%%h/printer-workspaces/%s/%s:/var/lib/%s-printer-app:z", a.Family.ID, a.Name, a.Family.ID)
}

// HostVolumeDir returns the absolute host path for the app's state volume.
func (a App) HostVolumeDir() (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "printer-workspaces", a.Family.ID, a.Name), nil
}

// families is the set of Printer Application families ChairLift can drive.
// Every reference was taken from the projectbluefin *-printer-app repositories,
// which publish digest-pinned container images.
var families = []Family{
	{
		ID:          "ghostscript",
		DisplayName: "Ghostscript",
		Repo:        "ghcr.io/projectbluefin/ghostscript-printer-app",
		Version:     "10.07.1-2",
		Digest:      "sha256:82487bd81925b824f16d79a50b4237230d00429fca7761454299a8a4393368cc",
		DefaultPort: 18010,
	},
	{
		ID:          "hplip",
		DisplayName: "HPLIP",
		Repo:        "ghcr.io/projectbluefin/hplip-printer-app",
		Version:     "3.26.4",
		Digest:      "sha256:1f81f507ce603f19eebb83fdcdc5b7de7bc7f52f728e9626c2c1224ea7477de8",
		DefaultPort: 18030,
	},
	{
		ID:          "gutenprint",
		DisplayName: "Gutenprint",
		Repo:        "ghcr.io/projectbluefin/gutenprint-printer-app",
		Version:     "5.3.6-4.1",
		Digest:      "sha256:3ca46b65bba16e258d7f93582beb9ccdf71a8b4e450b9d01f8cb545a945b93a1",
		DefaultPort: 18050,
	},
}

// Families returns the known Printer Application families, in display order.
func Families() []Family {
	out := make([]Family, len(families))
	copy(out, families)
	return out
}

// Select returns the default app for a family, named after the family. Callers
// that manage a specific physical printer pass their own app name.
func Select(f Family) App {
	return App{Family: f, Name: f.ID}
}

// sanitize collapses a name into the safe character set a quadlet unit name and
// volume path can carry, so an app named with arbitrary user text still yields
// a stable, collision-free unit name without reaching into the host.
func sanitize(name string) string {
	var b strings.Builder
	previousSep := true // so a leading separator is never emitted
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			previousSep = false
		} else if !previousSep {
			// Collapse runs of separators to one so "Office  Printer" and
			// "office-printer" land on the same name rather than two units.
			b.WriteRune('-')
			previousSep = true
		}
	}
	return b.String()
}

// ApplyOverrides replaces a family's pinned image from configuration. A site
// that mirrors the indexes points its families at the mirror; the mirror must
// serve the same digest-pinned index. This lives in the ordinary config
// file rather than the root-only channels.yml because the container runs
// rootless in the invoking account, so pointing it at another image grants
// nothing a user could not get by running podman themselves.
// An unknown family ID is an error rather than a silent no-op, since a typo'd
// key would otherwise leave the site believing its mirror was in use.
func ApplyOverrides(images map[string]string) error {
	for id, image := range images {
		if image == "" {
			return fmt.Errorf("printerapp: family %q has an empty image", id)
		}
		found := false
		for i := range families {
			if families[i].ID == id {
				families[i].Repo = repoPart(image)
				if strings.Contains(image, "@sha256:") {
					families[i].Digest = digestPart(image)
					families[i].Version = ""
				} else {
					families[i].Digest = ""
					families[i].Version = versionPart(image)
				}
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("printerapp: unknown family %q", id)
		}
	}
	return nil
}

func digestPart(image string) string {
	if i := strings.Index(image, "@"); i >= 0 {
		return image[i+1:]
	}
	return ""
}

// repoPart splits an image reference into its repository path.
func repoPart(image string) string {
	if i := strings.Index(image, "@"); i >= 0 {
		return image[:i]
	}
	if i := strings.LastIndex(image, ":"); i >= 0 {
		return image[:i]
	}
	return image
}

// versionPart splits an image reference into its tag.
func versionPart(image string) string {
	if i := strings.Index(image, "@"); i >= 0 {
		return ""
	}
	if i := strings.LastIndex(image, ":"); i >= 0 {
		return image[i+1:]
	}
	return "latest"
}

// RenderUnit returns the quadlet .container file for one printer application.
//
// The unit runs on host networking per ADR-0016 so IPP is LAN-reachable and
// DNS-SD advertisements carry a routable host address. The state volume is
// bind-mounted read-write so the printer's cached driver state persists across
// enable/disable.
func RenderUnit(app App) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[Unit]\nDescription=Printer Application (%s — %s)\nAfter=network-online.target\n\n",
		app.Family.DisplayName, app.Name)

	b.WriteString("[Container]\n")
	fmt.Fprintf(&b, "ContainerName=%s\n", app.ContainerName())
	fmt.Fprintf(&b, "Image=%s\n", app.Family.Image())
	fmt.Fprintf(&b, "Environment=PORT=%d\n", app.Port())
	b.WriteString("UserNS=keep-id:uid=65532,gid=65532\n")
	b.WriteString("Network=host\n")
	fmt.Fprintf(&b, "Volume=%s\n\n", app.Volume())

	b.WriteString("[Service]\nRestart=on-failure\nRestartSec=10\n\n")
	b.WriteString("[Install]\nWantedBy=default.target\n")

	return b.String()
}

// RenderUnits renders a set of printer applications. The units are sorted by
// name so the output is stable regardless of the order the caller passes them
// in, which keeps diffs and dry-run logs deterministic.
func RenderUnits(apps []App) string {
	sorted := make([]App, len(apps))
	copy(sorted, apps)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].UnitName() < sorted[j].UnitName()
	})

	var b strings.Builder
	for _, app := range sorted {
		b.WriteString(RenderUnit(app))
	}
	return b.String()
}

// unitDir is an injection seam for the quadlet directory, so the install and
// remove paths are testable without writing into a real home directory.
var unitDir = defaultUnitDir
var userHomeDir = os.UserHomeDir

func defaultUnitDir() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "containers", "systemd"), nil
}

var runSystemctl = execSystemctl
var runSystemctlOutput = execSystemctlOutput

func execSystemctl(ctx context.Context, args ...string) error {
	_, err := execSystemctlOutput(ctx, args...)
	return err
}

func execSystemctlOutput(ctx context.Context, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	full := append([]string{"--user"}, args...)
	cmd := exec.CommandContext(runCtx, "systemctl", full...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		return trimmed, fmt.Errorf("systemctl %s: %s", strings.Join(full, " "), trimmed)
	}
	return trimmed, nil
}

func writeAtomic(dest string, content []byte) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(dest)+".tmp.*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(content); err != nil {
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

// IsAvailable reports whether this host can run the applications at all.
// Quadlet is a Podman feature, so without Podman there is nothing to install
// into.
func IsAvailable() bool {
	_, err := exec.LookPath("podman")
	return err == nil
}

// UnitPath returns the absolute path of one printer application's quadlet file.
func UnitPath(app App) (string, error) {
	dir, err := unitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, app.UnitName()), nil
}

// IsEnabled reports whether ChairLift's quadlet is installed. The unit file's
// presence is the state, not the container's running status: a printer whose
// container is restarting is enabled, and reading it any other way would make
// the switch flicker during a first start.
func IsEnabled(app App) bool {
	path, err := UnitPath(app)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// ErrAdminUnauthenticated reports that the printer application image cannot be
// safely enabled on host networking because its web administration interface is
// reachable without authentication (ADR-0016).
var ErrAdminUnauthenticated = errors.New("printer application web administration is unauthenticated; refused on host network (ADR-0016)")

// CanEnable is the ADR-0016 enable condition, queryable so the view can show
// a family as a non-enabled state instead of offering a switch that refuses.
// An application may be enabled only when its web administration is either
// authenticated (an auth service, admin group, or password) or absent
// (server-options=no-web-interface).
//
// No published image can be given either yet: every entrypoint forwards only
// PORT and a log file, so ChairLift has no way to hand the credential over.
// The image-side contract is requested in projectbluefin/ghostscript-printer-app#65
// (mirrored in hplip-printer-app#51 and gutenprint-printer-app#57): the
// entrypoint is to read PRINTER_APP_AUTH_SERVICE, PRINTER_APP_ADMIN_GROUP,
// and PRINTER_APP_SERVER_OPTIONS and forward them as -o auth-service, -o
// admin-group, and -o server-options. Once an image ships that, this is
// where the family starts returning nil and RenderUnit gains the Environment
// lines that carry the values; nothing reads or writes those names today.
// Until then every family is refused, and the refusal is the same error
// Enable returns.
func CanEnable(f Family) error {
	return ErrAdminUnauthenticated
}

// Enable writes the quadlet for the printer application and starts it. Enabling
// only writes the unit and starts the service: the image pull happens inside
// the container runtime afterwards, so the switch must not wait on it.
//
// CanEnable runs first, before the dry-run branch: a preview of an enable
// ADR-0016 forbids would describe a change that must never happen.
func Enable(ctx context.Context, app App) error {
	if err := CanEnable(app.Family); err != nil {
		return err
	}
	return enableInternal(ctx, app)
}

// enableInternal writes the quadlet and starts the service once prerequisites are met.
func enableInternal(ctx context.Context, app App) error {
	path, err := UnitPath(app)
	if err != nil {
		return err
	}

	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would write %s for %s and start %s", path, app.Family.Image(), app.ServiceName())
		return nil
	}

	volDir, err := app.HostVolumeDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(volDir, 0o755); err != nil {
		return err
	}

	_, statErr := os.Stat(path)
	existed := statErr == nil
	rollback := func() {
		if existed {
			return
		}
		_ = os.Remove(path)
		_ = runSystemctl(ctx, "daemon-reload")
	}

	if err := writeAtomic(path, []byte(RenderUnit(app))); err != nil {
		return err
	}

	if err := runSystemctl(ctx, "daemon-reload"); err != nil {
		rollback()
		return err
	}
	if err := runSystemctl(ctx, "start", app.ServiceName()); err != nil {
		rollback()
		return err
	}
	return nil
}

// Disable stops the printer application and removes its quadlet. The pulled
// image and the per-app state under ~/printer-workspaces are left alone: they
// are large, they are expensive to re-fetch or reconfigure, and removing them
// is a disk-space decision the user did not make by turning a switch off.
func Disable(ctx context.Context, app App) error {
	path, err := UnitPath(app)
	if err != nil {
		return err
	}

	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would stop %s and remove %s", app.ServiceName(), path)
		return nil
	}

	if err := runSystemctl(ctx, "stop", app.ServiceName()); err != nil {
		if verifyErr := verifyStopped(ctx, app.ServiceName(), err); verifyErr != nil {
			return verifyErr
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return runSystemctl(ctx, "daemon-reload")
}

func verifyStopped(ctx context.Context, service string, stopErr error) error {
	state, err := runSystemctlOutput(ctx, "is-active", service)
	switch state = strings.TrimSpace(state); state {
	case "inactive", "failed", "unknown":
		return nil
	case "":
		return fmt.Errorf("%w; could not verify %s stopped: %v", stopErr, service, err)
	default:
		return fmt.Errorf("%w; %s is %s", stopErr, service, state)
	}
}
