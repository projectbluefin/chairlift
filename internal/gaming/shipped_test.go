package gaming

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestMain points the image declarations at nothing, so no test reads the
// host's own /usr/share/flatpak/preinstall.d or Bluefin's Brewfile — a
// Bluefin developer machine declares Flatseal there, a CI runner does not.
// A test that needs declarations writes its own with declareShipped.
func TestMain(m *testing.M) {
	preinstallDirs = nil
	imageFlatpakBrewfiles = nil
	os.Exit(m.Run())
}

// declareShipped points the image declarations at temporary copies: preinstall
// maps a directory index (0 = /usr/share, 1 = /etc) to file name to contents,
// and brewfile is the system Flatpak Brewfile ("" for none).
func declareShipped(t *testing.T, preinstall map[int]map[string]string, brewfile string) {
	t.Helper()

	root := t.TempDir()
	dirs := []string{filepath.Join(root, "usr-preinstall.d"), filepath.Join(root, "etc-preinstall.d")}
	for index, files := range preinstall {
		if err := os.MkdirAll(dirs[index], 0o755); err != nil {
			t.Fatal(err)
		}
		for name, contents := range files {
			if err := os.WriteFile(filepath.Join(dirs[index], name), []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	brewPath := filepath.Join(root, "system-flatpaks.Brewfile")
	if brewfile != "" {
		if err := os.WriteFile(brewPath, []byte(brewfile), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	previousDirs, previousBrewfiles := preinstallDirs, imageFlatpakBrewfiles
	preinstallDirs, imageFlatpakBrewfiles = dirs, []string{brewPath}
	t.Cleanup(func() { preinstallDirs, imageFlatpakBrewfiles = previousDirs, previousBrewfiles })
}

func TestShippedDeclarationsReadPreinstallAndBrewfileEntries(t *testing.T) {
	declareShipped(t, map[int]map[string]string{
		0: {
			"gaming.preinstall": "# vendor\n[Flatpak Preinstall " + steam + "]\nBranch=stable\n\n" +
				"[Flatpak Preinstall " + protonUp + "]\nInstall=false\n",
			"overridden.preinstall": "[Flatpak Preinstall " + goverlay + "]\n",
			"ignored.txt":           "[Flatpak Preinstall " + protontrick + "]\n",
		},
		// An administrator's file of the same name replaces the vendor's.
		1: {"overridden.preinstall": "[Flatpak Preinstall " + goverlay + "]\nInstall=false\n"},
	}, "# Default system-wide flatpaks\nbrew \"htop\"\nflatpak \""+flatseal+"\"\n  flatpak '"+mangohud+"', remote: \"flathub\"\n")

	shipped, err := imageShipped()
	if err != nil {
		t.Fatalf("imageShipped() error = %v", err)
	}
	want := map[string]bool{steam: true, flatseal: true, mangohud: true}
	if !reflect.DeepEqual(shipped, want) {
		t.Errorf("imageShipped() = %v, want %v", shipped, want)
	}
}

func TestShippedDeclarationsTreatMissingFilesAsNone(t *testing.T) {
	declareShipped(t, nil, "")

	shipped, err := imageShipped()
	if err != nil || len(shipped) != 0 {
		t.Errorf("imageShipped() = (%v, %v), want nothing and no error", shipped, err)
	}
}

// Disable leaves the system copy of a component the image ships in place —
// undoing gaming mode must not take a distro default away from every
// account — while still removing that component's user copy and every copy
// of a component the image does not ship.
func TestDisableLeavesAnImageShippedSystemCopyInPlace(t *testing.T) {
	declareShipped(t, map[int]map[string]string{
		0: {"steam.preinstall": "[Flatpak Preinstall " + steam + "]\n"},
	}, "flatpak \""+flatseal+"\"\n")
	stubScope(t, Scope{
		Installed: refsOf(steam, protonUp, flatseal),
		User:      refsOf(steam),
		System:    refsOf(steam, protonUp, flatseal),
	}, nil)
	log := fakeFlatpak(t, "exit 0")

	removed, kept, failures := Disable(allComponentIDs())
	if len(failures) != 0 {
		t.Fatalf("Disable() failures = %v, want none", failures)
	}
	if !reflect.DeepEqual(removed, []string{protonUp}) {
		t.Errorf("Disable() removed = %v, want only the component the image does not ship", removed)
	}
	if !reflect.DeepEqual(kept, []string{steam, flatseal}) {
		t.Errorf("Disable() kept = %v, want the image-shipped components", kept)
	}
	want := []string{
		"uninstall -y --user " + steam,
		"uninstall -y --system " + protonUp,
	}
	if calls := invocations(t, log); !reflect.DeepEqual(calls, want) {
		t.Errorf("Disable() ran %q, want %q", calls, want)
	}
}

// An unreadable declaration must not read as "the image ships nothing", which
// would remove a distro default system-wide; removal fails before mutating.
// A selection with no system copy never needs the declarations.
func TestDisableFailsClosedWhenImageDeclarationsAreUnreadable(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "preinstall.d")
	if err := os.WriteFile(notADir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	previous := preinstallDirs
	preinstallDirs = []string{notADir}
	t.Cleanup(func() { preinstallDirs = previous })
	stubScope(t, Scope{
		Installed: refsOf(steam, protonUp),
		User:      refsOf(protonUp),
		System:    refsOf(steam),
	}, nil)
	log := fakeFlatpak(t, "exit 0")

	removed, kept, failures := Disable([]string{steam})
	if removed != nil || kept != nil || len(failures) != 1 || !strings.Contains(failures[0].Error(), "system image") {
		t.Errorf("Disable() = (%v, %v, %v), want one failure naming the image declarations", removed, kept, failures)
	}
	if calls := invocations(t, log); len(calls) != 0 {
		t.Errorf("Disable() ran %q, want nothing", calls)
	}

	removed, _, failures = Disable([]string{protonUp})
	if len(failures) != 0 || !reflect.DeepEqual(removed, []string{protonUp}) {
		t.Errorf("Disable(user copy only) = (%v, %v), want it removed without reading declarations", removed, failures)
	}
}
