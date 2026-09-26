package installcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDestructiveActionsRequireConfirmation pins the "Powerwash and Factory
// Reset are opt-in and always confirmed" invariant in AGENTS.md: neither
// irreversible maintenance action may run without an AdwAlertDialog
// confirmation first. internal/views/reset.go builds that dialog and only
// calls runPowerwash / runFactoryReset after the user selects the "confirm"
// response. This scan fails if any future edit invokes one of those run
// functions without first showing the dialog and gating on the confirm
// response — i.e. a destructive action wired straight to a button with no
// confirmation.
//
// It lives in internal/installcheck (pure, gate-enforced) rather than
// internal/views, which imports puregotk and cannot host a test binary on a
// headless host — see docs/skills/gtk-headless-testing.md.
func TestDestructiveActionsRequireConfirmation(t *testing.T) {
	// runPowerwash and runFactoryReset are the two irreversible maintenance
	// actions. runFlatpakUninstall removes an application — system-wide
	// removals for every account — and once ran straight from its row's
	// trash button while every Homebrew removal asked first (#353).
	// Everything else reachable from internal/views is either a
	// config-driven script (opt-in group, not covered by this invariant) or a
	// reversible action with its own dialog.
	destructive := map[string]bool{
		"runPowerwash":        true,
		"runFactoryReset":     true,
		"runFlatpakUninstall": true,
	}

	viewsDir := filepath.Join(RepoRoot(), "internal", "views")

	entries, err := os.ReadDir(viewsDir)
	if err != nil {
		t.Fatalf("read views dir: %v", err)
	}

	fset := token.NewFileSet()
	var astFiles []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(viewsDir, e.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		astFiles = append(astFiles, f)
	}
	if len(astFiles) == 0 {
		t.Fatalf("no Go files found under %s — the scan would pass vacuously", viewsDir)
	}

	seenRun := map[string]bool{}
	for _, astFile := range astFiles {
		for _, decl := range astFile.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			// Record the destructive action definitions so a rename or
			// removal is caught here rather than letting the scan pass
			// vacuously.
			if fn.Recv != nil && receiverIsUserHome(fn.Recv.List[0].Type) && destructive[fn.Name.Name] {
				seenRun[fn.Name.Name] = true
			}

			if !callsDestructive(fn.Body, destructive) {
				continue
			}

			hasDialog, hasConfirmGate := confirmationPresent(fn.Body)
			if !hasDialog {
				t.Errorf("%s.%s runs a destructive action without showing an AdwAlertDialog confirmation", astFile.Name, fn.Name.Name)
			}
			if !hasConfirmGate {
				t.Errorf("%s.%s runs a destructive action without gating on the \"confirm\" response", astFile.Name, fn.Name.Name)
			}
		}
	}

	for name := range destructive {
		if !seenRun[name] {
			t.Errorf("expected to find the destructive action %q in internal/views, but it is not defined", name)
		}
	}
}

// callsDestructive reports whether fnBody invokes any of the named methods on
// a receiver — the run* entry points that execute an action.
func callsDestructive(fnBody *ast.BlockStmt, destructive map[string]bool) bool {
	found := false
	ast.Inspect(fnBody, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		recv, ok := sel.X.(*ast.Ident)
		if ok && recv.Name != "" && destructive[sel.Sel.Name] {
			found = true
		}
		return true
	})
	return found
}

// confirmationPresent reports whether fnBody both shows an AdwAlertDialog and
// gates on the "confirm" response (a comparison against the literal, as in
// `if response != "confirm"`).
func confirmationPresent(fnBody *ast.BlockStmt) (hasDialog, hasConfirmGate bool) {
	ast.Inspect(fnBody, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && callExprIs(call, "adw", "NewAlertDialog") {
			hasDialog = true
		}
		if bin, ok := n.(*ast.BinaryExpr); ok && (bin.Op == token.NEQ || bin.Op == token.EQL) {
			if isStringLit(bin.X, "confirm") || isStringLit(bin.Y, "confirm") {
				hasConfirmGate = true
			}
		}
		return true
	})
	return hasDialog, hasConfirmGate
}

// callExprIs reports whether call is a qualified selector of the form
// pkg.Name(...).
func callExprIs(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && x.Name == pkg && sel.Sel.Name == name
}

// isStringLit reports whether node is a string basic literal whose text
// equals want (a *ast.BasicLit keeps its surrounding quotes in Value).
func isStringLit(node ast.Node, want string) bool {
	b, ok := node.(*ast.BasicLit)
	return ok && b.Kind == token.STRING && b.Value == `"`+want+`"`
}

// receiverIsUserHome reports whether a method receiver type is *UserHome or
// UserHome.
func receiverIsUserHome(node ast.Node) bool {
	switch t := node.(type) {
	case *ast.StarExpr:
		return isIdent(t.X, "UserHome")
	case *ast.Ident:
		return t.Name == "UserHome"
	}
	return false
}

// isIdent reports whether node is an identifier named name.
func isIdent(node ast.Node, name string) bool {
	id, ok := node.(*ast.Ident)
	return ok && id.Name == name
}
