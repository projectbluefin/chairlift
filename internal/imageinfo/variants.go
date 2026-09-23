package imageinfo

import (
	"fmt"
	"slices"
	"strings"
)

// Driver is the graphics-driver flavour of an image. Universal Blue publishes
// the NVIDIA driver as a separate image rather than layering it, so moving
// between them is a `bootc switch` to a different image *name* — where a
// channel switch changes the *tag*.
type Driver string

const (
	// DriverStandard is the base image, carrying the open-source drivers
	// the kernel provides for AMD and Intel.
	DriverStandard Driver = "standard"
	// DriverNVIDIA carries NVIDIA's proprietary driver.
	DriverNVIDIA Driver = "nvidia"
	// DriverNVIDIAOpen carries NVIDIA's open kernel modules, which are the
	// supported choice on Turing and newer.
	DriverNVIDIAOpen Driver = "nvidia-open"
)

// DisplayName returns the human-readable variant name.
func (d Driver) DisplayName() string {
	switch d {
	case DriverStandard:
		return "Standard"
	case DriverNVIDIA:
		return "NVIDIA (proprietary)"
	case DriverNVIDIAOpen:
		return "NVIDIA (open modules)"
	default:
		return string(d)
	}
}

// suffix returns the image-name suffix for a variant.
func (d Driver) suffix() string {
	if d == DriverStandard {
		return ""
	}
	return "-" + string(d)
}

// variantStreams records which streams a variant is published for, because
// availability is not uniform across a family — the trap a family-level
// variant list walks straight into.
//
// Verified against GHCR by manifest request on 2026-08-17, for
// ghcr.io/ublue-os/bluefin:
//
//	image                    latest stable gts  beta lts  lts-hwe
//	bluefin                  200    200    200  200  200  200
//	bluefin-nvidia           200    200    200  200  404  404
//	bluefin-nvidia-open      200    200    200  200  404  404
//	bluefin-dx               200    200    200  200  200  200
//	bluefin-dx-nvidia        200    200    200  200  404  404
//	bluefin-asus             200    404    404  404  404  404
//	bluefin-surface          200    404    404  404  404  404
//
// So an LTS host has **no** NVIDIA variant: the driver images are published
// for the latest/stable/gts streams only. finupdate's KNOWN_FAMILIES models
// variants per family rather than per stream and would offer
// `bluefin-nvidia:lts`, which is a 404. ChairLift keys on both.
//
// Re-probed 2026-09-21: `bluefin:beta` is now 404, so the beta column is
// gone from the table below. `bluefin-nvidia:beta` still answers 200, but
// a host that switched to it could not switch back, so it is not offered.
// `stable-daily` was confirmed 200 on the base and on both NVIDIA variants
// and is now listed for all three.
//
// For ghcr.io/projectbluefin/dakota, re-probed on 2026-09-21 (the full
// table is recorded above imageChannelMap in imageinfo.go): the base image
// publishes latest, stable, testing, next, and btw, while `dakota-nvidia`
// publishes every one of those except latest — that tag was still live on
// 2026-08-17 and now answers 404, so it is no longer claimed here.
// `dakota-dx` and `dakota-nvidia-open` are not published at all.
//
// The gaming flavour is its own base image: `dakota-gaming` and
// `dakota-nvidia-gaming` both publish stable, testing, next, and btw. Note
// the name shape — the driver suffix sits *before* the flavour suffix, so
// the NVIDIA variant of `dakota-gaming` is `dakota-nvidia-gaming`, never
// `dakota-gaming-nvidia`, which is a 404. driverRef composes it.
//
// ghcr.io/projectbluefin/bluefin-lts publishes no variants.
type driverStreams struct {
	// driver is the flavour these streams belong to.
	driver Driver
	// streams are the tags this variant is published for. A tag absent here
	// means the variant does not exist for that stream, and ChairLift must
	// not offer it.
	streams []string
}

// imageDriverMap is the built-in per-base-image variant table, keyed by the
// clean registry path of the *base* image. Deriving the base from a variant
// image is baseImage's job.
//
// Adding an image here is not enough on its own — a driver is only offered
// when the running stream appears in its list. An administrator adds images
// without rebuilding ChairLift through the `drivers:` section of
// channels.yml; see LoadTable.
var imageDriverMap = map[string][]driverStreams{
	"ghcr.io/ublue-os/bluefin": {
		// No "beta" anywhere: ghcr.io/ublue-os/bluefin:beta answered 404 on
		// 2026-09-21. bluefin-nvidia:beta is still 200, but with no base
		// image on that stream a host that took the switch could not come
		// back, so the variant is not offered either.
		{driver: DriverStandard, streams: []string{"latest", "stable", "stable-daily", "gts", "lts", "lts-hwe", "lts-testing", "lts-hwe-testing"}},
		{driver: DriverNVIDIA, streams: []string{"latest", "stable", "stable-daily", "gts"}},
		{driver: DriverNVIDIAOpen, streams: []string{"latest", "stable", "stable-daily", "gts"}},
	},
	"ghcr.io/projectbluefin/dakota": {
		{driver: DriverStandard, streams: []string{"latest", "stable", "testing", "next", "btw"}},
		// No "latest": ghcr.io/projectbluefin/dakota-nvidia:latest answered
		// 404 on 2026-09-21, so a dakota:latest host has no driver choice.
		{driver: DriverNVIDIA, streams: []string{"stable", "testing", "next", "btw"}},
	},
	// The gaming images are their own base/variant pair; see driverRef for
	// why the composed name is dakota-nvidia-gaming, not dakota-gaming-nvidia.
	"ghcr.io/projectbluefin/dakota-gaming": {
		{driver: DriverStandard, streams: []string{"stable", "testing", "next", "btw"}},
		{driver: DriverNVIDIA, streams: []string{"stable", "testing", "next", "btw"}},
	},
	// projectbluefin/bluefin-lts publishes no driver variants; listing the
	// base explicitly is what makes "no variant to offer" a known answer
	// rather than an unknown image.
	"ghcr.io/projectbluefin/bluefin-lts": {
		{driver: DriverStandard, streams: []string{"lts", "stable", "testing"}},
	},
}

// knownDrivers are every driver suffix the table can produce, longest
// first so that "nvidia-open" is matched before "nvidia".
var knownDrivers = []Driver{DriverNVIDIAOpen, DriverNVIDIA}

// flavorSuffixes are image-name suffixes that a published driver variant
// keeps *after* its driver suffix, so they cannot be treated as part of the
// base name when one is composed or taken apart.
//
// There is one: the gaming flavour. ghcr.io/projectbluefin/dakota-gaming's
// NVIDIA variant is published as dakota-nvidia-gaming; appending the driver
// suffix to the end instead yields dakota-gaming-nvidia, which answered 404
// on 2026-09-21.
var flavorSuffixes = []string{gamingSuffix}

// splitFlavor separates a clean registry path from the trailing flavour
// suffix its driver variants carry at the end of the name.
func splitFlavor(cleanRef string) (stem, flavor string) {
	for _, candidate := range flavorSuffixes {
		if strings.HasSuffix(cleanRef, candidate) {
			return strings.TrimSuffix(cleanRef, candidate), candidate
		}
	}
	return cleanRef, ""
}

// driverRef composes the published image name for a driver flavour of base,
// inserting the driver suffix ahead of any flavour suffix.
func driverRef(base string, driver Driver) string {
	stem, flavor := splitFlavor(base)
	return stem + driver.suffix() + flavor
}

// SplitDriver separates a clean registry path into its base image and the
// variant its name encodes. An unrecognized suffix is not treated as a
// variant: `bluefin-asus` is a hardware-specific image ChairLift does not
// manage, and mistaking "asus" for a driver flavour would offer a switch
// that drops the machine's hardware support.
func SplitDriver(cleanRef string) (base string, driver Driver) {
	stem, flavor := splitFlavor(cleanRef)
	for _, candidate := range knownDrivers {
		suffix := candidate.suffix()
		if !strings.HasSuffix(stem, suffix) {
			continue
		}
		// Only accept the split if the remainder is an image the table
		// knows, so `bluefin-dx-nvidia` — a variant of an image ChairLift
		// does not manage — is left alone.
		trimmed := strings.TrimSuffix(stem, suffix) + flavor
		if _, ok := activeDriverTable[trimmed]; ok {
			return trimmed, candidate
		}
	}
	return cleanRef, DriverStandard
}

// AvailableDrivers returns the variants published for cleanRef's base image
// on the given tag, in table order. It returns nil for an image outside the
// table, which is the signal to offer no variant switch at all.
func AvailableDrivers(cleanRef, tag string) []Driver {
	base, _ := SplitDriver(cleanRef)
	entries, ok := activeDriverTable[base]
	if !ok || tag == "" {
		return nil
	}

	available := make([]Driver, 0, len(entries))
	for _, entry := range entries {
		if slices.Contains(entry.streams, tag) {
			available = append(available, entry.driver)
		}
	}
	if len(available) < 2 {
		// One variant is no choice at all.
		return nil
	}
	return available
}

// DriverTarget returns the full image reference for switching cleanRef to
// the requested variant on the same tag. ok is false when the variant is not
// published for that image and stream — which is every LTS host asking for
// NVIDIA, and every image outside the table.
func DriverTarget(cleanRef, tag string, driver Driver) (string, bool) {
	if tag == "" {
		return "", false
	}
	base, current := SplitDriver(cleanRef)
	if current == driver {
		return "", false // already there
	}
	if !slices.Contains(driverNames(AvailableDrivers(cleanRef, tag)), string(driver)) {
		return "", false
	}
	return fmt.Sprintf("%s:%s", driverRef(base, driver), tag), true
}

func driverNames(drivers []Driver) []string {
	names := make([]string, 0, len(drivers))
	for _, driver := range drivers {
		names = append(names, string(driver))
	}
	return names
}

// Driver returns the graphics-driver flavour of the running image.
func (i Info) Driver() Driver {
	_, driver := SplitDriver(i.CleanRef())
	return driver
}

// RecommendedDriver returns the driver flavour this machine's hardware wants,
// and whether switching to it is both possible and worthwhile.
//
// hasNVIDIA comes from internal/gpu. The recommendation is deliberately
// one-directional and narrow: a machine with an NVIDIA card on a standard
// image is offered the NVIDIA image, because that is the case where the
// hardware does not work as well as it could. ChairLift does not push a
// machine *off* an NVIDIA image — someone running one deliberately on a
// machine whose card was removed does not need to be nagged — and it does
// not choose between the proprietary and open modules, because that depends
// on the GPU generation and getting it wrong leaves an unbootable desktop.
func (i Info) RecommendedDriver(hasNVIDIA bool) (Driver, bool) {
	if !hasNVIDIA {
		return DriverStandard, false
	}
	if i.Driver() != DriverStandard {
		return i.Driver(), false // already on a driver image
	}
	if _, ok := DriverTarget(i.CleanRef(), i.EffectiveTag(), DriverNVIDIA); !ok {
		return DriverStandard, false // not published for this stream
	}
	return DriverNVIDIA, true
}
