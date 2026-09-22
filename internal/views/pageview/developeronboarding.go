package pageview

// DeveloperOnboardingLink is one destination ChairLift offers when Developer
// Mode is switched on.
type DeveloperOnboardingLink struct {
	Title string
	URL   string
}

// developerOnboardingLinks is the ordered set of tabs a live enable opens.
//
// The set lives here, in a package that imports no puregotk, so the exact
// collection and its order can be asserted headlessly — see
// docs/agents/skills/gtk-headless-tests.md. Order is part of the contract:
// tabs open left to right in this order, so the developer documentation the
// user is expected to read first is listed first.
//
// The training catalog is named as the redirect target rather than the
// Linux Foundation host it lands on, because training.projectbluefin.io is
// the stable address and the redirect destination is not.
var developerOnboardingLinks = []DeveloperOnboardingLink{
	{
		Title: "Bluefin Developer Documentation",
		URL:   "https://docs.projectbluefin.io/bluefin-dx/",
	},
	{
		Title: "Project Bluefin Training Catalog",
		URL:   "https://training.projectbluefin.io",
	},
	{
		Title: "GNOME Developer Center",
		URL:   "https://developer.gnome.org/",
	},
}

// DeveloperOnboardingTargets returns the onboarding destinations a Developer
// Mode toggle should open, in the order they should be opened.
//
// Opening three tabs at once is deliberate but potentially distracting, so
// the admission rule is deliberately narrow. Every one of the three
// arguments must line up:
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
