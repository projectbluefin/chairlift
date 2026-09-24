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

	"github.com/projectbluefin/chairlift/internal/pkexec"
)

// pkexecOwner is the one file allowed to spell the privilege-escalation
// program name. Everything else names internal/pkexec.Command.
const pkexecOwner = "internal/pkexec/pkexec.go"

// TestPkexecCommandHasOneOwner is the drift gate behind internal/pkexec.
//
// pkexec is the single route by which ChairLift reaches root, and the program
// PolicyKit binds every data/*.policy action to. internal/helperexec and
// internal/stageexec both take it as a parameter so tests can substitute a
// stand-in, which means the production value lives in the *callers* — and for
// a long time each OS-update, ublue, and updex provider package declared a
// private copy of it. Four copies of the escalation
// entrypoint is four places for one of them to be changed alone.
//
// The gate walks Go source rather than grepping so comments (which legitimately
// discuss pkexec by name) never register: only a string literal in the AST
// counts.
func TestPkexecCommandHasOneOwner(t *testing.T) {
	root := RepoRoot()
	fileSet := token.NewFileSet()

	offenders := make([]string, 0)
	seenOwner := false

	for _, dir := range []string{"internal", "cmd"} {
		walkRoot := filepath.Join(root, dir)
		err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, err error) error {
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

			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			relative = filepath.ToSlash(relative)

			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, unquoteErr := strconv.Unquote(literal.Value)
				if unquoteErr != nil || value != pkexec.Command {
					return true
				}

				if relative == pkexecOwner {
					seenOwner = true
				} else {
					position := fileSet.Position(literal.Pos())
					offenders = append(offenders, relative+":"+strconv.Itoa(position.Line))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}

	if !seenOwner {
		t.Errorf("%s no longer declares the %q literal — internal/pkexec must remain its owner", pkexecOwner, pkexec.Command)
	}

	for _, offender := range offenders {
		t.Errorf("%s spells %q directly; use pkexec.Command so the escalation entrypoint has one owner", offender, pkexec.Command)
	}
}
