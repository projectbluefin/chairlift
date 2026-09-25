package pageview

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/aistack"
)

func TestAgentModeGroupDescriptionAddressesCloudAndPromptRetention(t *testing.T) {
	d := AgentModeGroupDescription()
	for _, want := range []string{"cloud", "not saved"} {
		if !strings.Contains(d, want) {
			t.Errorf("description %q does not mention %q", d, want)
		}
	}
}

// Every state reads differently, so a degraded or kept-but-off Agent Mode
// can never be mistaken for ready.
func TestAgentModeSubtitleDistinguishesEveryState(t *testing.T) {
	seen := map[string]aistack.State{}
	for s := aistack.StateUnavailable; s <= aistack.StateDisabled; s++ {
		text := AgentModeSubtitle(s)
		if prev, dup := seen[text]; dup {
			t.Errorf("states %d and %d share subtitle %q", prev, s, text)
		}
		seen[text] = s
		if s != aistack.StateReady && strings.HasPrefix(text, "Ready") {
			t.Errorf("state %d claims ready: %q", s, text)
		}
	}
}

// #262: environment changes never reach running processes, and the text
// must not pretend they do.
func TestAgentModeReadySaysRunningAppsMustRestart(t *testing.T) {
	if got := AgentModeSubtitle(aistack.StateReady); !strings.Contains(got, "restart") {
		t.Errorf("ready subtitle does not say open apps must restart: %q", got)
	}
}

func TestAgentModeDisabledSaysModelsWereKept(t *testing.T) {
	if got := AgentModeSubtitle(aistack.StateDisabled); !strings.Contains(got, "kept") {
		t.Errorf("disabled subtitle implies downloads were discarded: %q", got)
	}
}

// A failed stop keeps the unit because the service may still run, so the text
// has to keep saying it is running.
func TestAgentModeFailedStopDoesNotClaimItStopped(t *testing.T) {
	for name, text := range map[string]string{"subtitle": AgentModeFailureSubtitle(false), "toast": AgentModeFailureToast(false)} {
		lower := strings.ToLower(text)
		if !strings.Contains(lower, "still running") || !strings.Contains(lower, "nothing was removed") {
			t.Errorf("%s = %q", name, text)
		}
	}
}

// A failed enable rolls back the unit but not what Homebrew installed, so it
// must not say nothing changed.
func TestAgentModeFailedEnableDoesNotClaimNothingChanged(t *testing.T) {
	for _, text := range []string{AgentModeFailureSubtitle(true), AgentModeFailureToast(true)} {
		if strings.Contains(strings.ToLower(text), "nothing") {
			t.Errorf("failed enable claims nothing changed: %q", text)
		}
	}
}

func TestAgentModeDetailsGiveTheAddressAndGateJan(t *testing.T) {
	find := func(rows []Row, title string) string {
		for _, r := range rows {
			if r.Title == title {
				return r.Subtitle
			}
		}
		return ""
	}
	if got := find(AgentModeDetails(true), "Address for other apps"); got != "127.0.0.1:17434" {
		t.Errorf("address row = %q", got)
	}
	if got := find(AgentModeDetails(true), "Chat app"); got != "Jan" {
		t.Errorf("x86_64 chat row = %q", got)
	}
	if got := find(AgentModeDetails(false), "Chat app"); strings.Contains(got, "Jan") {
		t.Errorf("non-x86_64 host offered Jan: %q", got)
	}
}

func TestAgentModeActiveModelAndPresetStrings(t *testing.T) {
	if got := AgentModeActiveModelTitle(); got == "" {
		t.Error("AgentModeActiveModelTitle is empty")
	}
	if got := AgentModeActiveModelSubtitle(""); !strings.Contains(got, "Qwen") {
		t.Errorf("AgentModeActiveModelSubtitle(\"\") = %q, want default Qwen", got)
	}
	if got := AgentModeActiveModelSubtitle("custom/model:latest"); got != "custom/model:latest" {
		t.Errorf("AgentModeActiveModelSubtitle() = %q, want custom/model:latest", got)
	}
	if got := AgentModePresetsTitle(); got == "" {
		t.Error("AgentModePresetsTitle is empty")
	}
	if got := AgentModePresetsSubtitle(); !strings.Contains(got, "Qwen") {
		t.Errorf("AgentModePresetsSubtitle() = %q, want mention of models", got)
	}
	if got := AgentModeSwitchPresetLabel(); got == "" {
		t.Error("AgentModeSwitchPresetLabel is empty")
	}
}
