package pageview

import "testing"

func TestSelfCaskRecognizesOnlyThisApplication(t *testing.T) {
	for token, want := range map[string]bool{
		"chairlift":              true,
		"ublue-os/tap/chairlift": true,
		"chairlift-nightly":      false,
		"goose-linux":            false,
		"mychairlift":            false,
		"":                       false,
	} {
		if got := IsSelfCask(token); got != want {
			t.Errorf("IsSelfCask(%q) = %v, want %v", token, got, want)
		}
	}
}
