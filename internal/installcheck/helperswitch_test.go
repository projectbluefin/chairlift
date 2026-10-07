package installcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// TestHelperImageSwitchesGoThroughSwitchImage holds every chairlift-helper
// arm that runs `bootc switch` to switchImage, which falls back to
// `bootc rollback` when the derived target is the rollback deployment.
// A composefs `bootc switch` refuses that target ("Target image has the same
// fs-verity digest as the existing Some(Rollback) deployment"), and
// "Return to stream" (unpin) always targets the stream the host left when it
// pinned — the rollback deployment — so an arm that calls run("bootc", …)
// directly fails exactly where the person asked to go back.
//
// cmd/ is outside the enforced ./internal/... unit gate, so the wiring is
// read from main.go's AST here.
func TestHelperImageSwitchesGoThroughSwitchImage(t *testing.T) {
	path := filepath.Join(RepoRoot(), "cmd", "chairlift-helper", "main.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	switchers := map[string]bool{"runChannelSwitch": false, "runDriverSwitch": false, "runPin": false}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if _, tracked := switchers[fn.Name.Name]; !tracked {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			switch ident.Name {
			case "switchImage":
				switchers[fn.Name.Name] = true
			case "run":
				if len(call.Args) >= 2 {
					if lit, ok := call.Args[1].(*ast.BasicLit); ok {
						if program, err := strconv.Unquote(lit.Value); err == nil && program == "bootc" {
							t.Errorf("%s calls run(ctx, \"bootc\", …) directly; image switches must go through switchImage", fn.Name.Name)
						}
					}
				}
			}
			return true
		})
	}
	for name, routed := range switchers {
		if !routed {
			t.Errorf("%s does not call switchImage (or no longer exists in cmd/chairlift-helper/main.go)", name)
		}
	}
}

// TestViewsRunImageSwitchesUnderTheImageSwitchContext holds every view that
// calls an image-switching ublue mutation to ublue.ImageSwitchContext. Those
// commands pull a full image under the helper's 30-minute budget; a caller
// on DefaultContext gives up after 15 minutes and reports a timeout (and
// restores its control) while the helper is still pulling.
func TestViewsRunImageSwitchesUnderTheImageSwitchContext(t *testing.T) {
	// Pin and Unpin have no GUI caller since the published-versions calendar
	// was withdrawn (#522); the helper arms stay and are held above.
	switchers := map[string]bool{"SwitchChannel": true, "SwitchDriver": true}
	dir := filepath.Join(RepoRoot(), "internal", "views")
	matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := 0
	for _, path := range matches {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if !ok {
				return true
			}
			var switching []string
			var contexts []string
			for _, stmt := range lit.Body.List {
				ast.Inspect(stmt, func(n ast.Node) bool {
					if _, nested := n.(*ast.FuncLit); nested {
						return false
					}
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "ublue" {
						return true
					}
					switch {
					case switchers[sel.Sel.Name]:
						switching = append(switching, sel.Sel.Name)
					case sel.Sel.Name == "ImageSwitchContext" || sel.Sel.Name == "DefaultContext":
						contexts = append(contexts, sel.Sel.Name)
					}
					return true
				})
			}
			for _, name := range switching {
				found++
				if len(contexts) != 1 || contexts[0] != "ImageSwitchContext" {
					t.Errorf("%s: ublue.%s runs under %v, want ublue.ImageSwitchContext",
						fset.Position(lit.Pos()), name, contexts)
				}
			}
			return true
		})
	}
	if found < len(switchers) {
		t.Errorf("found %d image-switch calls in internal/views, want at least %d; did the call shape change?", found, len(switchers))
	}
}
