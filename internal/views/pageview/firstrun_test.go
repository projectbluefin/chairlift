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
	if GetMovingDescription == "" {
		t.Error("GetMovingDescription must not be empty")
	}
}
