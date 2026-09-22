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

			var dest string
			if surface == Panel {
				pDir, dirErr := panelIconDir()
				if dirErr != nil {
					t.Fatal(dirErr)
				}
				entries, readErr := os.ReadDir(pDir)
				if readErr != nil {
					t.Fatal(readErr)
				}
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), PanelIconPrefix) {
						dest = filepath.Join(pDir, entry.Name())
						break
					}
				}
			} else {
				p, pathErr := IconPath(surface, CustomID)
				if pathErr != nil {
					t.Fatal(pathErr)
				}
				dest = p
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

func TestCustomPanelReplacementUpdatesIconNameAndPrunesOldFile(t *testing.T) {
	useTempDataHome(t)
	cmds := newFakeCommands(t)

	src := filepath.Join(t.TempDir(), "custom.svg")
	markA := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M1 1h4v4H1z"/></svg>`
	if err := os.WriteFile(src, []byte(markA), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Apply(context.Background(), Panel, Source{Kind: FromFile, Value: src}); err != nil {
		t.Fatalf("Apply markA: %v", err)
	}
	var valA string
	prefix := "gsettings set " + extensionSchema + " " + extensionIconKey + " "
	for _, call := range cmds.calls {
		if strings.HasPrefix(call, prefix) {
			valA = strings.TrimPrefix(call, prefix)
		}
	}
	if valA == "" {
		t.Fatalf("gsettings %s not set after Apply markA; calls: %v", extensionIconKey, cmds.calls)
	}

	pDir, err := panelIconDir()
	if err != nil {
		t.Fatal(err)
	}
	entriesA, err := os.ReadDir(pDir)
	if err != nil {
		t.Fatal(err)
	}
	var filesA []string
	for _, e := range entriesA {
		if strings.HasPrefix(e.Name(), PanelIconPrefix) {
			filesA = append(filesA, e.Name())
		}
	}
	if len(filesA) != 1 {
		t.Fatalf("expected 1 panel icon file, got %v", filesA)
	}

	markB := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M2 2h8v8H2z"/></svg>`
	if err := os.WriteFile(src, []byte(markB), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Apply(context.Background(), Panel, Source{Kind: FromFile, Value: src}); err != nil {
		t.Fatalf("Apply markB: %v", err)
	}
	var valB string
	for _, call := range cmds.calls {
		if strings.HasPrefix(call, prefix) {
			valB = strings.TrimPrefix(call, prefix)
		}
	}
	if valB == "" {
		t.Fatalf("gsettings %s not set after Apply markB; calls: %v", extensionIconKey, cmds.calls)
	}
	if valA == valB {
		t.Errorf("expected different menuicon-setting values to emit changed signal, got same %q", valA)
	}

	entriesB, err := os.ReadDir(pDir)
	if err != nil {
		t.Fatal(err)
	}
	var filesB []string
	for _, e := range entriesB {
		if strings.HasPrefix(e.Name(), PanelIconPrefix) {
			filesB = append(filesB, e.Name())
		}
	}
	if len(filesB) != 1 {
		t.Fatalf("expected old panel icon to be pruned, got %v", filesB)
	}
	if filesA[0] == filesB[0] {
		t.Errorf("expected new icon filename for markB, got same %q", filesA[0])
	}
}

// TestCustomPathWithAnApostropheRoundTrips pins the read side of the
// free-form *-custom-path keys. GVariant prints a string containing an
// apostrophe double-quoted, so /home/o'brien/mark.svg comes back from
// `gsettings list-recursively` as "/home/o'brien/mark.svg" — and a reader
// that only strips single quotes hands Apply a path that does not exist.
func TestCustomPathWithAnApostropheRoundTrips(t *testing.T) {
	const path = `/home/o'brien/mark.svg`

	f := newFakeCommands(t)
	detectGamingOriginal := detectGaming
	detectGaming = func() bool { return false }
	t.Cleanup(func() { detectGaming = detectGamingOriginal })
	f.reply["gsettings list-recursively "+SchemaID] = strings.Join([]string{
		SchemaID + " " + KeyPanelCustom + ` "` + path + `"`,
		SchemaID + " " + KeyDockCustom + ` "/home/o\'brien/dock.svg"`,
		SchemaID + " " + KeyAppGridCustom + ` '/home/plain/app.svg'`,
	}, "\n")

	state, err := Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.PanelCustom != path {
		t.Errorf("panel custom path = %q, want %q", state.PanelCustom, path)
	}
	if want := `/home/o'brien/dock.svg`; state.DockCustom != want {
		t.Errorf("dock custom path = %q, want %q", state.DockCustom, want)
	}
	if want := "/home/plain/app.svg"; state.AppGridCustom != want {
		t.Errorf("app-grid custom path = %q, want %q", state.AppGridCustom, want)
	}
}
