package actionstate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResetPageWiresConfirmationAndActionGates(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate reset_wiring_test.go")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	path := filepath.Join(repoRoot, "internal", "views", "reset.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(source)

	for _, required := range []string{
		`powerwashBtn.AddCssClass("destructive-action")`,
		`resetBtn.AddCssClass("destructive-action")`,
		`presentation := pageview.PowerwashRow("")`,
		`resetPresentation := pageview.FactoryResetRow()`,
		`if !uh.powerwashGate.TryStart()`,
		`title, body := pageview.PowerwashConfirmation()`,
		`dialog := adw.NewAlertDialog(title, body)`,
		`dialog.AddResponse("cancel", "Cancel")`,
		`dialog.AddResponse("confirm", "Remove Everything")`,
		`dialog.SetResponseAppearance("confirm", adw.ResponseDestructiveValue)`,
		`if response != "confirm"`,
		`uh.powerwashGate.Reset()`,
		`uh.runPowerwash(button, row)`,
		`uh.powerwashGate.Complete()`,
		`runner := powerwash.Runner{`,
		`powerwash.Summarize(results)`,
		`actionmsg.Powerwash(dryrun.Enabled(), summary.Succeeded, summary.Failed)`,
		`if !uh.factoryResetGate.TryStart()`,
		`title, body := pageview.FactoryResetConfirmation()`,
		`dialog.AddResponse("confirm", "Factory Reset")`,
		`uh.factoryResetGate.Reset()`,
		`uh.runFactoryReset(button, row)`,
		`uh.factoryResetGate.Complete()`,
		`ublue.FactoryReset(ctx)`,
		`actionmsg.FactoryReset(dryrun.Enabled())`,
	} {
		if !strings.Contains(text, required) {
			t.Errorf("reset.go wiring does not contain %q", required)
		}
	}
}
