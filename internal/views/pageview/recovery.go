package pageview

import "strings"

// Powerwash is the pure-presentation side of the Powerwash detail view: the
// single named place a user returns to a previous version or performs an
// explicitly scoped reset. It is reached deliberately from Maintenance, so the
// strings here describe a destination, not a routine control.
//
// The bootc rollback row and the two reset rows all live in their own
// pageview functions (BootcRollbackRow, PowerwashRow, FactoryResetRow); this
// file only supplies the container-level copy that is new to Powerwash. That
// copy is derived from what the detail actually built, so the Maintenance
// entry never promises a control the detail does not hold.

// ResetAvailability is why the Powerwash detail does or does not hold the
// reset rows (reset_group). Configuration and host capability stay apart,
// like the update shell's sources: a reset an administrator turned off must
// not read as one this machine cannot perform, and vice versa.
type ResetAvailability int

const (
	// ResetOffered means reset_group is configured on and the host backs it.
	ResetOffered ResetAvailability = iota
	// ResetDisabledByConfig means the configuration turns reset_group off,
	// which is the shipped default.
	ResetDisabledByConfig
	// ResetUnsupported means reset_group is configured on but the host has
	// neither Flatpak nor Distrobox for Powerwash to act on.
	ResetUnsupported
)

// ResetAvailabilityFor classifies reset_group from its configured state and
// its composed (configuration and capability) state.
func ResetAvailabilityFor(configured, effective bool) ResetAvailability {
	switch {
	case !configured:
		return ResetDisabledByConfig
	case !effective:
		return ResetUnsupported
	default:
		return ResetOffered
	}
}

// RecoveryOffer is what the Powerwash detail currently shows. Rollback is
// true only once bootc has confirmed a previous deployment the helper can
// return to, because the Roll Back group is built hidden and revealed
// asynchronously.
type RecoveryOffer struct {
	Rollback bool
	Reset    bool
}

// RecoveryEntrySubtitle is the subtitle on the Maintenance page's Powerwash
// entry. It names only the jobs the detail actually offers, so a default
// configuration (reset_group off) on a host with no previous deployment does
// not promise a reset or a rollback it will not find there.
func RecoveryEntrySubtitle(offer RecoveryOffer) string {
	var jobs []string
	if offer.Rollback {
		jobs = append(jobs, "roll back to the previous system version")
	}
	if offer.Reset {
		jobs = append(jobs, "reset this machine")
	}
	if len(jobs) == 0 {
		return "Nothing to roll back to or reset on this system right now."
	}
	var phrase string
	switch len(jobs) {
	case 1:
		phrase = jobs[0]
	case 2:
		phrase = jobs[0] + " or " + jobs[1]
	default:
		phrase = strings.Join(jobs[:len(jobs)-1], ", ") + ", or " + jobs[len(jobs)-1]
	}
	return strings.ToUpper(phrase[:1]) + phrase[1:] + "."
}

// RecoveryPageDescription is the Powerwash detail page's heading
// description. When the reset rows are present their own group explains
// them, so the page says nothing; when they are absent it says why, so a
// page holding only Roll Back does not leave the user hunting for the
// reset the destination's name suggests.
func RecoveryPageDescription(reset ResetAvailability) string {
	switch reset {
	case ResetDisabledByConfig:
		return "Powerwash and Factory Reset are turned off in this computer's configuration."
	case ResetUnsupported:
		return "Powerwash and Factory Reset are not available on this system."
	default:
		return ""
	}
}
