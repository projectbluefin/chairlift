package imageinfo

import "slices"

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
