package pageview

// DeveloperOnboardingLink is one destination ChairLift offers when Developer
// Mode is switched on.
type DeveloperOnboardingLink struct {
	Title string
	URL   string
}

// developerOnboardingLinks is what a live enable opens: one page.
//
// The set lives here, in a package that imports no puregotk, so the exact
// collection can be asserted headlessly — see
// docs/agents/skills/gtk-headless-tests.md.
//
// It once held three links (this page, the training catalog, and the GNOME
// Developer Center), and flipping the switch opened all three at once; on a
// desktop with no default browser yet that stacked three app-choosers
// (#494). One switch flip opens at most one page now. The Bluefin developer
// documentation is the one kept because it describes exactly what the
// switch just changed and is the hub that links onward to the training and
// platform material; opening nothing would leave a new developer with no
// pointer at all.
var developerOnboardingLinks = []DeveloperOnboardingLink{
	{
		Title: "Bluefin Developer Documentation",
		URL:   "https://docs.projectbluefin.io/bluefin-dx/",
	},
}

// DeveloperOnboardingTargets returns the onboarding destination a Developer
// Mode toggle should open — never more than one.
//
// Opening a browser from a switch is a side effect, so the admission rule is
// deliberately narrow. Every one of the three arguments must line up:
//
//   - dryRun is true when the process is previewing under --dry-run. The
//     capture run is headless and must not spawn browser processes, so a
//     preview opens nothing even though the switch reports a live enable.
//   - enabled is the requested new state. Switching Developer Mode *off* is
//     a clean no-op for browser tabs; the links are onboarding material for
//     a developer who has just joined the groups, not a parting gift.
//   - succeeded is false when the group promotion returned an error. A
//     failed enable leaves the account unchanged, so pointing the user at
//     developer material would describe a state they are not in.
//
// A confirmed live enable is therefore the only combination that produces a
// non-empty slice, which is what pageview's headless tests assert. The
// launcher wiring itself cannot be tested here — internal/views imports
// puregotk — so this function is the seam those tests reach through.
//
// The result is a fresh slice: the caller iterates it on the GTK main
// thread, and a copy keeps the package-level table immutable from there.
func DeveloperOnboardingTargets(dryRun, enabled, succeeded bool) []DeveloperOnboardingLink {
	if dryRun || !enabled || !succeeded {
		return nil
	}

	targets := make([]DeveloperOnboardingLink, len(developerOnboardingLinks))
	copy(targets, developerOnboardingLinks)
	return targets
}
