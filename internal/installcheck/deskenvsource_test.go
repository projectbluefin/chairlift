package installcheck

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// deskenvPackage is the package this gate holds to its no-subprocess contract.
const deskenvPackage = "internal/deskenv"

// spawnImports are the import paths through which Go code starts a process.
// os/exec is the route a rewrite takes; syscall covers a hand-rolled
// ForkExec or Exec.
var spawnImports = map[string]struct{}{
	"os/exec": {},
	"syscall": {},
}

// spawnSelector is the third route, and the reason this gate reads the AST
// rather than only the import list: package os is imported legitimately (for
// os.Getenv), and os.StartProcess starts a process all the same.
const spawnSelector = "StartProcess"

// TestDesktopDetectionSpawnsNoProcess holds internal/deskenv to the property
// its callers depend on: detection is an environment-variable read.
//
// The package answers which desktop is running, and it is asked on every host
// ChairLift starts on — including a container, a minimal image with no
// desktop tool installed, and a session where spawning anything is the
// expensive part of startup. A detector rewritten to shell out
// (`plasmashell --version`, `gnome-shell --version`) would still pass a
// behavioral test that merely empties PATH, because an absolute path needs no
// lookup; the import and selector checks here are what actually forbid it.
//
// The walk covers the package's test files too. A test has no more business
// starting a desktop than the production code does, and leaving the test files
// exempt would make the gate quiet exactly where a spawn would first appear.
func TestDesktopDetectionSpawnsNoProcess(t *testing.T) {
	root := RepoRoot()
	packageRoot := filepath.Join(root, deskenvPackage)
	fileSet := token.NewFileSet()

	found := 0
	err := filepath.WalkDir(packageRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		found++

		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)

		for _, spec := range file.Imports {
			imported, unquoteErr := strconv.Unquote(spec.Path.Value)
			if unquoteErr != nil {
				continue
			}
			if _, forbidden := spawnImports[imported]; forbidden {
				position := fileSet.Position(spec.Pos())
				t.Errorf("%s:%d imports %s; %s must detect the desktop from the environment, never by starting a process",
					relative, position.Line, imported, deskenvPackage)
			}
		}

		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != spawnSelector {
				return true
			}
			position := fileSet.Position(selector.Pos())
			t.Errorf("%s:%d calls %s; %s must detect the desktop from the environment, never by starting a process",
				relative, position.Line, spawnSelector, deskenvPackage)
			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", deskenvPackage, err)
	}

	// A gate that silently stops finding its subject is a gate that has
	// stopped working — a moved or renamed package must fail here rather than
	// pass by scanning nothing.
	if found == 0 {
		t.Fatalf("no Go files found under %s; update this gate if the package moved", deskenvPackage)
	}
}
