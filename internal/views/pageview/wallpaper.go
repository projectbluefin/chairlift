package pageview

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
