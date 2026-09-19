// Package imageinfo parses the ublue-os image descriptor that Bluefin,
// Bluefin LTS, and Dakota ship at /usr/share/ublue-os/image-info.json, and
// derives from it the facts ChairLift's Bluefin-family feature rows need:
// which OS variant is running, which release channel it tracks, and the
// exact image reference a stable/testing channel switch must target.
//
// The package is pure: it reads from an io.Reader or an explicit path and
// performs no process execution, no privileged work, and no GTK calls. That
// keeps the whole variant matrix (Dakota, Bluefin, Bluefin LTS) covered by
// ordinary table tests on hosts that are none of those three.
package imageinfo

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// DescriptorPath is the fixed location of the ublue-os image descriptor on
// Bluefin, Bluefin LTS, and Dakota. It is read-only and world-readable, so
// ChairLift never needs privilege to consult it.
const DescriptorPath = "/usr/share/ublue-os/image-info.json"

// Info is the subset of image-info.json ChairLift consumes. Unknown keys in
// the descriptor are ignored so that a newer image format does not break
// parsing.
type Info struct {
	Name   string `json:"image-name"`
	Tag    string `json:"image-tag"`
	Ref    string `json:"image-ref"`
	Vendor string `json:"image-vendor"`
	Flavor string `json:"image-flavor"`
}

// Variant identifies which supported OS is running.
type Variant string

// The supported variants. VariantUnknown covers every other bootc image,
// including Aurora and uCore: ChairLift still renders read-only status for
// those, but hides the channel switch, because the tag mapping below is not
// known to be correct for them.
const (
	VariantDakota     Variant = "dakota"
	VariantBluefin    Variant = "bluefin"
	VariantBluefinLTS Variant = "bluefin-lts"
	VariantUnknown    Variant = "unknown"
)

// DisplayName returns the human-readable product name for a variant.
func (v Variant) DisplayName() string {
	switch v {
	case VariantDakota:
		return "Dakota"
	case VariantBluefin:
		return "Bluefin"
	case VariantBluefinLTS:
		return "Bluefin LTS"
	default:
		return "Unknown"
	}
}

// Supported reports whether the variant is one of the three ChairLift's
// Bluefin-family rows are validated against.
func (v Variant) Supported() bool {
	switch v {
	case VariantDakota, VariantBluefin, VariantBluefinLTS:
		return true
	default:
		return false
	}
}

// Channel is the release stream an image tag tracks.
type Channel string

// The release channels. ChannelUnknown means the running tag is outside the
// stable/testing mapping — a pinned digest, a personal build, or a tag this
// package has not been taught.
const (
	ChannelStable  Channel = "stable"
	ChannelTesting Channel = "testing"
	ChannelUnknown Channel = "unknown"
)

// imageChannels is the verified stable/testing tag surface of one published
// image. Both directions are stored explicitly rather than inverting one
// map, because the mapping is not one-to-one: projectbluefin/bluefin-lts
// publishes both "lts" and "stable" and both move to the single "testing"
// tag, so the reverse has to name a winner.
type imageChannels struct {
	// stableTags are the recognized non-testing stream tags. A tag listed
	// here but absent from toTesting is a stable stream with no testing
	// counterpart — recognized for display, not switchable.
	stableTags []string
	// testingTags are the recognized testing stream tags.
	testingTags []string
	// toTesting maps a stable tag to this image's testing tag.
	toTesting map[string]string
	// toStable maps a testing tag back to a stable tag.
	toStable map[string]string
}

// imageChannelMap is the per-image channel table, keyed by clean registry
// path. It is deliberately keyed on the image rather than the tag alone.
//
// bluefinctl's `bctl toggle-testing` (src/bluefinctl/cli.py) uses a tag-only
// map — {stable: testing, latest: testing, lts: lts-testing, lts-hwe:
// lts-hwe-testing} — and that map produces two references that do not exist.
// Verified against GHCR by manifest request on 2026-08-17:
//
//	ghcr.io/ublue-os/bluefin:latest                 200
//	ghcr.io/ublue-os/bluefin:stable                 200
//	ghcr.io/ublue-os/bluefin:gts                    200
//	ghcr.io/ublue-os/bluefin:beta                   200
//	ghcr.io/ublue-os/bluefin:lts                    200
//	ghcr.io/ublue-os/bluefin:lts-testing            200
//	ghcr.io/ublue-os/bluefin:lts-hwe                200
//	ghcr.io/ublue-os/bluefin:lts-hwe-testing        200
//	ghcr.io/ublue-os/bluefin:testing                404  <- bluefinctl targets this
//	ghcr.io/projectbluefin/bluefin-lts:lts          200
//	ghcr.io/projectbluefin/bluefin-lts:stable       200
//	ghcr.io/projectbluefin/bluefin-lts:testing      200
//	ghcr.io/projectbluefin/bluefin-lts:lts-testing  404  <- bluefinctl targets this
//	ghcr.io/projectbluefin/dakota:latest            200
//	ghcr.io/projectbluefin/dakota:stable            200
//	ghcr.io/projectbluefin/dakota:testing           200
//
// Do not collapse this back into a single tag-keyed map: a wrong target is
// not a cosmetic bug, it is a failed `bootc switch` on someone's OS.
//
// An image that is not in this table — a fork, a custom build, anything
// rebased by hand — resolves to no channel and no switch at all, rather than
// to a guessed tag suffix. Other images are added by shipping a channels.yml
// override rather than by editing this map; see channels.go.
var imageChannelMap = map[string]imageChannels{
	// Bluefin Stable and the LTS streams published on the same image. Only
	// the LTS streams have testing counterparts; latest/stable/gts/beta do
	// not, so a Bluefin Stable host correctly offers no channel switch.
	"ghcr.io/ublue-os/bluefin": {
		stableTags:  []string{"latest", "stable", "stable-daily", "gts", "beta", "lts", "lts-hwe"},
		testingTags: []string{"lts-testing", "lts-hwe-testing"},
		toTesting: map[string]string{
			"lts":     "lts-testing",
			"lts-hwe": "lts-hwe-testing",
		},
		toStable: map[string]string{
			"lts-testing":     "lts",
			"lts-hwe-testing": "lts-hwe",
		},
	},
	// Project Bluefin's own LTS image uses the bare "testing" tag, not the
	// "lts-testing" tag ublue-os/bluefin uses.
	"ghcr.io/projectbluefin/bluefin-lts": {
		stableTags:  []string{"lts", "stable"},
		testingTags: []string{"testing"},
		toTesting: map[string]string{
			"lts":    "testing",
			"stable": "testing",
		},
		// Both "lts" and "stable" lead to the same "testing" tag, so the
		// return trip cannot recover which one the host started on. It
		// picks "stable", which is published, rather than tracking origin
		// state ChairLift has no place to store.
		toStable: map[string]string{"testing": "stable"},
	},
	"ghcr.io/projectbluefin/dakota": {
		stableTags:  []string{"latest", "stable"},
		testingTags: []string{"testing"},
		toTesting: map[string]string{
			"latest": "testing",
			"stable": "testing",
		},
		toStable: map[string]string{"testing": "stable"},
	},
}

// channelsFor returns the channel table entry for a clean registry path. It
// resolves through the active table, which is the compiled-in
// imageChannelMap unless an administrator or image maintainer has supplied
// an override — see channels.go. Images beyond the three Bluefin-family ones
// (TunaOS, a downstream rebuild, a private registry) are added that way,
// without a code change.
func channelsFor(cleanRef string) (imageChannels, bool) {
	channels, ok := activeTable[cleanRef]
	return channels, ok
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Parse decodes an image descriptor. It returns an error for malformed JSON
// and for a descriptor carrying neither an image name nor an image ref,
// which is not usable for any decision this package makes.
func Parse(reader io.Reader) (Info, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Info{}, fmt.Errorf("reading image descriptor: %w", err)
	}

	var info Info
	if err := json.Unmarshal(data, &info); err != nil {
		return Info{}, fmt.Errorf("parsing image descriptor: %w", err)
	}
	if info.Name == "" && info.Ref == "" {
		return Info{}, fmt.Errorf("image descriptor has neither image-name nor image-ref")
	}
	return info, nil
}

// Load reads and parses the descriptor at path. A missing file yields an
// error wrapping fs.ErrNotExist, which callers use to detect a host that is
// not a ublue-os image at all.
func Load(path string) (Info, error) {
	file, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = file.Close() }()
	return Parse(file)
}

// Detect loads the descriptor from its fixed system path.
func Detect() (Info, error) {
	return Load(DescriptorPath)
}

// Variant identifies the running OS from the image name, falling back to the
// image ref. Bluefin LTS is matched before Bluefin because "bluefin-lts"
// contains "bluefin"; reversing the order would misclassify every LTS host.
func (i Info) Variant() Variant {
	haystack := strings.ToLower(i.Name + " " + i.Ref)
	switch {
	case strings.Contains(haystack, "bluefin-lts"):
		return VariantBluefinLTS
	case strings.Contains(haystack, "dakota"):
		return VariantDakota
	case strings.Contains(haystack, "bluefin"):
		return VariantBluefin
	default:
		return VariantUnknown
	}
}

// gamingFlavor is the value ublue-os writes to image-flavor on the images
// that ship the gaming stack preinstalled.
const gamingFlavor = "gaming"

// gamingSuffix is the image-name suffix those images carry, e.g.
// "dakota-gaming" and "dakota-nvidia-gaming".
const gamingSuffix = "-gaming"

// IsGaming reports whether the running image already ships the gaming stack.
//
// The gaming images (dakota-gaming, dakota-nvidia-gaming) carry Steam and the
// rest of the stack as system packages. ChairLift's gaming mode installs the
// same applications as user Flatpaks, so offering it here would install a
// second, shadowing copy of software the image already provides.
//
// Two signals are accepted because only the first is authoritative and it is
// not present on every descriptor: the descriptor's own image-flavor, and the
// "-gaming" suffix on the image name. Either one is enough. Both are compared
// case-insensitively, and the suffix is checked against the image name and
// the clean ref so a descriptor carrying only one of them still matches.
func (i Info) IsGaming() bool {
	if strings.EqualFold(strings.TrimSpace(i.Flavor), gamingFlavor) {
		return true
	}
	for _, name := range []string{i.Name, i.CleanRef()} {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), gamingSuffix) {
			return true
		}
	}
	return false
}

// CleanRef returns the image ref with any ostree/docker transport prefix and
// any trailing tag removed — e.g. "ghcr.io/projectbluefin/dakota". It
// mirrors bluefinctl's prefix stripping (core/system.py clean_image_ref plus
// cli.py's transport regex) so both tools switch the same registry path.
func (i Info) CleanRef() string {
	return stripTag(stripTransport(i.Ref))
}

// stripTransport removes a leading ostree/docker transport. It handles both
// the "scheme:transport://path" form ("ostree-image-signed:docker://ghcr.io/…")
// and the bare "scheme:path" form ("ostree-image-signed:ghcr.io/…").
func stripTransport(ref string) string {
	if index := strings.Index(ref, "://"); index >= 0 {
		return ref[index+len("://"):]
	}
	// No "://", so at most one leading "scheme:" remains. Only strip it when
	// the scheme looks like a transport word rather than a registry port
	// ("ghcr.io:443/…" must survive untouched), which means it contains no
	// "/" and no digits-only tail.
	if index := strings.Index(ref, ":"); index >= 0 && !strings.Contains(ref[:index], "/") {
		if isTransportScheme(ref[:index]) {
			return ref[index+1:]
		}
	}
	return ref
}

// isTransportScheme reports whether word is a lowercase, hyphenated
// transport name such as "docker" or "ostree-image-signed". A registry host
// ("ghcr.io") contains a dot and is rejected, so "ghcr.io:443/x" keeps its
// port.
func isTransportScheme(word string) bool {
	if word == "" {
		return false
	}
	for _, r := range word {
		if (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

// stripTag removes a trailing ":tag" from a registry path, leaving a
// registry port (":443" before a "/") in place.
func stripTag(ref string) string {
	index := strings.LastIndex(ref, ":")
	if index < 0 {
		return ref
	}
	if strings.Contains(ref[index:], "/") {
		return ref // the colon belongs to a host:port, not a tag
	}
	return ref[:index]
}

// hasTag reports whether ref carries a trailing image tag, e.g.
// "ghcr.io/org/img:latest". A colon that sits before the first slash is a
// registry port ("registry.example:5000/org/img"), which the channel-table
// validation accepts; only a trailing tag is rejected.
func hasTag(ref string) bool {
	if index := strings.LastIndex(ref, ":"); index >= 0 {
		return !strings.Contains(ref[index:], "/")
	}
	return false
}

// EffectiveTag returns the running image tag, falling back to the tag
// embedded in the image ref when the descriptor omits image-tag.
func (i Info) EffectiveTag() string {
	if i.Tag != "" {
		return i.Tag
	}
	ref := stripTransport(i.Ref)
	index := strings.LastIndex(ref, ":")
	if index < 0 || strings.Contains(ref[index:], "/") {
		return ""
	}
	return ref[index+1:]
}

// Channel reports the release stream the running tag tracks. It is resolved
// through the running image's own channel table, so a tag that is a testing
// stream on one image and absent from another is classified correctly for
// each. An image outside the table, or a tag outside that image's known
// streams (a pinned date tag, a personal build), is ChannelUnknown.
func (i Info) Channel() Channel {
	tag := i.EffectiveTag()
	if tag == "" {
		return ChannelUnknown
	}
	channels, ok := channelsFor(i.CleanRef())
	if !ok {
		return ChannelUnknown
	}
	switch {
	case contains(channels.testingTags, tag):
		return ChannelTesting
	case contains(channels.stableTags, tag):
		return ChannelStable
	default:
		return ChannelUnknown
	}
}

// TargetTag returns the tag to switch cleanRef to for the requested channel,
// given the currently running tag. ok is false — meaning the channel switch
// stays inert rather than targeting a guessed reference — for an unknown
// image, an unknown tag, a tag already on the requested channel, or a stable
// tag whose image publishes no testing counterpart.
//
// That last case is not hypothetical: it is every ghcr.io/ublue-os/bluefin
// host on latest, stable, gts, or beta.
func TargetTag(cleanRef, currentTag string, channel Channel) (string, bool) {
	if currentTag == "" {
		return "", false
	}
	channels, ok := channelsFor(cleanRef)
	if !ok {
		return "", false
	}

	switch channel {
	case ChannelTesting:
		if contains(channels.testingTags, currentTag) {
			return "", false // already on testing
		}
		target, ok := channels.toTesting[currentTag]
		return target, ok
	case ChannelStable:
		if contains(channels.stableTags, currentTag) {
			return "", false // already on stable
		}
		target, ok := channels.toStable[currentTag]
		return target, ok
	default:
		return "", false
	}
}

// SwitchTarget returns the complete image reference `bootc switch` must be
// given to move this host to channel — e.g.
// "ghcr.io/projectbluefin/bluefin-lts:lts-testing". ok is false when the
// running tag has no counterpart in that channel, or when the descriptor
// carries no usable registry path.
func (i Info) SwitchTarget(channel Channel) (string, bool) {
	ref := i.CleanRef()
	if ref == "" || !strings.Contains(ref, "/") {
		return "", false
	}
	tag, ok := TargetTag(ref, i.EffectiveTag(), channel)
	if !ok {
		return "", false
	}
	return ref + ":" + tag, true
}
