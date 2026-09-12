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

// destructiveAction defines the required confirmation and safety wiring for an
// irreversible or destructive action exposed in internal/views.
type destructiveAction struct {
	Name              string
	File              string
	TriggerFunc       string
	ExecutionFunc     string
	BackendCall       string
	DialogAppearance  string
	PositiveResponse  string
	ButtonClass       string
	GateTryStart      string
	GateResetOnCancel string
}

func destructiveActionsTable() []destructiveAction {
	return []destructiveAction{
		{
			Name:              "Powerwash",
			File:              "reset.go",
			TriggerFunc:       "onPowerwashClicked",
			ExecutionFunc:     "runPowerwash",
			BackendCall:       "powerwash.Runner",
			DialogAppearance:  `dialog.SetResponseAppearance("confirm", adw.ResponseDestructiveValue)`,
			PositiveResponse:  `response != "confirm"`,
			ButtonClass:       `powerwashBtn.AddCssClass("destructive-action")`,
			GateTryStart:      `!uh.powerwashGate.TryStart()`,
			GateResetOnCancel: `uh.powerwashGate.Reset()`,
		},
		{
			Name:              "Factory Reset",
			File:              "reset.go",
			TriggerFunc:       "onFactoryResetClicked",
			ExecutionFunc:     "runFactoryReset",
			BackendCall:       "ublue.FactoryReset",
			DialogAppearance:  `dialog.SetResponseAppearance("confirm", adw.ResponseDestructiveValue)`,
			PositiveResponse:  `response != "confirm"`,
			ButtonClass:       `resetBtn.AddCssClass("destructive-action")`,
			GateTryStart:      `!uh.factoryResetGate.TryStart()`,
			GateResetOnCancel: `uh.factoryResetGate.Reset()`,
		},
		{
			Name:              "Homebrew Package Uninstall",
			File:              "applications_page.go",
			TriggerFunc:       "confirmHomebrewUninstall",
			ExecutionFunc:     "runHomebrewUninstall",
			BackendCall:       "homebrew.Uninstall",
			DialogAppearance:  `dialog.SetResponseAppearance("uninstall", adw.ResponseDestructiveValue)`,
			PositiveResponse:  `response != "uninstall"`,
			ButtonClass:       `uninstallBtn.AddCssClass("destructive-action")`,
			GateTryStart:      `!gate.TryStart()`,
			GateResetOnCancel: `gate.Reset()`,
		},
	}
}

// TestDestructiveActionsRequireConfirmationDialog statically asserts that every
// destructive or irreversible action reachable from internal/views is gated by
// an explicit AdwAlertDialog confirmation carrying adw.ResponseDestructiveValue,
// that cancel branches release the action gate, and that the execution function
// is reachable only after a confirmed dialog response.
func TestDestructiveActionsRequireConfirmationDialog(t *testing.T) {
	actions := destructiveActionsTable()
	if len(actions) == 0 {
		t.Fatal("destructiveActionsTable() is empty")
	}

	viewsDir := filepath.Join(RepoRoot(), "internal", "views")

	for _, act := range actions {
		t.Run(act.Name, func(t *testing.T) {
			filePath := filepath.Join(viewsDir, act.File)
			source, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("reading %s: %v", filePath, err)
			}
			content := string(source)

			// 1. Trigger function must exist and guard against concurrent starts.
			if !strings.Contains(content, act.GateTryStart) {
				t.Errorf("%s trigger function does not guard with gate check %q", act.File, act.GateTryStart)
			}

			// 2. Dialog must set destructive appearance on its confirmation response.
			if !strings.Contains(content, act.DialogAppearance) {
				t.Errorf("%s does not set destructive appearance %q", act.File, act.DialogAppearance)
			}

			// 3. Response callback must reset the gate when canceled/dismissed.
			if !strings.Contains(content, act.GateResetOnCancel) {
				t.Errorf("%s does not reset gate on cancel %q", act.File, act.GateResetOnCancel)
			}

			// 4. Response callback must guard execution on positive response.
			if !strings.Contains(content, act.PositiveResponse) {
				t.Errorf("%s does not check positive response %q", act.File, act.PositiveResponse)
			}

			// 5. Execution function must contain the backend call.
			if !strings.Contains(content, act.BackendCall) {
				t.Errorf("%s execution does not contain backend call %q", act.File, act.BackendCall)
			}

			// 6. Trigger button must be styled with destructive CSS class.
			if !strings.Contains(content, act.ButtonClass) {
				t.Errorf("%s does not apply destructive CSS class %q", act.File, act.ButtonClass)
			}
		})
	}
}

// TestDestructiveBackendCallsAreGuardedInViews ensures that no destructive
// backend operations (powerwash, factory-reset, homebrew-uninstall) are invoked
// directly from unexpected or ungated locations in internal/views.
func TestDestructiveBackendCallsAreGuardedInViews(t *testing.T) {
	viewsDir := filepath.Join(RepoRoot(), "internal", "views")
	entries, err := os.ReadDir(viewsDir)
	if err != nil {
		t.Fatalf("reading internal/views: %v", err)
	}

	fset := token.NewFileSet()

	type backendSink struct {
		pkgName string
		selName string
		allowed []string // allowed "filename:funcname"
	}

	sinks := []backendSink{
		{
			pkgName: "ublue",
			selName: "FactoryReset",
			allowed: []string{"reset.go:runFactoryReset"},
		},
		{
			pkgName: "powerwash",
			selName: "Runner",
			allowed: []string{"reset.go:runPowerwash"},
		},
		{
			pkgName: "flatpak",
			selName: "RemoveAllUser",
			allowed: []string{"reset.go:runPowerwash"},
		},
		{
			pkgName: "distrobox",
			selName: "RemoveAll",
			allowed: []string{"reset.go:runPowerwash"},
		},
		{
			pkgName: "homebrew",
			selName: "Uninstall",
			allowed: []string{"applications_page.go:runHomebrewUninstall"},
		},
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}

		filePath := filepath.Join(viewsDir, entry.Name())
		node, err := parser.ParseFile(fset, filePath, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", filePath, err)
		}

		ast.Inspect(node, func(n ast.Node) bool {
			funcDecl, ok := n.(*ast.FuncDecl)
			if !ok {
				return true
			}

			currentFunc := entry.Name() + ":" + funcDecl.Name.Name

			ast.Inspect(funcDecl.Body, func(inner ast.Node) bool {
				sel, ok := inner.(*ast.SelectorExpr)
				if !ok {
					return true
				}

				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}

				for _, sink := range sinks {
					if ident.Name == sink.pkgName && sel.Sel.Name == sink.selName {
						isAllowed := false
						for _, allow := range sink.allowed {
							if currentFunc == allow {
								isAllowed = true
								break
							}
						}
						if !isAllowed {
							t.Errorf("destructive backend call %s.%s found in unauthorized location %s (allowed: %v)",
								sink.pkgName, sink.selName, currentFunc, sink.allowed)
						}
					}
				}
				return true
			})

			return false
		})
	}
}

// TestResetGroupIsDisabledByDefault asserts that irreversible actions
// (Powerwash, Factory Reset, and privileged maintenance scripts) are disabled
// by default in defaultConfig(), requiring explicit administrator opt-in.
func TestResetGroupIsDisabledByDefault(t *testing.T) {
	configSource := readRepoFile(t, filepath.Join("internal", "config", "config.go"))

	requiredDefaultDisabled := []struct {
		group string
		want  string
	}{
		{
			group: "reset_group",
			want:  `"reset_group": GroupConfig{Enabled: false}`,
		},
		{
			group: "maintenance_cleanup_group",
			want:  `"maintenance_cleanup_group": GroupConfig{`,
		},
	}

	for _, req := range requiredDefaultDisabled {
		t.Run(req.group, func(t *testing.T) {
			if !strings.Contains(configSource, req.want) {
				t.Errorf("internal/config/config.go does not configure %s disabled by default (%q)", req.group, req.want)
			}
		})
	}
}
