package installcheck

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdateBadgeStaysNoninteractive pins the "accessibility: render the
// update count as a noninteractive badge" fix (chairlift#67): the sidebar
// Updates row's count suffix must be built as a plain *gtk.Label — never a
// clickable control — and must never gain SetActivatable(true) or a signal
// handler, so it cannot expose a focusable "N button" with no behavior or
// steal pointer activation from the Updates row itself
// (row.SetActivatable(true) in createNavRow is the row's own, sole,
// actionable target).
//
// It lives in internal/installcheck (pure, gate-enforced) rather than
// internal/window, which imports puregotk and cannot host a test binary on a
// headless host — see docs/skills/gtk-headless-testing/SKILL.md.
func TestUpdateBadgeStaysNoninteractive(t *testing.T) {
	path := filepath.Join(RepoRoot(), "internal", "window", "window.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	fieldType, found := updateBadgeFieldType(fset, file)
	if !found {
		t.Fatalf("Window struct in %s no longer declares an updateBadge field", path)
	}
	if fieldType != "*gtk.Label" {
		t.Errorf("Window.updateBadge field type = %q, want \"*gtk.Label\" — a different widget type can be focusable or activatable", fieldType)
	}

	sawLabelConstruction := false
	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			if !isSelectorOn(lhs, "w", "updateBadge") || i >= len(assign.Rhs) {
				continue
			}
			call, ok := assign.Rhs[i].(*ast.CallExpr)
			if !ok {
				t.Errorf("w.updateBadge is assigned from a non-call expression; expected gtk.NewLabel(...)")
				continue
			}
			if !callExprIs(call, "gtk", "NewLabel") {
				t.Errorf("w.updateBadge is constructed via %s, want gtk.NewLabel(...) — another constructor may produce a focusable or activatable widget", exprString(fset, call.Fun))
				continue
			}
			sawLabelConstruction = true
		}
		return true
	})
	if !sawLabelConstruction {
		t.Fatalf("found no w.updateBadge = gtk.NewLabel(...) assignment in %s", path)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !rootedAtUpdateBadge(sel.X) {
			return true
		}
		switch {
		case sel.Sel.Name == "SetActivatable":
			t.Errorf("w.updateBadge...SetActivatable(...) is called — the badge must stay noninteractive; only the Updates row may be activatable")
		case strings.HasPrefix(sel.Sel.Name, "Connect"):
			t.Errorf("w.updateBadge...%s(...) is called — the badge must not carry a signal handler", sel.Sel.Name)
		case sel.Sel.Name == "AddController":
			t.Errorf("w.updateBadge...AddController(...) is called — a gesture/event controller makes the badge interactive just like a signal handler would")
		case sel.Sel.Name == "SetFocusable":
			t.Errorf("w.updateBadge...SetFocusable(...) is called — a focusable badge can receive keyboard focus like a real control")
		case sel.Sel.Name == "SetCanTarget":
			t.Errorf("w.updateBadge...SetCanTarget(...) is called — a badge that can be a pointer target can receive clicks/gestures")
		case sel.Sel.Name == "SetSelectable":
			t.Errorf("w.updateBadge...SetSelectable(...) is called — a selectable badge can take keyboard focus")
		case sel.Sel.Name == "SetCanFocus":
			t.Errorf("w.updateBadge...SetCanFocus(...) is called — a focusable badge can receive keyboard focus")
		}
		return true
	})
}

// rootedAtUpdateBadge reports whether expr is w.updateBadge, or a selector
// chain built on top of it (e.g. w.updateBadge.Widget), so a call routed
// through an embedded field — w.updateBadge.Widget.AddController(...) is the
// idiom this package's own construction code uses at window.go:202 — is not
// missed just because it isn't the bare w.updateBadge receiver.
func rootedAtUpdateBadge(expr ast.Expr) bool {
	if isSelectorOn(expr, "w", "updateBadge") {
		return true
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return rootedAtUpdateBadge(sel.X)
}

// updateBadgeFieldType returns the source text of the Window struct's
// updateBadge field type, and whether the field was found at all.
func updateBadgeFieldType(fset *token.FileSet, file *ast.File) (string, bool) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "Window" {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range structType.Fields.List {
				for _, name := range field.Names {
					if name.Name == "updateBadge" {
						return exprString(fset, field.Type), true
					}
				}
			}
		}
	}
	return "", false
}

// isSelectorOn reports whether expr is a selector of the form recv.field,
// where recv is a plain identifier.
func isSelectorOn(expr ast.Expr, recv, field string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == recv && sel.Sel.Name == field
}

// exprString renders an AST expression back to source text, e.g. "*gtk.Label"
// or "gtk.NewButton".
func exprString(fset *token.FileSet, expr ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, expr); err != nil {
		return "<unprintable>"
	}
	return buf.String()
}
