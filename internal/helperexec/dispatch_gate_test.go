package helperexec_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/ubluehelper"
	"github.com/projectbluefin/chairlift/internal/updexhelper"
)

// repoRoot is this package's directory relative to the module root, inverted.
const repoRoot = "../.."

// helperUnderGate names one privileged helper binary: the main.go that
// dispatches an already-validated Invocation, the internal package that
// declares the accepted command surface, and the selector main.go uses to
// reference that package's constants.
type helperUnderGate struct {
	name      string
	mainFile  string
	pkgDir    string
	selector  string
	supported []string
}

// TestHelperBinariesDispatchEverySupportedCommand gates each privileged
// helper's top-level `switch invocation.Command` against the command surface
// its parser accepts.
//
// Both helpers dispatch with a switch that has no default clause, so a
// command ParseInvocation accepts but main.go forgets to handle does not
// fail: the switch falls through, main returns, and the helper exits 0
// having done nothing. pkexec has already authenticated the action by then,
// so the GUI reports success for a privileged operation that never ran.
//
// The command packages live under cmd/, which the enforced unit and race
// commands (`go test ./internal/... -run "^Test[^I]"`) never compile, so no
// existing gate observes the dispatch arms at all. This test reads main.go's
// AST from inside internal/, where those commands do run.
func TestHelperBinariesDispatchEverySupportedCommand(t *testing.T) {
	for _, helper := range gatedHelpers() {
		t.Run(helper.name, func(t *testing.T) {
			consts := stringConstants(t, filepath.Join(repoRoot, helper.pkgDir))
			dispatched := dispatchedCommands(t, helper, consts)

			want := append([]string(nil), helper.supported...)
			sort.Strings(want)
			got := append([]string(nil), dispatched...)
			sort.Strings(got)

			for _, command := range missing(want, got) {
				t.Errorf("%s: ParseInvocation accepts %q but %s has no dispatch arm for it; "+
					"the switch falls through and the helper exits 0 without performing the operation",
					helper.name, command, helper.mainFile)
			}
			for _, command := range missing(got, want) {
				t.Errorf("%s: %s dispatches %q, which %s.SupportedCommands() does not list; "+
					"ParseInvocation rejects it, so the arm is unreachable",
					helper.name, helper.mainFile, command, helper.selector)
			}
		})
	}
}

func gatedHelpers() []helperUnderGate {
	return []helperUnderGate{
		{
			name:      "chairlift-ublue-helper",
			mainFile:  "cmd/chairlift-ublue-helper/main.go",
			pkgDir:    "internal/ubluehelper",
			selector:  "ubluehelper",
			supported: ubluehelper.SupportedCommands(),
		},
		{
			name:      "chairlift-updex-helper",
			mainFile:  "cmd/chairlift-updex-helper/main.go",
			pkgDir:    "internal/updexhelper",
			selector:  "updexhelper",
			supported: updexhelper.SupportedCommands(),
		},
	}
}

// stringConstants maps every untyped string constant declared in dir to its
// literal value, so a case expression written as a package-qualified
// identifier can be resolved to the argv token it stands for.
func stringConstants(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	values := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", entry.Name(), err)
		}

		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.CONST {
				continue
			}
			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range valueSpec.Names {
					if i >= len(valueSpec.Values) {
						continue
					}
					literal, ok := valueSpec.Values[i].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					unquoted, err := strconv.Unquote(literal.Value)
					if err != nil {
						continue
					}
					values[name.Name] = unquoted
				}
			}
		}
	}

	if len(values) == 0 {
		t.Fatalf("no string constants found in %s", dir)
	}
	return values
}

// dispatchedCommands returns the argv tokens main.go's command switch has an
// arm for. It fails the test rather than returning an empty set when the
// switch cannot be located, so a refactor that moves or renames the dispatch
// is reported instead of silently passing the gate.
func dispatchedCommands(t *testing.T, helper helperUnderGate, consts map[string]string) []string {
	t.Helper()

	path := filepath.Join(repoRoot, helper.mainFile)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", helper.mainFile, err)
	}

	var commands []string
	var found bool
	ast.Inspect(file, func(node ast.Node) bool {
		switchStmt, ok := node.(*ast.SwitchStmt)
		if !ok || !isCommandSwitch(switchStmt) {
			return true
		}
		found = true
		for _, stmt := range switchStmt.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expr := range clause.List {
				selector, ok := expr.(*ast.SelectorExpr)
				if !ok {
					t.Errorf("%s: unsupported case expression in the command switch; "+
						"this gate resolves %s.Command… identifiers only",
						helper.mainFile, helper.selector)
					continue
				}
				ident, ok := selector.X.(*ast.Ident)
				if !ok || ident.Name != helper.selector {
					t.Errorf("%s: case expression %v is not qualified with %s",
						helper.mainFile, selector.Sel.Name, helper.selector)
					continue
				}
				value, ok := consts[selector.Sel.Name]
				if !ok {
					t.Errorf("%s: case references %s.%s, which %s does not declare as a string constant",
						helper.mainFile, helper.selector, selector.Sel.Name, helper.pkgDir)
					continue
				}
				commands = append(commands, value)
			}
		}
		return false
	})

	if !found {
		t.Fatalf("%s: no `switch invocation.Command` found; the dispatch this gate checks has moved or been renamed",
			helper.mainFile)
	}
	return commands
}

// isCommandSwitch reports whether stmt switches on the Command field of the
// parsed invocation.
func isCommandSwitch(stmt *ast.SwitchStmt) bool {
	selector, ok := stmt.Tag.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Command" {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == "invocation"
}

// missing returns the elements of want that have no counterpart in got.
func missing(want, got []string) []string {
	present := make(map[string]bool, len(got))
	for _, value := range got {
		present[value] = true
	}

	var absent []string
	for _, value := range want {
		if !present[value] {
			absent = append(absent, value)
		}
	}
	return absent
}
