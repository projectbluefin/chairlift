package installcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/branding"
)

// desktopEntryPath is the desktop entry the Makefile and every nfpm package
// install. Its basename is the application ID and must not change; only the
// display keys inside it carry the product name.
const desktopEntryPath = "data/io.projectbluefin.chairlift.desktop"

// codeNameExemptions are the exact string literals permitted to spell the code
// name after the structural rules below have been applied. Each one is a
// deliberate use that a user either never reads, or reads as a path rather
// than as the product's name, and the reason is the point of the entry: an
// addition here is a claim, in writing, that the code name is correct there.
var codeNameExemptions = map[string]string{
	"ChairLiftApplication": "GObject type name registered with the GLib type system",
	"ChairLiftWindow":      "GObject type name registered with the GLib type system",
	"ChairLift activated": "E2E readiness log marker; a public contract polled by exact literal in " +
		"test/e2e/e2e_test.go and test/e2e/capture_walkthrough.sh (ADR-0008)",
	"ChairLift Contributors": "About-dialog credit naming contributors to the project, which is the code name",
	"CONFIGURATION ERROR: %s; all feature groups were disabled; fix the configuration file and restart ChairLift":                       "log line, not the toast; internal/config.LoadError.ToastMessage is the user-facing counterpart and uses branding.AppName",
	"sudo actions are only permitted in trusted configurations (/etc/chairlift, /usr/share/chairlift) (line %d)":                        "user-visible validation error, but the code name appears only as the two trusted configuration directories, which are filesystem paths fixed by ADR-0002",
	"group %q enables sudo action %q; sudo actions are only permitted in trusted configurations (/etc/chairlift, /usr/share/chairlift)": "same: the code name appears only as filesystem paths the user must actually navigate to",
	"You're ready to go! You can launch %s anytime from the Application Menu or by running chairlift.":                                  "First-run exit toast naming the CLI binary as an alternative launch method (issue #265)",
}

// exemptStructurally reports whether a literal spelling the code name is a
// path, identifier, or other machine-facing token rather than prose a user
// reads. These categories are load-bearing: renaming any of them breaks
// pkexec's exec-path match, the install layout, or a systemd unit.
//
// Every predicate is anchored. An unanchored one — "contains a slash", say —
// would let any user-facing sentence buy an exemption by including "and/or" or
// "N/A", which is precisely the leak that made the previous shape-matching
// gate useless. A prose string must never be able to exempt itself by
// containing a separator character.
func exemptStructurally(value string) string {
	switch {
	case strings.HasPrefix(value, "/"),
		strings.HasPrefix(value, "https://"),
		strings.HasPrefix(value, "http://"),
		strings.HasPrefix(value, "github.com/"):
		return "filesystem path, URL, or import path"
	case strings.HasPrefix(value, "io.projectbluefin.chairlift"):
		return "application ID, polkit action, or notification ID"
	case strings.HasPrefix(value, "CHAIRLIFT_"):
		return "environment variable name"
	case unitOrBinaryName.MatchString(value):
		return "binary, systemd unit, or quadlet name"
	case strings.HasPrefix(value, "usage: chairlift"):
		return "command-line usage string naming the invoked binary"
	}
	return ""
}

// unitOrBinaryName matches a literal that is *entirely* a binary, unit, or
// quadlet name — never a sentence that merely mentions one.
var unitOrBinaryName = regexp.MustCompile(`^chairlift(-[a-z0-9.-]+)?$`)

// TestDisplayNameHasOneOwner is the drift gate behind branding.AppName.
//
// ChairLift ships in Bluefin as "Control Center". The code name survives in the
// repository, the binaries, the package names, and the application ID
// io.projectbluefin.chairlift — all fixed by the pkexec exec-path annotations
// (ADR-0001) and the install prefix (ADR-0002). Two spellings coexist
// permanently, and nothing but a parser keeps them straight: the walkthrough
// screenshot check is referential rather than pixel-based, so captures whose
// title bar reads "ChairLift" pass CI while contradicting the shipped product.
//
// The gate is deliberately inverted. An earlier version matched *shapes* —
// arguments to a known GTK setter — and missed two live user-visible strings on
// consecutive attempts: notify.Notification's Body field literal, and
// config.LoadError.ToastMessage's fmt.Sprintf format string, which is the
// persistent fail-closed toast. Enumerating the ways a string can reach a user
// is a losing game, so instead *every* literal spelling the code name must
// justify itself, structurally or by name.
func TestDisplayNameHasOneOwner(t *testing.T) {
	root := RepoRoot()
	fileSet := token.NewFileSet()

	offenders := make([]string, 0)

	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}

			file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
			if parseErr != nil {
				return parseErr
			}

			// Import paths are string literals too, and every one of them
			// spells the module path.
			imports := make(map[token.Pos]bool, len(file.Imports))
			for _, spec := range file.Imports {
				imports[spec.Path.Pos()] = true
			}

			ast.Inspect(file, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || imports[lit.Pos()] {
					return true
				}
				value, unquoteErr := strconv.Unquote(lit.Value)
				if unquoteErr != nil || !strings.Contains(strings.ToLower(value), "chairlift") {
					return true
				}
				if exemptStructurally(value) != "" {
					return true
				}
				if _, exempt := codeNameExemptions[value]; exempt {
					return true
				}

				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+":"+
					strconv.Itoa(fileSet.Position(lit.Pos()).Line)+" "+lit.Value)
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}

	if len(offenders) > 0 {
		t.Errorf("string literals spell the code name where a user may read them; "+
			"use branding.AppName (%q), or add an entry to codeNameExemptions stating why no user sees it:\n  %s",
			branding.AppName, strings.Join(offenders, "\n  "))
	}
}

// TestCodeNameExemptionsAreAllLive keeps the exemption table honest.
//
// An exemption is a written claim that a specific string is not user-visible.
// Once the string it names is gone, the claim is unverifiable clutter that the
// next reader mistakes for a live constraint.
func TestCodeNameExemptionsAreAllLive(t *testing.T) {
	root := RepoRoot()
	fileSet := token.NewFileSet()

	found := make(map[string]bool, len(codeNameExemptions))

	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
			if parseErr != nil {
				return parseErr
			}
			ast.Inspect(file, func(node ast.Node) bool {
				if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if value, unquoteErr := strconv.Unquote(lit.Value); unquoteErr == nil {
						if _, listed := codeNameExemptions[value]; listed {
							found[value] = true
						}
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}

	for value, reason := range codeNameExemptions {
		if !found[value] {
			t.Errorf("codeNameExemptions has a stale entry %q (%s); no source file contains it", value, reason)
		}
	}
}

// TestDesktopEntryMatchesTheDisplayName keeps the shell's name for the
// application and the window's own title from diverging, and holds the rest of
// the entry's required shape.
//
// GNOME Shell and KRunner search the desktop entry's Name, GenericName, Comment
// and Keywords — not AppStream metainfo, which only feeds software centres.
// Nothing else in this package reads the desktop entry's contents, so without
// this the keys a user actually sees are ungated.
//
// It rejects duplicate keys and requires Type explicitly because an edit to
// this very file once dropped Type=Application and left two Categories lines,
// and the whole of `make ci` passed anyway: an entry without Type is invalid
// per the Desktop Entry Specification, so the launcher silently vanishes from
// the shell, and a last-wins map parse hides the duplicate.
func TestDesktopEntryMatchesTheDisplayName(t *testing.T) {
	path := filepath.Join(RepoRoot(), desktopEntryPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", desktopEntryPath, err)
	}

	entries := make(map[string]string)
	for number, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			t.Errorf("%s:%d is not a key=value line: %q", desktopEntryPath, number+1, trimmed)
			continue
		}
		if _, duplicate := entries[key]; duplicate {
			t.Errorf("%s:%d repeats key %s; a last-wins parse would hide this", desktopEntryPath, number+1, key)
		}
		entries[key] = value
	}

	// Required by the Desktop Entry Specification. Without it the entry is
	// invalid and the application does not appear in the shell at all.
	if got := entries["Type"]; got != "Application" {
		t.Errorf("%s Type = %q, want %q", desktopEntryPath, got, "Application")
	}

	// The application ID. It is the polkit icon_name, the icon theme name, and
	// this file's own basename, so it moves only with the pkexec policies.
	if got := entries["Icon"]; got != "io.projectbluefin.chairlift" {
		t.Errorf("%s Icon = %q, want the application ID io.projectbluefin.chairlift", desktopEntryPath, got)
	}

	if got := entries["Name"]; got != branding.AppName {
		t.Errorf("%s Name = %q, want branding.AppName %q", desktopEntryPath, got, branding.AppName)
	}

	// Categories must carry the registered Settings category, or the entry does
	// not file under Settings in GNOME or KDE. It must carry exactly one
	// registered main category, or desktop-file-validate warns that the
	// application may appear twice in the menu.
	registeredMainCategories := map[string]bool{
		"AudioVideo": true, "Audio": true, "Video": true, "Development": true,
		"Education": true, "Game": true, "Graphics": true, "Network": true,
		"Office": true, "Science": true, "Settings": true, "System": true,
		"Utility": true,
	}
	mainCategories := make([]string, 0, 1)
	hasSettings := false
	for _, category := range strings.Split(strings.Trim(entries["Categories"], ";"), ";") {
		if category == "Settings" {
			hasSettings = true
		}
		if registeredMainCategories[category] {
			mainCategories = append(mainCategories, category)
		}
	}
	if !hasSettings {
		t.Errorf("%s Categories = %q, want it to include Settings", desktopEntryPath, entries["Categories"])
	}
	if len(mainCategories) != 1 {
		t.Errorf("%s Categories = %q has %d registered main categories %v, want exactly 1; "+
			"more than one makes the application appear twice in the menu",
			desktopEntryPath, entries["Categories"], len(mainCategories), mainCategories)
	}

	// The search keys behind discovery by the common names for an OS
	// configuration tool. GenericName and Comment are matched by the shell
	// alongside Keywords, so an empty one silently narrows discovery.
	for _, key := range []string{"GenericName", "Comment", "Keywords"} {
		if strings.TrimSpace(entries[key]) == "" {
			t.Errorf("%s is missing %s, which GNOME Shell and KRunner match against", desktopEntryPath, key)
		}
	}

	keywords := entries["Keywords"]
	if !strings.HasSuffix(keywords, ";") {
		t.Errorf("%s Keywords = %q, want a trailing semicolon per the desktop entry spec", desktopEntryPath, keywords)
	}
	for _, want := range []string{"Settings", "Control Panel", "Preferences", "Configuration"} {
		if !strings.Contains(keywords, want) {
			t.Errorf("%s Keywords does not include %q", desktopEntryPath, want)
		}
	}
}
