package imageinfo

import (
	"slices"
	"strings"
)

// GamingTarget returns the matching published Dakota image on the same stream
// and driver, without changing registry or family. Only the known reversible
// pairs are supported; a channel-table error or a removed publication blocks
// both directions. Already being on the requested image is not a switch.
func GamingTarget(cleanRef, tag string, enabled bool) (string, bool) {
	if SystemTableError() != "" {
		return "", false
	}
	switch tag {
	case "stable", "testing", "next", "btw":
	default:
		return "", false
	}

	base, driver := SplitDriver(cleanRef)
	var targetBase string
	switch base {
	case "ghcr.io/projectbluefin/dakota":
		if !enabled {
			return "", false
		}
		targetBase = "ghcr.io/projectbluefin/dakota-gaming"
	case "ghcr.io/projectbluefin/dakota-gaming":
		if enabled {
			return "", false
		}
		targetBase = "ghcr.io/projectbluefin/dakota"
	default:
		return "", false
	}
	if cleanRef != driverRef(base, driver) {
		return "", false
	}
	if driver != DriverStandard && driver != DriverNVIDIA {
		return "", false
	}
	for _, ref := range []string{base, targetBase} {
		published := false
		for _, entry := range activeDriverTable[ref] {
			if entry.driver == driver && slices.Contains(entry.streams, tag) {
				published = true
				break
			}
		}
		if !published {
			return "", false
		}
		// next/btw are deliberately unclassified by the channel table;
		// the driver publication table above covers those streams. Stable
		// and testing must also respect channel-table removals.
		if tag == "stable" || tag == "testing" {
			channels, ok := channelsFor(ref)
			if !ok || (!slices.Contains(channels.stableTags, tag) && !slices.Contains(channels.testingTags, tag)) {
				return "", false
			}
		}
	}
	return driverRef(targetBase, driver) + ":" + tag, true
}

// PendingImageSwitch inspects the booted and staged image references from bootc
// and reports whether a switch to a different published operating system image
// is staged for the next boot. A routine update staging (same repository and tag
// with a different digest) is not an image switch and returns false.
func PendingImageSwitch(bootedRef, stagedRef string) (target string, isGaming bool, pending bool) {
	if stagedRef == "" || bootedRef == "" {
		return "", false, false
	}
	bootedClean := stripDigest(stripTransport(bootedRef))
	stagedClean := stripDigest(stripTransport(stagedRef))
	if stagedClean == "" || bootedClean == "" || stagedClean == bootedClean {
		return "", false, false
	}
	stagedInfo := Info{Ref: stagedClean}
	return stagedClean, stagedInfo.IsGaming(), true
}

func stripDigest(ref string) string {
	if index := strings.Index(ref, "@"); index >= 0 {
		return ref[:index]
	}
	return ref
}
