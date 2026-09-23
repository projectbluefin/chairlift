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
	"slices"
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

	// BootedRef is the reference the machine actually booted, recorded at
	// boot rather than baked at build time. It is not part of the
	// descriptor: Detect fills it from BootedImagePath, and it stays empty
	// on a host that does not publish the marker. See EffectiveTag for why
	// the baked image-tag cannot be trusted on its own.
	BootedRef string `json:"-"`
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
// Every tag above was re-probed on 2026-09-21, and the dakota family was
// extended, because the alpha's own host runs
// ghcr.io/projectbluefin/dakota-gaming — a separately published image that
// no entry covered, so a gaming host was offered no channel switch at all:
//
//	ghcr.io/ublue-os/bluefin:latest                      200
//	ghcr.io/ublue-os/bluefin:stable                      200
//	ghcr.io/ublue-os/bluefin:stable-daily                200
//	ghcr.io/ublue-os/bluefin:gts                         200
//	ghcr.io/ublue-os/bluefin:lts                         200
//	ghcr.io/ublue-os/bluefin:lts-hwe                     200
//	ghcr.io/ublue-os/bluefin:lts-testing                 200
//	ghcr.io/ublue-os/bluefin:lts-hwe-testing             200
//	ghcr.io/ublue-os/bluefin:beta                        404  <- was 200 on 2026-08-17
//	ghcr.io/ublue-os/bluefin:testing                     404
//	ghcr.io/ublue-os/bluefin-nvidia:latest               200
//	ghcr.io/ublue-os/bluefin-nvidia:stable               200
//	ghcr.io/ublue-os/bluefin-nvidia:stable-daily         200
//	ghcr.io/ublue-os/bluefin-nvidia:gts                  200
//	ghcr.io/ublue-os/bluefin-nvidia:beta                 200  <- base is 404, so unreachable
//	ghcr.io/ublue-os/bluefin-nvidia:lts                  404
//	ghcr.io/ublue-os/bluefin-nvidia:lts-hwe              404
//	ghcr.io/ublue-os/bluefin-nvidia-open:latest          200
//	ghcr.io/ublue-os/bluefin-nvidia-open:stable          200
//	ghcr.io/ublue-os/bluefin-nvidia-open:stable-daily    200
//	ghcr.io/ublue-os/bluefin-nvidia-open:gts             200
//	ghcr.io/ublue-os/bluefin-nvidia-open:beta            404
//	ghcr.io/ublue-os/bluefin-nvidia-open:lts             404
//	ghcr.io/ublue-os/bluefin-nvidia-open:lts-hwe         404
//	ghcr.io/projectbluefin/bluefin-lts:lts               200
//	ghcr.io/projectbluefin/bluefin-lts:stable            200
//	ghcr.io/projectbluefin/bluefin-lts:testing           200
//	ghcr.io/projectbluefin/bluefin-lts:lts-testing       404
//	ghcr.io/projectbluefin/dakota:latest                 200
//	ghcr.io/projectbluefin/dakota:stable                 200
//	ghcr.io/projectbluefin/dakota:testing                200
//	ghcr.io/projectbluefin/dakota:next                   200
//	ghcr.io/projectbluefin/dakota:btw                    200
//	ghcr.io/projectbluefin/dakota:gts                    404
//	ghcr.io/projectbluefin/dakota:beta                   404
//	ghcr.io/projectbluefin/dakota-nvidia:latest          404  <- was 200 on 2026-08-17
//	ghcr.io/projectbluefin/dakota-nvidia:stable          200
//	ghcr.io/projectbluefin/dakota-nvidia:testing         200
//	ghcr.io/projectbluefin/dakota-nvidia:next            200
//	ghcr.io/projectbluefin/dakota-nvidia:btw             200
//	ghcr.io/projectbluefin/dakota-gaming:latest          404  <- the descriptor's own tag
//	ghcr.io/projectbluefin/dakota-gaming:stable          200
//	ghcr.io/projectbluefin/dakota-gaming:testing         200
//	ghcr.io/projectbluefin/dakota-gaming:next            200
//	ghcr.io/projectbluefin/dakota-gaming:btw             200
//	ghcr.io/projectbluefin/dakota-nvidia-gaming:latest   404
//	ghcr.io/projectbluefin/dakota-nvidia-gaming:stable   200
//	ghcr.io/projectbluefin/dakota-nvidia-gaming:testing  200
//	ghcr.io/projectbluefin/dakota-nvidia-gaming:next     200
//	ghcr.io/projectbluefin/dakota-nvidia-gaming:btw      200
//
// Three results from that pass are load-bearing rather than trivia.
//
// "beta" is retired. It was 200 on 2026-08-17 and is 404 now, so it is gone
// from the bluefin entry below and from the driver table in variants.go —
// bluefin-nvidia:beta still resolves, but with no base image to return to
// it is a one-way trip and must not be offered.
//
// "next" and "btw" are the same image, not two streams: they resolved to
// one digest on every dakota repository probed, and projectbluefin/dakota's
// publish.yml pushes :btw as an alias immediately after :next, for builds
// of the `next` branch only ("rolling GNOME master / bleeding edge"). That
// makes them a third stream below testing, not a stable or testing
// counterpart of anything, so neither is classified here — a guess in
// either direction would be the tag-keyed-map mistake in another form.
//
// The gaming images publish no "latest", yet this machine's
// /usr/share/ublue-os/image-info.json declares image-tag "latest" for
// ghcr.io/projectbluefin/dakota-gaming — a tag GHCR answered 404 for on
// 2026-09-21. That is not a table problem and must not be papered over by
// listing a dead tag. It is a Dakota build property: promotion retags an
// existing digest instead of rebuilding, so every Dakota image bakes
// "latest" whatever stream it ships on, which projectbluefin/dakota states
// in elements/bluefin/common.bst. The machine's real stream comes from the
// boot-time marker instead — see EffectiveTag and BootedImagePath.
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
	// the LTS streams have testing counterparts; latest/stable/stable-daily/gts
	// do not, so a Bluefin Stable host correctly offers no channel switch.
	// "beta" is gone: it was 200 on 2026-08-17 and 404 on 2026-09-21.
	"ghcr.io/ublue-os/bluefin": {
		stableTags:  []string{"latest", "stable", "stable-daily", "gts", "lts", "lts-hwe"},
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
	// The gaming flavour is a separate published image, not a tag of the
	// base one, and its name does not reduce to "dakota" — so without its
	// own row a gaming host matched no entry and was offered no switch.
	// It publishes "stable" and "testing" but no "latest".
	"ghcr.io/projectbluefin/dakota-gaming": {
		stableTags:  []string{"stable"},
		testingTags: []string{"testing"},
		toTesting:   map[string]string{"stable": "testing"},
		toStable:    map[string]string{"testing": "stable"},
	},
}

// channelsFor returns the channel table entry for a clean registry path. It
// resolves through the active table, which is the compiled-in
// imageChannelMap unless an administrator or image maintainer has supplied
// an override — see channels.go. Images beyond the Bluefin-family ones the
// table names (TunaOS, a downstream rebuild, a private registry) are added
// that way, without a code change.
func channelsFor(cleanRef string) (imageChannels, bool) {
	channels, ok := activeTable[cleanRef]
	return channels, ok
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

// BootedImagePath is the fixed path ublue-booted-image.service writes the
// booted image reference to at boot. It is root-owned, world-readable, and
// lives on tmpfs, so reading it needs no privilege and cannot be forged by
// an unprivileged user — which matters, because the privileged helper
// resolves its `bootc switch` target through the same Info this produces.
//
// It exists because the descriptor's baked image-tag is not a reliable
// statement of the running stream on every image. See EffectiveTag.
const BootedImagePath = "/run/ublue-os/booted-image"

// Detect loads the descriptor from its fixed system path and overlays the
// boot-time reference when the host records one. Both the GUI and the
// privileged helper call this, so both see the same running stream.
func Detect() (Info, error) {
	info, err := Load(DescriptorPath)
	if err != nil {
		return info, err
	}
	info.BootedRef = readBootedRef(BootedImagePath)
	return info, nil
}

// readBootedRef returns the first line of the boot-time marker, or "" when
// the host does not publish one. A missing marker is the ordinary case on
// every image that bakes its tag correctly, so it is not an error.
func readBootedRef(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	// The marker holds one reference. Bound the read so a corrupt or
	// substituted file cannot make startup allocate without limit.
	data, err := io.ReadAll(io.LimitReader(file, 1024))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
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

// EffectiveTag returns the running image tag: the boot-time marker's tag
// when the host records one for this same image, then the descriptor's
// image-tag, then the tag embedded in the image ref.
//
// The marker comes first because the baked image-tag is not a per-stream
// value on every image. Dakota promotes a stream by retagging an existing
// digest rather than rebuilding, so every Dakota image — base, nvidia, and
// gaming — bakes image-tag "latest" whatever stream it is published on.
// (projectbluefin/dakota states this in elements/bluefin/common.bst, where
// it patches fastfetch to read the same marker.) Trusting the baked value
// on a gaming host claims the tag "latest", which that image does not
// publish at all — observed 2026-09-21 on a dakota-gaming machine whose
// descriptor said "latest" while the marker said
// ghcr.io/projectbluefin/dakota-gaming:testing.
//
// The marker is only honoured for the image the descriptor names, so a
// stale marker left by a previous deployment of a different image cannot
// move this host onto another image's streams. A marker that names this
// image by digest rather than by tag yields no tag at all, which is the
// honest answer for a pinned host: no stream, and no switch offered.
func (i Info) EffectiveTag() string {
	if tag, ok := i.bootedTag(); ok {
		return tag
	}
	if i.Tag != "" {
		return i.Tag
	}
	return tagOf(stripTransport(i.Ref))
}

// bootedTag returns the tag recorded by the boot-time marker. ok is false
// when there is no usable marker for this image, which is every host that
// does not publish one.
func (i Info) bootedTag() (string, bool) {
	ref := stripTransport(i.BootedRef)
	if ref == "" {
		return "", false
	}
	// A digest-pinned marker names no stream. Report that rather than
	// letting the baked tag answer for it.
	if index := strings.LastIndex(ref, "@"); index >= 0 {
		return "", stripTag(ref[:index]) == i.CleanRef()
	}
	if stripTag(ref) != i.CleanRef() {
		return "", false
	}
	return tagOf(ref), true
}

// tagOf returns the trailing image tag of a transport-free reference, or ""
// when it carries none.
func tagOf(ref string) string {
	if !hasTag(ref) {
		return ""
	}
	return ref[strings.LastIndex(ref, ":")+1:]
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
	case slices.Contains(channels.testingTags, tag):
		return ChannelTesting
	case slices.Contains(channels.stableTags, tag):
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
		if slices.Contains(channels.testingTags, currentTag) {
			return "", false // already on testing
		}
		target, ok := channels.toTesting[currentTag]
		return target, ok
	case ChannelStable:
		if slices.Contains(channels.stableTags, currentTag) {
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
