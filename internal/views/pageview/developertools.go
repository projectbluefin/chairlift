package pageview

// DeveloperToolObservation is what the Features page knows about one optional
// IDE or terminal editor from its last Homebrew read.
type DeveloperToolObservation struct {
	// Supported reports whether the tool is published for this architecture.
	Supported bool
	// Homebrew reports whether this host can run Homebrew at all.
	Homebrew bool
	// ReadFailed means the installed inventory could not be read, so
	// whether the tool is present is unknown.
	ReadFailed bool
	// Installed means the last successful read listed the tool.
	Installed bool
}

// DeveloperToolView is one optional tool row's subtitle and Install button.
// Every button reads "Install" on screen, so AccessibleLabel names the tool:
// a screen reader moving between rows otherwise hears ten identical buttons.
type DeveloperToolView struct {
	Subtitle        string
	ButtonLabel     string
	AccessibleLabel string
	// Allowed reports whether the button may start an install.
	Allowed bool
}

// DeveloperTool presents one optional tool from its last observation. The
// description leads every subtitle so each row stays identifiable, and an
// unknown state never offers an install.
func DeveloperTool(name, description string, o DeveloperToolObservation) DeveloperToolView {
	view := DeveloperToolView{ButtonLabel: "Install", AccessibleLabel: "Install " + name}
	switch {
	case !o.Supported:
		view.Subtitle = description + " Not available for this architecture."
	case !o.Homebrew:
		view.Subtitle = description + " Needs Homebrew."
	case o.ReadFailed:
		view.Subtitle = description + " Could not check installed state; no installation is started."
	case o.Installed:
		view.Subtitle = description + " Installed through Homebrew."
		view.ButtonLabel = "Installed"
		view.AccessibleLabel = name + " installed"
	default:
		view.Subtitle = description + " Installs through Homebrew when you choose it."
		view.Allowed = true
	}
	return view
}

// DeveloperToolChecking is a row before its first Homebrew read completes.
func DeveloperToolChecking(name, description string) DeveloperToolView {
	return DeveloperToolView{
		Subtitle:        description + " Checking installed state…",
		ButtonLabel:     "Install",
		AccessibleLabel: "Install " + name,
	}
}

// DeveloperToolInstalling is a row while its install runs.
func DeveloperToolInstalling(name string) DeveloperToolView {
	return DeveloperToolView{
		Subtitle:        "Installing " + name + " through Homebrew…",
		ButtonLabel:     "Installing…",
		AccessibleLabel: "Installing " + name,
	}
}

// DeveloperToolUnverified is a row whose install finished but whose state
// could not be read back. The install was offered, so it stays offered.
func DeveloperToolUnverified(name, description string) DeveloperToolView {
	return DeveloperToolView{
		Subtitle:        description + " Could not verify installed state; the previous observation is kept.",
		ButtonLabel:     "Install",
		AccessibleLabel: "Install " + name,
		Allowed:         true,
	}
}
