// Package dockerbrew installs a rootful Docker engine from Homebrew bottles
// without ever letting root execute or load anything from the user-writable
// Homebrew prefix.
//
// The privileged helper fetches Homebrew's signed formula index, verifies it
// against the homebrew-1 key embedded here, picks the fixed daemon-side bottle
// set for this architecture, downloads each bottle straight from GHCR and
// checks it against the signed sha256, extracts a closed set of members into a
// root-owned staging directory, rewrites the Homebrew loader and RPATH
// placeholders to system and $ORIGIN-relative paths, smoke-runs the result,
// and only then swaps it into place. The Homebrew prefix is never read.
//
// The package is pure Go and imports no GTK bindings, so its tests run on any
// headless host.
package dockerbrew

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"sort"
	"strconv"
)

const (
	// Root is the root-owned install root: ChairLift's own state directory
	// under /var, the only root-writable tree on a bootc image (/usr is
	// read-only and /usr/local is a symlink into /var that may not exist).
	Root = "/var/lib/chairlift/docker"
	// Current is the symlink naming the active set directory under Root.
	Current = Root + "/current"
	// ManifestPath is the world-readable record of the active set.
	ManifestPath = Current + "/manifest.json"
	// UnitDir is where the managed system units are written.
	UnitDir = "/etc/systemd/system"
	// ServiceName is the engine's systemd service unit.
	ServiceName = "docker.service"
	// SocketName is the engine's systemd socket unit.
	SocketName = "docker.socket"
	// ImageUnitPath is the unit an image-provided Docker ships; when it exists
	// the image's Docker is used instead of Homebrew's.
	ImageUnitPath = "/usr/lib/systemd/system/docker.service"
	// ManagedMarker is the first line of every unit ChairLift writes, and the
	// only thing that makes a unit ChairLift's to overwrite or remove.
	ManagedMarker = "# Managed by chairlift-helper"
	// IndexURL is Homebrew's signed formula index.
	IndexURL = "https://formulae.brew.sh/api/formula.jws.json"
)

// The system-provided dependency docker-engine declares and that is not
// installed from Homebrew: dockerd finds iptables/nft from the OS.
const systemDependency = "nftables"

// Formulae is the fixed daemon-side set, in install order.
func Formulae() []string {
	return []string{"docker-engine", "containerd", "runc", "tini", "libpathrs", "libseccomp"}
}

// CLIFormulae is the unprivileged user-side set installed with homebrew.Install.
func CLIFormulae() []string {
	return []string{"docker", "docker-buildx", "docker-compose"}
}

// Bottle is one verified Homebrew bottle.
type Bottle struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Revision int    `json:"revision"`
	SHA256   string `json:"sha256"`
	URL      string `json:"url"`
}

// Dir is the bottle's top-level version directory: "<version>" or
// "<version>_<revision>".
func (b Bottle) Dir() string {
	if b.Revision > 0 {
		return b.Version + "_" + strconv.Itoa(b.Revision)
	}
	return b.Version
}

// Manifest records the installed set.
type Manifest struct {
	Arch    string   `json:"arch"`
	Bottles []Bottle `json:"bottles"`
}

// SetID is a short, order-independent hash of the bottle digests; it names
// the set directory under Root.
func (m Manifest) SetID() string {
	lines := make([]string, 0, len(m.Bottles))
	for _, b := range m.Bottles {
		lines = append(lines, b.Name+" "+b.SHA256+"\n")
	}
	sort.Strings(lines)
	h := sha256.New()
	h.Write([]byte(m.Arch + "\n"))
	for _, l := range lines {
		h.Write([]byte(l))
	}
	return hex.EncodeToString(h.Sum(nil))[:setIDLen]
}

const setIDLen = 16

// Arch maps the running architecture to Homebrew's bottle tag.
func Arch() (string, error) {
	return archFor(runtime.GOARCH)
}

func archFor(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64_linux", nil
	case "arm64":
		return "arm64_linux", nil
	}
	return "", fmt.Errorf("dockerbrew: no Homebrew bottles for architecture %q", goarch)
}

// Loader is the system dynamic loader for a bottle architecture, or "" for an
// unknown one.
func Loader(arch string) string {
	switch arch {
	case "x86_64_linux":
		return "/lib64/ld-linux-x86-64.so.2"
	case "arm64_linux":
		return "/lib/ld-linux-aarch64.so.1"
	}
	return ""
}

// Outdated reports whether latest names a different bottle for any installed
// formula, or a formula the installed set lacks. An empty latest carries no
// information and is never outdated.
func Outdated(installed Manifest, latest []Bottle) bool {
	if len(latest) == 0 {
		return false
	}
	have := make(map[string]string, len(installed.Bottles))
	for _, b := range installed.Bottles {
		have[b.Name] = b.SHA256
	}
	for _, b := range latest {
		if have[b.Name] != b.SHA256 {
			return true
		}
	}
	return false
}
