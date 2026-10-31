package pageview

import (
	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/livery"
)

// WallpaperGroupTitle labels the optional Wallpapers group.
const WallpaperGroupTitle = "Wallpapers"

// WallpaperGroupFragment introduces what the group can do. It names the
// channel selector, the rotation switch, and Damask together, because the
// group only exists when at least one of them is usable.
const WallpaperGroupFragment = "Choose a wallpaper channel, rotate it at login, or manage live feeds with Damask."

// WallpaperRotationRow is the Wallpapers group's rotation switch text.
//
// A source build gets a different subtitle, because the login unit it writes
// points at the binary's current path — the same reason the Livery rotation
// switch carries one. That path is stable for an installed build and
// disposable for a source one.
func WallpaperRotationRow(systemInstall bool) Row {
	if !systemInstall {
		return Row{
			Title:    "Rotate Wallpaper at Login",
			Subtitle: "Stops working if this copy of " + branding.AppName + " is moved or reinstalled.",
		}
	}
	return Row{
		Title:    "Rotate Wallpaper at Login",
		Subtitle: "Show the next image in the channel each time you log in.",
	}
}

// WallpaperRotationAvailable reports whether the rotation switch may operate.
//
// Rotation writes GNOME's background keys, so it needs a desktop that owns
// them, and it advances a channel's images, so a channel must be selected.
// Without either, leaving the switch sensitive would offer a setting that
// applies nothing.
func WallpaperRotationAvailable(gnomeAvailable, channelSelected bool) bool {
	return gnomeAvailable && channelSelected
}

// WallpaperSelectorItem is one channel in the thumbnail selector.
type WallpaperSelectorItem struct {
	ID       string
	Name     string
	Selected bool
}

// WallpaperSelectorRows is the thumbnail selector's model: every discovered
// channel in catalog order, with the confirmed selection marked. An empty
// catalog yields no rows, so the selector is hidden rather than drawn empty.
func WallpaperSelectorRows(selectedID string) []WallpaperSelectorItem {
	out := make([]WallpaperSelectorItem, 0, len(livery.WallpaperChannels()))
	for _, channel := range livery.WallpaperChannels() {
		out = append(out, WallpaperSelectorItem{
			ID:       channel.ID,
			Name:     channel.Name,
			Selected: channel.ID == selectedID,
		})
	}
	return out
}

// DamaskRow describes the single stateful Damask control.
//
// Damask is an external Flatpak, not a ChairLift action, so the row's title
// never changes — only what activating it does. When installed the control
// launches the manager; when absent it installs it. The subtitle says which,
// and the action row's label (pageview.DamaskActionName) says what clicking
// does.
func DamaskRow(installed bool) Row {
	if installed {
		return Row{Title: "Damask", Subtitle: "Manage live wallpaper feeds from Bing, Wallhaven, and more."}
	}
	return Row{Title: "Damask", Subtitle: "Install a live wallpaper manager."}
}

// DamaskUnknownRow describes the Damask row when the install probe failed.
// It offers no action: installing over an unknown state could fail on an app
// that is already present.
func DamaskUnknownRow() Row {
	return Row{Title: "Damask", Subtitle: "Couldn't check whether Damask is installed."}
}

// WallpaperUnknownRow describes the read-only row the Wallpapers group shows
// when it could not probe whether GNOME owns the desktop picture. It offers no
// selector or rotation switch: the user is told the state is unknown rather
// than the feature being silently hidden as unsupported.
func WallpaperUnknownRow() Row {
	return Row{Title: "Wallpapers", Subtitle: "Couldn't check whether GNOME owns the desktop picture."}
}

// DamaskActionName is the label the single Damask control shows. It is
// "Open" when Damask is installed and "Install" when it is not, so the
// control is one action with two states rather than two competing buttons.
func DamaskActionName(installed bool) string {
	if installed {
		return "Open"
	}
	return "Install"
}

// DamaskState is what the Wallpapers group can offer for Damask on this host.
type DamaskState int

const (
	// DamaskHidden means Damask is neither installed nor installable
	// (no Flatpak), so the group shows no Damask control at all.
	DamaskHidden DamaskState = iota
	// DamaskInstallable means Flatpak is present but Damask is not.
	DamaskInstallable
	// DamaskInstalled means the Damask Flatpak is installed.
	DamaskInstalled
	// DamaskUnknown means Flatpak is present but the install probe failed,
	// so the group says it could not tell rather than offering an install
	// for an app that may already be there.
	DamaskUnknown
)

// DamaskOffer reports which Damask control, if any, the group may show. A
// host without Flatpak can neither install nor run Damask, so it gets none. A
// failed probe on a Flatpak host is unknown, never installable.
func DamaskOffer(installed, probeFailed, flatpakAvailable bool) DamaskState {
	switch {
	case installed:
		return DamaskInstalled
	case probeFailed && flatpakAvailable:
		return DamaskUnknown
	case flatpakAvailable:
		return DamaskInstallable
	default:
		return DamaskHidden
	}
}

// WallpaperGroupVisible reports whether the optional Wallpapers group is
// built: when the Homebrew wallpaper collection is configured or a Damask
// control can be offered (installed, or installable through Flatpak). Damask
// does not depend on Homebrew, so a host without the collection still gets the
// group when Flatpak can install or open Damask. A host with neither keeps
// Livery without an empty Wallpapers group.
func WallpaperGroupVisible(collection bool, damask DamaskState) bool {
	return collection || damask != DamaskHidden
}
