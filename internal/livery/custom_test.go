package livery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCustomFileIsInstalledForEverySurface covers the escape hatch on all
// three surfaces, which is what makes adding a mark the catalogs do not carry
// possible at all.
func TestCustomFileIsInstalledForEverySurface(t *testing.T) {
	const mark = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="currentColor" d="M1 1h4v4H1z"/></svg>`

	for name, surface := range map[string]Surface{"app-grid": AppGrid, "panel": Panel, "dock": Dock} {
		t.Run(name, func(t *testing.T) {
			useTempDataHome(t)
			newFakeCommands(t)

			src := filepath.Join(t.TempDir(), "mine.svg")
			if err := os.WriteFile(src, []byte(mark), 0o644); err != nil {
				t.Fatal(err)
			}

			if err := Apply(context.Background(), surface, Source{Kind: FromFile, Value: src}); err != nil {
				t.Fatalf("Apply: %v", err)
			}

			dest, err := IconPath(surface, CustomID)
			if err != nil {
				t.Fatal(err)
			}
			installed, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("custom mark was not installed: %v", err)
			}
			if string(installed) != mark {
				t.Error("installed bytes differ from the chosen file")
			}
		})
	}
}

// TestCustomSelectionSurvivesAReload asserts the custom sentinel is not
// repaired away as though it were a stale catalog id — which would silently
// drop the user back to a default the moment they reopened the page.
func TestCustomSelectionSurvivesAReload(t *testing.T) {
	if got := resolveID(CustomID, false); got != CustomID {
		t.Errorf("panel custom selection resolved to %q, want %q", got, CustomID)
	}
	if got := resolveCNCFID(CustomID); got != CustomID {
		t.Errorf("dock custom selection resolved to %q, want %q", got, CustomID)
	}
}

// TestCustomSourceIsUsedWhenSelected asserts each surface's source resolves to
// the user's file rather than a catalog entry once custom is chosen.
func TestCustomSourceIsUsedWhenSelected(t *testing.T) {
	state := State{
		AppGridSlug: CustomID, AppGridCustom: "/tmp/a.svg",
		PanelID: CustomID, PanelCustom: "/tmp/p.svg",
		DockID: CustomID, DockCustom: "/tmp/d.svg",
	}
	for name, got := range map[string]Source{
		"app-grid": state.AppGridSource(),
		"panel":    state.PanelSource(),
		"dock":     state.DockSource(),
	} {
		if got.Kind != FromFile {
			t.Errorf("%s source kind = %v, want FromFile", name, got.Kind)
		}
		if !strings.HasSuffix(got.Value, ".svg") {
			t.Errorf("%s source value = %q, want the chosen file", name, got.Value)
		}
	}
}
