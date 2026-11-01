package pageview

import "fmt"

// WallpaperGroupTitle labels the optional Livery Wallpapers group.
const WallpaperGroupTitle = "Wallpapers"

// WallpaperGroupFragment introduces the group. It names both things it can
// hold, because either one alone builds it: the Homebrew collection needs
// Homebrew, Damask needs Flatpak.
const WallpaperGroupFragment = "Curated wallpaper collections and wallpaper apps for your desktop."

// DamaskState is what the Wallpapers group can offer for the external Damask
// wallpaper manager on this host.
type DamaskState int

const (
	// DamaskHidden means Damask is neither installed nor installable (no
	// Flatpak), so the group shows no Damask control.
	DamaskHidden DamaskState = iota
	// DamaskInstallable means Flatpak is present and Damask is not.
	DamaskInstallable
	// DamaskInstalled means the Damask Flatpak is installed.
	DamaskInstalled
	// DamaskUnknown means the install probe failed. The control offers to
	// check again instead of installing over an app that may already exist.
	DamaskUnknown
)

// DamaskOffer reports which Damask control the group may show. A failed probe
// is unknown, never absent and never installable; a host without Flatpak can
// neither install nor run Damask, so it gets no control.
func DamaskOffer(installed, probeFailed, flatpakAvailable bool) DamaskState {
	switch {
	case installed:
		return DamaskInstalled
	case probeFailed:
		return DamaskUnknown
	case flatpakAvailable:
		return DamaskInstallable
	default:
		return DamaskHidden
	}
}

// DamaskRow describes the single Damask row. The title never changes; the
// subtitle says what the one button will do in this state.
func DamaskRow(state DamaskState) Row {
	switch state {
	case DamaskInstalled:
		return Row{Title: "Damask", Subtitle: "Manage wallpaper feeds from Bing, Wallhaven, NASA and more."}
	case DamaskUnknown:
		return Row{Title: "Damask", Subtitle: "Couldn't check whether Damask is installed."}
	default:
		return Row{Title: "Damask", Subtitle: "Install a wallpaper manager for online feeds."}
	}
}

// DamaskActionName is the label of the single stateful Damask button, so
// install and open are one control with distinct states rather than two
// competing buttons. An unknown probe is retried, not guessed.
func DamaskActionName(state DamaskState) string {
	switch state {
	case DamaskInstalled:
		return "Open"
	case DamaskUnknown:
		return "Try Again"
	default:
		return "Install"
	}
}

// DamaskFailure is the error toast for a failed Damask action. The control
// keeps its last confirmed state, so every failure is retryable.
func DamaskFailure(state DamaskState) string {
	switch state {
	case DamaskInstalled:
		return "Couldn't open Damask. Try again."
	case DamaskUnknown:
		return "Couldn't check whether Damask is installed. Try again."
	default:
		return "Couldn't install Damask. Try again."
	}
}

// WallpaperChannelsState is what the Wallpapers group can offer for choosing
// a picture from the installed collections.
type WallpaperChannelsState int

const (
	// WallpaperChannelsHidden means no picture can be offered: the desktop is
	// not GNOME, GNOME's background schema is absent, or no collection is
	// installed. Nothing about channels is shown.
	WallpaperChannelsHidden WallpaperChannelsState = iota
	// WallpaperChannelsAvailable means GNOME takes the background and at least
	// one installed collection has pictures to pick.
	WallpaperChannelsAvailable
	// WallpaperChannelsUnknown means a check failed. The group says so and
	// offers to check again rather than reading the failure as unsupported.
	WallpaperChannelsUnknown
)

// WallpaperChannelsOffer decides the channel selector's state from the desktop
// probe and the catalog read. A failed check is never "unsupported", and an
// unreadable catalog never hides the channels that were read.
func WallpaperChannelsOffer(supported, probeFailed bool, channels int, discoveryFailed bool) WallpaperChannelsState {
	switch {
	case probeFailed && (channels > 0 || discoveryFailed):
		return WallpaperChannelsUnknown
	case !supported:
		return WallpaperChannelsHidden
	case channels > 0:
		return WallpaperChannelsAvailable
	case discoveryFailed:
		return WallpaperChannelsUnknown
	default:
		return WallpaperChannelsHidden
	}
}

// WallpaperChannelsRow describes the selector's row in each visible state.
func WallpaperChannelsRow(state WallpaperChannelsState) Row {
	if state == WallpaperChannelsUnknown {
		return Row{Title: "Wallpaper Channels", Subtitle: "Couldn't check which wallpapers this desktop can use."}
	}
	return Row{Title: "Wallpaper Channel", Subtitle: "Pick a picture to make it your desktop background."}
}

// WallpaperRetryLabel is the unknown state's button.
const WallpaperRetryLabel = "Try Again"

// WallpaperSelectedLabel announces the badge on the current picture.
const WallpaperSelectedLabel = "Current wallpaper"

// WallpaperApplyFailure is the error toast for a failed selection. The tiles
// keep the last confirmed picture marked and stay pickable, so it is a retry.
const WallpaperApplyFailure = "Couldn't set the wallpaper. Try again."

// WallpaperCount is a channel's size, for its subtitle.
func WallpaperCount(n int) string {
	if n == 1 {
		return "1 wallpaper"
	}
	return fmt.Sprintf("%d wallpapers", n)
}

// WallpaperChannelSubtitle is the selector's subtitle for the channel on
// screen. When a catalog could not be read, it says so: the list shown is
// then not every collection on this computer.
func WallpaperChannelSubtitle(count int, incomplete bool) string {
	if incomplete {
		return WallpaperCount(count) + " · Some collections couldn't be read."
	}
	return WallpaperCount(count)
}

// WallpaperRotationTitle labels the opt-in rotation switch.
const WallpaperRotationTitle = "Rotate Wallpapers"

// WallpaperRotationView is the observed rotation state the switch's row
// describes. It carries display values only, so the wording is testable
// without GTK.
type WallpaperRotationView struct {
	// On means the user turned rotation on and the timer is installed.
	On bool
	// ChannelName is the rotating collection's name, or empty when unknown.
	ChannelName string
	// Scheduled means the user manager reports the timer enabled and active.
	Scheduled bool
	// Next is the formatted next change, or empty when unknown.
	Next string
	// LastRunFailed means the most recent change did not succeed.
	LastRunFailed bool
	// CheckFailed means the state could not be read.
	CheckFailed bool
}

// WallpaperRotationSubtitle says what rotation is doing, from what the user
// manager reported. It never claims a schedule that was not observed: a timer
// that is installed but not running, or whose last change failed, says so
// and points at the switch as the retry.
func WallpaperRotationSubtitle(v WallpaperRotationView, shownChannel string) string {
	switch {
	case v.CheckFailed:
		return "Couldn't check wallpaper rotation. Turn it off and on to try again."
	case !v.On:
		return fmt.Sprintf("Change to the next picture in %s once a day.", shownChannel)
	case !v.Scheduled:
		return "Rotation is turned on but isn't scheduled. Turn it off and on to try again."
	case v.LastRunFailed:
		return "The last wallpaper change failed. Check that the collection is still installed."
	}
	channel := v.ChannelName
	if channel == "" {
		channel = "a collection"
	}
	if v.Next != "" {
		return fmt.Sprintf("Rotating through %s daily · Next change %s", channel, v.Next)
	}
	return fmt.Sprintf("Rotating through %s daily.", channel)
}

// WallpaperRotationFailure is the error toast for a failed toggle. The switch
// returns to the observed state, so it is a retry.
func WallpaperRotationFailure(enable bool) string {
	if enable {
		return "Couldn't turn on wallpaper rotation. Try again."
	}
	return "Couldn't turn off wallpaper rotation. Try again."
}
