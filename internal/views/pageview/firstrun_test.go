package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/firstrun"
)

func TestWelcomeViewModelConstructsProperDefaults(t *testing.T) {
	vm := NewWelcomeViewModel(false)

	if vm.Title != WelcomeTitle {
		t.Errorf("Title = %q, want %q", vm.Title, WelcomeTitle)
	}
	if vm.Subtitle != WelcomeSubtitle {
		t.Errorf("Subtitle = %q, want %q", vm.Subtitle, WelcomeSubtitle)
	}
	if vm.PrimaryButtonText != ConfigureEverythingAction {
		t.Errorf("PrimaryButtonText = %q, want %q", vm.PrimaryButtonText, ConfigureEverythingAction)
	}
	if vm.SecondaryButtonText != GetMovingAction {
		t.Errorf("SecondaryButtonText = %q, want %q", vm.SecondaryButtonText, GetMovingAction)
	}
	if !vm.DefaultIsPrimary {
		t.Error("DefaultIsPrimary should be true for recommended flow")
	}
	if vm.DinosaurAsset != firstrun.AssetDinosaur {
		t.Errorf("DinosaurAsset = %q, want %q", vm.DinosaurAsset, firstrun.AssetDinosaur)
	}
	if vm.WordmarkAsset != firstrun.AssetWordmarkLight {
		t.Errorf("WordmarkAsset for light theme = %q, want %q", vm.WordmarkAsset, firstrun.AssetWordmarkLight)
	}
}

func TestWelcomeViewModelThemeWordmarkSelection(t *testing.T) {
	lightVM := NewWelcomeViewModel(false)
	if lightVM.WordmarkAsset != firstrun.AssetWordmarkLight {
		t.Errorf("light wordmark = %q, want %q", lightVM.WordmarkAsset, firstrun.AssetWordmarkLight)
	}

	darkVM := NewWelcomeViewModel(true)
	if darkVM.WordmarkAsset != firstrun.AssetWordmarkDark {
		t.Errorf("dark wordmark = %q, want %q", darkVM.WordmarkAsset, firstrun.AssetWordmarkDark)
	}
}

func TestGetMovingToastMessageContainsAppNameAndCommand(t *testing.T) {
	msg := GetMovingToastMessage()
	if !strings.Contains(msg, branding.AppName) {
		t.Errorf("toast message %q does not contain AppName %q", msg, branding.AppName)
	}
	if !strings.Contains(msg, "chairlift") {
		t.Errorf("toast message %q does not mention chairlift command", msg)
	}
	// Issue #265 specifies "by running `chairlift`." — a space before the
	// period reads as a typo in a toast the user cannot dismiss and reread.
	if strings.Contains(msg, " .") {
		t.Errorf("toast message %q has a stray space before the period", msg)
	}
	if !strings.Contains(msg, "Application Menu") {
		t.Errorf("toast message %q does not mention Application Menu", msg)
	}
}

func TestWelcomeActionHeadingsAndSubtitlesAreNonEmpty(t *testing.T) {
	if WelcomeTitle == "" {
		t.Error("WelcomeTitle must not be empty")
	}
	if WelcomeSubtitle == "" {
		t.Error("WelcomeSubtitle must not be empty")
	}
	if ConfigureEverythingAction == "" {
		t.Error("ConfigureEverythingAction must not be empty")
	}
	if ConfigureEverythingDescription == "" {
		t.Error("ConfigureEverythingDescription must not be empty")
	}
	if GetMovingAction == "" {
		t.Error("GetMovingAction must not be empty")
	}
	if GetMovingDescription() == "" {
		t.Error("GetMovingDescription must not be empty")
	}
	if ConfigStepInfoSubtitle() == "" {
		t.Error("ConfigStepInfoSubtitle must not be empty")
	}
}

// TestFirstRunCopyNamesTheProductThroughBranding holds the branding package's
// ownership of the user-facing product name: a literal here drifts silently on
// rename, because installcheck's display-name gate only covers the code name.
func TestFirstRunCopyNamesTheProductThroughBranding(t *testing.T) {
	for name, text := range map[string]string{
		"GetMovingDescription":   GetMovingDescription(),
		"ConfigStepInfoSubtitle": ConfigStepInfoSubtitle(),
		"GetMovingToastMessage":  GetMovingToastMessage(),
	} {
		if !strings.Contains(text, branding.AppName) {
			t.Errorf("%s() = %q, does not name branding.AppName %q", name, text, branding.AppName)
		}
	}

	for name, format := range map[string]string{
		"GetMovingDescriptionFormat":   GetMovingDescriptionFormat,
		"ConfigStepInfoSubtitleFormat": ConfigStepInfoSubtitleFormat,
		"GetMovingToastFormat":         GetMovingToastFormat,
	} {
		if !strings.Contains(format, "%s") {
			t.Errorf("%s = %q, must interpolate the product name", name, format)
		}
	}
}

// TestStepForwardActionNamesWhatTheClickDoes covers the label the forward
// button carries: "Finish" on an intermediate step promises a completion the
// click does not deliver, because the assistant shows the next step instead.
func TestStepForwardActionNamesWhatTheClickDoes(t *testing.T) {
	if got := StepForwardAction(false); got != NextStepAction {
		t.Errorf("StepForwardAction(false) = %q, want %q", got, NextStepAction)
	}
	if got := StepForwardAction(true); got != FinishAction {
		t.Errorf("StepForwardAction(true) = %q, want %q", got, FinishAction)
	}
}

func TestNavigationAndCompletionCopyIsNonEmpty(t *testing.T) {
	for name, text := range map[string]string{
		"BackAction":            BackAction,
		"NextStepAction":        NextStepAction,
		"FinishAction":          FinishAction,
		"SetupCompletedMessage": SetupCompletedMessage,
	} {
		if text == "" {
			t.Errorf("%s must not be empty", name)
		}
	}
}

// TestWelcomeCopyIsReExportedFromFirstrun keeps the welcome screen's copy
// owned by one package; a restated literal here drifts from the step.
func TestWelcomeCopyIsReExportedFromFirstrun(t *testing.T) {
	if WelcomeTitle != firstrun.StepWelcome.Title {
		t.Errorf("WelcomeTitle = %q, want %q", WelcomeTitle, firstrun.StepWelcome.Title)
	}
	if WelcomeSubtitle != firstrun.StepWelcome.Description {
		t.Errorf("WelcomeSubtitle = %q, want %q", WelcomeSubtitle, firstrun.StepWelcome.Description)
	}
}
