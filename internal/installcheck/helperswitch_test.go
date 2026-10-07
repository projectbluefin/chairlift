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
