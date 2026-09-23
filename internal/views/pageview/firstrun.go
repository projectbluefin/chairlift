package pageview

import (
	"fmt"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/firstrun"
)

const (
	// WelcomeTitle is the prominent header on the onboarding hero screen.
	// The welcome step and the hero screen are one screen, so the copy is
	// owned by internal/firstrun and re-exported here.
	WelcomeTitle = firstrun.WelcomeStepTitle

	// WelcomeSubtitle is the welcoming narrative copy explaining the system's nature.
	WelcomeSubtitle = firstrun.WelcomeStepDescription

	// ConfigureEverythingAction is the label for the recommended default action.
	ConfigureEverythingAction = "Configure Everything"

	// ConfigureEverythingDescription describes proceeding into optional decision steps.
	ConfigureEverythingDescription = "Proceed into optional onboarding steps to customize your appearance, apps, and developer tools."

	// GetMovingAction is the label for immediately exiting the wizard to the desktop.
	GetMovingAction = "Get Moving"

	// GetMovingDescriptionFormat is the template describing the skip action;
	// the product name is interpolated from branding.AppName.
	GetMovingDescriptionFormat = "Jump straight to your desktop. You can open %s anytime from the Application Menu or terminal."

	// ConfigStepInfoTitle heads the reassurance row on a configuration step.
	ConfigStepInfoTitle = "Onboarding Configuration"

	// ConfigStepInfoSubtitleFormat is the template reassuring the user the
	// choice is not final; the product name comes from branding.AppName.
	ConfigStepInfoSubtitleFormat = "Configuration for this step can be adjusted at any time in %s."

	// GetMovingToastFormat is the template for the reassuring exit toast.
	GetMovingToastFormat = "You're ready to go! You can launch %s anytime from the Application Menu or by running chairlift."

	// BackAction labels the configuration step's reverse navigation button.
	BackAction = "Back"

	// NextStepAction labels the forward button while configuration steps remain.
	NextStepAction = "Next"

	// FinishAction labels the forward button on the final configuration step.
	FinishAction = "Finish"

	// SetupCompletedMessage is the toast confirming the assistant finished.
	SetupCompletedMessage = "Setup completed!"
)

// StepForwardAction names the forward navigation button for a configuration
// step. finishes reports whether clicking it concludes setup rather than
// moving onto a further step.
//
// The button advances through the remaining steps before it finishes setup,
// so labeling it "Finish" on an intermediate step misdescribes the click the
// user is about to make.
func StepForwardAction(finishes bool) string {
	if finishes {
		return FinishAction
	}
	return NextStepAction
}

// GetMovingDescription describes skipping the wizard while affirming the
// application remains available afterwards.
func GetMovingDescription() string {
	return fmt.Sprintf(GetMovingDescriptionFormat, branding.AppName)
}

// ConfigStepInfoSubtitle returns the reassurance subtitle for a configuration step.
func ConfigStepInfoSubtitle() string {
	return fmt.Sprintf(ConfigStepInfoSubtitleFormat, branding.AppName)
}

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
		SecondaryButtonTooltip: GetMovingDescription(),
		DefaultIsPrimary:       true,
		DinosaurAsset:          firstrun.AssetDinosaur,
		WordmarkAsset:          wordmark,
	}
}
