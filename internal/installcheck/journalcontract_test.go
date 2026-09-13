package installcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// internal/journal states its contract in universal terms — it "records every
// privileged action ChairLift takes or would take" — and calls a journal
// captured on a real system "the audit trail for what ChairLift did to that
// machine, which matters most for the operations that cannot be undone".
//
// A doc comment cannot hold that line. Before this gate the only package
// honoring it was internal/helperexec, so the two staging polkit actions and
// the maintenance scripts escalated with no entry at all, and nothing in the
// tree said so. These tests make the contract structural: every function that
// hands a command to os/exec is classified exactly once, and every function
// classified as a privileged escalation must journal.
//
// Both tables are keyed by repo-relative file path and enclosing function
// name, because that pair is what a reviewer can check by eye against the
// source, and because a renamed or moved executor should fail loudly rather
// than silently leave its classification behind.

// execSite identifies one os/exec call site by the file and function that
// contains it. For methods, Func is "Receiver.Method".
type execSite struct {
	File string
	Func string
}

// privilegedExecSite is an os/exec call site whose command word can be
// pkexec, paired with the function that owes it a journal entry. The two are
// separate because the journal record belongs at the dispatch choke point,
// not at the syscall: stageexec.Stage journals and then delegates to
// stageexec.Run, so Run itself never sees the dry-run branch it must be
// distinguishable from.
type privilegedExecSite struct {
	Site execSite
	// JournalledBy must call journal.Record and reference both suppression
	// states. It is the function a reader should open to see what gets
	// recorded for this escalation.
	JournalledBy execSite
}

// privilegedExecSites are the os/exec call sites that can escalate via
// pkexec. Every privilege ChairLift can exercise reaches one of them:
//   - helperexec.Run drives both fixed-path helper binaries, and so covers all
//     9 io.projectbluefin.chairlift.ublue.* and 3 .updex.* polkit actions.
//   - stageexec.Run, entered only through stageexec.Stage, drives both stage
//     scripts, covering io.projectbluefin.chairlift.bootc.stage and
//     .sysupdate.stage.
//   - UserHome.runMaintenanceAction runs a config-declared maintenance script,
//     which pageview.MaintenanceCommand prefixes with pkexec when the action
//     is declared sudo. It has no dedicated polkit action and therefore falls
//     back to org.freedesktop.policykit.exec — the broadest privilege in the
//     application, and the one least able to afford being unrecorded.
var privilegedExecSites = []privilegedExecSite{
	{
		Site:         execSite{File: "internal/helperexec/helperexec.go", Func: "Run"},
		JournalledBy: execSite{File: "internal/helperexec/helperexec.go", Func: "Run"},
	},
	{
		Site:         execSite{File: "internal/stageexec/stageexec.go", Func: "Run"},
		JournalledBy: execSite{File: "internal/stageexec/stageexec.go", Func: "Stage"},
	},
	{
		Site:         execSite{File: "internal/views/maintenance_page.go", Func: "UserHome.runMaintenanceAction"},
		JournalledBy: execSite{File: "internal/views/maintenance_page.go", Func: "UserHome.runMaintenanceAction"},
	},
}

// unprivilegedExecSites are the os/exec call sites that never produce a
// pkexec command word. They are listed rather than inferred so that adding an
// exec call site is a deliberate act: an unclassified site fails
// TestEveryExecSiteIsClassified, and the only way to pass is to declare here
// that the new command runs with the calling user's privileges, or to declare
// it above and journal it.
//
// The comment on each entry is the command word that makes it unprivileged.
var unprivilegedExecSites = []execSite{
	{File: "internal/aistack/aistack.go", Func: "execSystemctl"},              // systemctl --user
	{File: "internal/autoupdate/autoupdate.go", Func: "systemctlOutput"},      // systemctl (query)
	{File: "internal/bootc/bootc.go", Func: "getStatusFrom"},                  // bootc status (read-only)
	{File: "internal/distrobox/distrobox.go", Func: "RemoveAll"},              // distrobox
	{File: "internal/flatpak/flatpak.go", Func: "runFlatpakCommandAt"},        // flatpak
	{File: "internal/flatpak/flatpak.go", Func: "IsInstalled"},                // flatpak --version
	{File: "internal/homebrew/homebrew.go", Func: "runBrewCommandAt"},         // brew
	{File: "internal/homebrew/homebrew.go", Func: "IsInstalled"},              // brew --version
	{File: "internal/sysupdate/rollback.go", Func: "runLsblk"},                // lsblk (read-only)
	{File: "internal/troubleshoot/troubleshoot.go", Func: "defaultRunSetup"},  // user-scope setup
	{File: "internal/views/applications_page.go", Func: "UserHome.launchApp"}, // gtk-launch
	{File: "internal/views/help_page.go", Func: "UserHome.openURL"},           // xdg-open
}

func TestEveryPrivilegedExecutorJournals(t *testing.T) {
	t.Parallel()

	for _, privileged := range privilegedExecSites {
		t.Run(privileged.Site.File+":"+privileged.Site.Func, func(t *testing.T) {
			t.Parallel()

			fn := findFunc(t, privileged.JournalledBy)
			if !callsJournalRecord(fn) {
				t.Fatalf("%s can escalate privilege but %s in %s never calls journal.Record.\n"+
					"internal/journal records every privileged action ChairLift takes or would take, "+
					"in both the dry-run and the live branch, so the argv ChairLift assembled can be "+
					"asserted without granting privilege and a real run leaves an audit trail.",
					privileged.Site.Func, privileged.JournalledBy.Func, privileged.JournalledBy.File)
			}
		})
	}
}

// A privileged executor that journals only when it actually runs leaves the
// most interesting case unassertable: "nothing happened" and "the wrong thing
// was almost attempted" look identical from outside. Both suppression states
// must be reachable from each executor.
func TestEveryPrivilegedExecutorRecordsBothSuppressionStates(t *testing.T) {
	t.Parallel()

	for _, privileged := range privilegedExecSites {
		t.Run(privileged.Site.File+":"+privileged.Site.Func, func(t *testing.T) {
			t.Parallel()

			fn := findFunc(t, privileged.JournalledBy)
			for _, want := range []string{"SuppressedNone", "SuppressedDryRun"} {
				if !referencesSelector(fn, "journal", want) {
					t.Errorf("%s in %s never references journal.%s; a privileged executor must "+
						"record both the suppressed dry-run attempt and the live invocation",
						privileged.JournalledBy.Func, privileged.JournalledBy.File, want)
				}
			}
		})
	}
}

// TestEveryExecSiteIsClassified is the completeness half of the gate. Without
// it the tables above would only describe the executors someone remembered to
// list, and a new pkexec call site could be added without ever failing a test.
func TestEveryExecSiteIsClassified(t *testing.T) {
	t.Parallel()

	classified := map[execSite]bool{}
	sites := append([]execSite{}, unprivilegedExecSites...)
	for _, privileged := range privilegedExecSites {
		sites = append(sites, privileged.Site)
	}
	for _, site := range sites {
		if classified[site] {
			t.Errorf("%s:%s is classified twice; an exec site is either privileged or not", site.File, site.Func)
		}
		classified[site] = true
	}

	found := map[execSite]bool{}
	for _, site := range discoverExecSites(t) {
		found[site] = true
		if !classified[site] {
			t.Errorf("unclassified os/exec call site %s:%s.\n"+
				"Add it to privilegedExecSites (with the function that journals it) if it can run pkexec, "+
				"or to unprivilegedExecSites with the command word that makes it unprivileged.",
				site.File, site.Func)
		}
	}

	var stale []string
	for site := range classified {
		if !found[site] {
			stale = append(stale, site.File+":"+site.Func)
		}
	}
	sort.Strings(stale)
	for _, s := range stale {
		t.Errorf("classified exec site %s no longer contains an os/exec call; "+
			"remove or update the entry so the table keeps describing the tree", s)
	}
}

// findFunc parses site.File and returns the declaration named site.Func,
// where a method is named "Receiver.Method".
func findFunc(t *testing.T, site execSite) *ast.FuncDecl {
	t.Helper()

	path := filepath.Join(RepoRoot(), site.File)
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", site.File, err)
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && funcName(fn) == site.Func {
			return fn
		}
	}
	t.Fatalf("no function %s in %s", site.Func, site.File)
	return nil
}

// discoverExecSites walks every non-test Go file under internal/ and returns
// the enclosing function of each exec.Command or exec.CommandContext call.
func discoverExecSites(t *testing.T) []execSite {
	t.Helper()

	root := RepoRoot()
	internal := filepath.Join(root, "internal")

	var sites []execSite
	err := filepath.WalkDir(internal, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if containsExecCall(fn) {
				sites = append(sites, execSite{File: filepath.ToSlash(rel), Func: funcName(fn)})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking internal/: %v", err)
	}
	return sites
}

func containsExecCall(fn *ast.FuncDecl) bool {
	return referencesSelector(fn, "exec", "Command") || referencesSelector(fn, "exec", "CommandContext")
}

func callsJournalRecord(fn *ast.FuncDecl) bool {
	return referencesSelector(fn, "journal", "Record")
}

// referencesSelector reports whether fn's body contains pkg.name anywhere,
// including inside nested function literals.
func referencesSelector(fn *ast.FuncDecl, pkg, name string) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if ok && ident.Name == pkg {
			found = true
			return false
		}
		return true
	})
	return found
}

// funcName renders a declaration's name, qualifying methods with their
// receiver type so "Run" and "Foo.Run" cannot collide in a table.
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return receiverTypeName(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func receiverTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(typed.X)
	case *ast.IndexExpr:
		return receiverTypeName(typed.X)
	case *ast.Ident:
		return typed.Name
	default:
		return ""
	}
}
