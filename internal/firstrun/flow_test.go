package firstrun

import (
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/navigation"
)

func TestPagesPreservesWizardOrderAndFiltersUnavailable(t *testing.T) {
	all := navigation.VisibleItems(func(string, string) bool { return true })
	want := []string{"features", "applications", "agents", "livery"}
	if got := Pages(all); !reflect.DeepEqual(got, want) {
		t.Fatalf("Pages = %v, want %v", got, want)
	}
	var subset []navigation.Item
	for _, item := range all {
		if item.Name != "features" && item.Name != "agents" {
			subset = append(subset, item)
		}
	}
	if got := Pages(subset); !reflect.DeepEqual(got, []string{"applications", "livery"}) {
		t.Fatalf("filtered Pages = %v", got)
	}
	if got := Pages(navigation.VisibleItems(nil)); len(got) != 0 {
		t.Fatalf("closed floor Pages = %v", got)
	}
}

func TestOrdinaryLaunchNeverPresentsSetup(t *testing.T) {
	if ShouldPresent(false) {
		t.Fatal("ordinary launch presents setup")
	}
	if !ShouldPresent(true) {
		t.Fatal("explicit launch does not present setup")
	}
}
