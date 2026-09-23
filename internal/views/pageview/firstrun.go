package pageview

import (
	"fmt"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/firstrun"
)

const (
	// WelcomeTitle is the prominent header on the onboarding hero screen.
	WelcomeTitle = "Welcome to Bluefin"

	// WelcomeSubtitle is the welcoming narrative copy explaining the system's nature.
	WelcomeSubtitle = "Your cloud-native developer workstation is ready. Choose how you'd like to get started."

	// ConfigureEverythingAction is the label for the recommended default action.
	ConfigureEverythingAction = "Configure Everything"

	// ConfigureEverythingDescription describes proceeding into optional decision steps.
	ConfigureEverythingDescription = "Proceed into optional onboarding steps to customize your appearance, apps, and developer tools."

	// GetMovingAction is the label for immediately exiting the wizard to the desktop.
	GetMovingAction = "Get Moving"

	// GetMovingDescription describes skipping the wizard while affirming Control Center availability.
	GetMovingDescription = "Jump straight to your desktop. You can open Control Center anytime from the Application Menu or terminal."

	// GetMovingToastFormat is the template for the reassuring exit toast.
	GetMovingToastFormat = "You're ready to go! You can launch %s anytime from the Application Menu or by running chairlift ."
)

// GetMovingToastMessage returns the affirming status toast text.
func GetMovingToastMessage() string {
	return fmt.Sprintf(GetMovingToastFormat, branding.AppName)
}

// WelcomeViewModel models the presentation state of the first-run welcome screen.
type WelcomeViewModel struct {
	Title                  string
	Subtitle               string
	PrimaryButtonText      string
	PrimaryButtonTooltip   string
	SecondaryButtonText    string
	SecondaryButtonTooltip string
	DefaultIsPrimary       bool
	DinosaurAsset          string
	WordmarkAsset          string
}

// NewWelcomeViewModel returns a configured WelcomeViewModel for light or dark palettes.
func NewWelcomeViewModel(isDark bool) WelcomeViewModel {
	wordmark := firstrun.AssetWordmarkLight
	if isDark {
		wordmark = firstrun.AssetWordmarkDark
	}

	return WelcomeViewModel{
		Title:                  WelcomeTitle,
		Subtitle:               WelcomeSubtitle,
		PrimaryButtonText:      ConfigureEverythingAction,
		PrimaryButtonTooltip:   ConfigureEverythingDescription,
		SecondaryButtonText:    GetMovingAction,
		SecondaryButtonTooltip: GetMovingDescription,
		DefaultIsPrimary:       true,
		DinosaurAsset:          firstrun.AssetDinosaur,
		WordmarkAsset:          wordmark,
	}
}
