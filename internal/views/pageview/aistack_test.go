package pageview

import (
	"testing"

	"github.com/projectbluefin/chairlift/internal/aistack"
)

// The chooser's responses stack bottom-up, so the recommended family must be
// added last and be the only suggested response; every family is offered once.
func TestAgentModePresetResponsesPromoteTheRecommendedFamily(t *testing.T) {
	responses := AgentModePresetResponses()
	families := aistack.Families()
	if len(responses) != len(families) {
		t.Fatalf("got %d responses, want one per family (%d)", len(responses), len(families))
	}
	last := responses[len(responses)-1]
	if last.ID != string(aistack.DefaultFamily) || !last.Suggested {
		t.Errorf("last response = %+v, want the suggested default family %q", last, aistack.DefaultFamily)
	}
	seen := map[string]bool{}
	for _, r := range responses {
		if seen[r.ID] {
			t.Errorf("family %q offered twice", r.ID)
		}
		seen[r.ID] = true
		if r.Suggested != (r.ID == string(aistack.DefaultFamily)) {
			t.Errorf("response %q Suggested = %v", r.ID, r.Suggested)
		}
		if r.Label != aistack.Family(r.ID).DisplayName() {
			t.Errorf("response %q label = %q", r.ID, r.Label)
		}
	}
	// Stacked in reverse, the rest read in Families' display order.
	for i, fam := range families[1:] {
		if got := responses[len(responses)-2-i].ID; got != string(fam) {
			t.Errorf("stack position %d = %q, want %q", i+1, got, fam)
		}
	}
}
