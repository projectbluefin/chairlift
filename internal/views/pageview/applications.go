package pageview

import "fmt"

// HomebrewSearchLabel is the accessible name of the Apps page's Homebrew
// search entry. The entry sits in a row titled only "Search", which assistive
// technology does not associate with the entry, so it must be named itself.
const HomebrewSearchLabel = "Search Homebrew packages"

// HomebrewSearchPlaceholder is the hint shown in the empty Homebrew search
// entry.
const HomebrewSearchPlaceholder = "Search command line tools and apps"

// FlatpakUninstallConfirmation returns the title and body of the dialog a
// Flatpak application's remove button must show before anything is removed.
// The body names the installation the app leaves: a system-wide removal
// takes it away from every account on the computer, not only the caller's.
func FlatpakUninstallConfirmation(name string, userScope bool) (title, body string) {
	title = fmt.Sprintf("Uninstall %s?", name)
	if userScope {
		return title, fmt.Sprintf("Removes %s from your account. Its settings and data in your home folder are kept.", name)
	}
	return title, fmt.Sprintf("Removes %s for everyone who uses this computer. Their settings and data are kept. You may be asked for an administrator password.", name)
}
