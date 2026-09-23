package firstrun

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestWelcomeModelPresentsHeroScreenAsFirstStep(t *testing.T) {
	model := NewAssistantModel(nil)
	if model.TotalSteps() == 0 {
		t.Fatal("AssistantModel should have at least the welcome step")
	}

	if !model.IsWelcome() {
		t.Error("expected initial step to be the welcome screen")
	}

	step := model.CurrentStep()
	if step.ID != StepIDWelcome {
		t.Errorf("CurrentStep ID = %q, want %q", step.ID, StepIDWelcome)
	}

	if step.Title != "Welcome to Bluefin" {
		t.Errorf("CurrentStep Title = %q, want %q", step.Title, "Welcome to Bluefin")
	}
}

func TestFlowSelectionGetMovingExitsImmediately(t *testing.T) {
	model := NewAssistantModel(nil)
	next, dismissed, disp := model.SelectFlow(FlowChoiceGetMoving)

	if !dismissed {
		t.Error("Get Moving should immediately dismiss the assistant")
	}
	if next != nil {
		t.Errorf("Get Moving returned next step %v, want nil", next)
	}
	if disp != DispositionSkipped {
		t.Errorf("Get Moving disposition = %q, want %q", disp, DispositionSkipped)
	}
}

func TestFlowSelectionConfigureAdvancesToFirstConfigStep(t *testing.T) {
	model := NewAssistantModel(func(page, group string) bool {
		return true
	})

	if model.TotalSteps() < 2 {
		t.Fatalf("expected candidate configuration steps, got %d steps", model.TotalSteps())
	}

	next, dismissed, disp := model.SelectFlow(FlowChoiceConfigure)
	if dismissed {
		t.Error("Configure Everything should not dismiss when steps remain")
	}
	if next == nil {
		t.Fatal("Configure Everything should return the first configuration step")
	}
	if model.CurrentIndex() != 1 {
		t.Errorf("CurrentIndex = %d, want 1", model.CurrentIndex())
	}
	if disp.IsSettled() {
		t.Errorf("expected unsettled disposition while configuring, got %v", disp)
	}
}

func TestFlowSelectionConfigureWithNoConfigStepsCompletes(t *testing.T) {
	model := NewAssistantModel(func(page, group string) bool {
		return false
	})

	if model.TotalSteps() != 1 {
		t.Fatalf("expected only welcome step when all groups disabled, got %d", model.TotalSteps())
	}

	next, dismissed, disp := model.SelectFlow(FlowChoiceConfigure)
	if !dismissed {
		t.Error("Configure Everything should complete and dismiss if no steps exist")
	}
	if next != nil {
		t.Errorf("expected nil next step, got %v", next)
	}
	if disp != DispositionCompleted {
		t.Errorf("expected DispositionCompleted, got %v", disp)
	}
}

func TestStepFilteringHonorsGroupPredicate(t *testing.T) {
	// Enable only livery_group
	model := NewAssistantModel(func(page, group string) bool {
		return group == "livery_group"
	})

	steps := model.Steps()
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps (welcome + appearance), got %d", len(steps))
	}
	if steps[0].ID != StepIDWelcome {
		t.Errorf("step 0 = %q, want %q", steps[0].ID, StepIDWelcome)
	}
	if steps[1].ID != StepIDTheme {
		t.Errorf("step 1 = %q, want %q", steps[1].ID, StepIDTheme)
	}
}

func TestLinearStepNavigationNextAndPrevious(t *testing.T) {
	model := NewAssistantModel(func(page, group string) bool {
		return true
	})

	if model.CanGoBack() {
		t.Error("CanGoBack should be false at start")
	}

	next, dismissed, _ := model.SelectFlow(FlowChoiceConfigure)
	if dismissed || next == nil {
		t.Fatal("expected advance to step 1")
	}
	if !model.CanGoBack() {
		t.Error("CanGoBack should be true after advancing")
	}

	prev, ok := model.Previous()
	if !ok || prev == nil || prev.ID != StepIDWelcome {
		t.Errorf("Previous step = %v, ok = %v, want StepWelcome", prev, ok)
	}

	// Advance through all steps
	for model.HasNext() {
		n, fin, _ := model.Next()
		if fin || n == nil {
			t.Fatalf("unexpected completion at index %d", model.CurrentIndex())
		}
	}

	_, finished, disp := model.Next()
	if !finished {
		t.Error("expected finished = true when stepping past final step")
	}
	if disp != DispositionCompleted {
		t.Errorf("disp = %v, want DispositionCompleted", disp)
	}
}

func TestShouldPresentHonorsDryRunAndExplicitFlags(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name          string
		dryRun        bool
		explicitSetup bool
		disposition   Disposition
		wantPresent   bool
	}{
		{
			name:          "normal first run (not addressed)",
			dryRun:        false,
			explicitSetup: false,
			disposition:   DispositionNotAddressed,
			wantPresent:   true,
		},
		{
			name:          "normal subsequent run (already skipped)",
			dryRun:        false,
			explicitSetup: false,
			disposition:   DispositionSkipped,
			wantPresent:   false,
		},
		{
			name:          "normal subsequent run (already completed)",
			dryRun:        false,
			explicitSetup: false,
			disposition:   DispositionCompleted,
			wantPresent:   false,
		},
		{
			name:          "dry-run auto-presentation suppressed",
			dryRun:        true,
			explicitSetup: false,
			disposition:   DispositionNotAddressed,
			wantPresent:   false,
		},
		{
			name:          "dry-run with explicit setup presents",
			dryRun:        true,
			explicitSetup: true,
			disposition:   DispositionNotAddressed,
			wantPresent:   true,
		},
		{
			name:          "explicit setup re-opens even if previously settled",
			dryRun:        false,
			explicitSetup: true,
			disposition:   DispositionCompleted,
			wantPresent:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore(tc.disposition)
			got := ShouldPresent(ctx, tc.dryRun, tc.explicitSetup, store)
			if got != tc.wantPresent {
				t.Errorf("ShouldPresent(%v, %v, %v) = %v, want %v",
					tc.dryRun, tc.explicitSetup, tc.disposition, got, tc.wantPresent)
			}
		})
	}
}

func TestMemoryStoreGetAndSet(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore(DispositionNotAddressed)

	disp, err := store.GetDisposition(ctx)
	if err != nil {
		t.Fatalf("GetDisposition: %v", err)
	}
	if disp != DispositionNotAddressed {
		t.Errorf("initial disp = %v, want %v", disp, DispositionNotAddressed)
	}

	if err := store.SetDisposition(ctx, DispositionSkipped); err != nil {
		t.Fatalf("SetDisposition: %v", err)
	}

	disp, err = store.GetDisposition(ctx)
	if err != nil {
		t.Fatalf("GetDisposition after set: %v", err)
	}
	if disp != DispositionSkipped {
		t.Errorf("disp = %v, want %v", disp, DispositionSkipped)
	}
}

func TestGSettingsStoreRespectsDryRun(t *testing.T) {
	dryrun.Set(true)
	defer dryrun.Set(false)

	ctx := context.Background()
	store := NewGSettingsStore()

	// In dry-run mode, SetDisposition must succeed without calling external commands
	if err := store.SetDisposition(ctx, DispositionCompleted); err != nil {
		t.Fatalf("SetDisposition in dry-run mode failed: %v", err)
	}
}

func TestEmbeddedAssetsAreNonEmpty(t *testing.T) {
	dino, err := DinosaurLogo()
	if err != nil {
		t.Fatalf("DinosaurLogo: %v", err)
	}
	if len(dino) == 0 {
		t.Error("DinosaurLogo returned empty byte slice")
	}
	if !strings.Contains(string(dino), "<svg") {
		t.Error("DinosaurLogo does not contain SVG root element")
	}

	wordmarkDark, err := Wordmark(true)
	if err != nil {
		t.Fatalf("Wordmark(dark): %v", err)
	}
	if len(wordmarkDark) == 0 {
		t.Error("Wordmark(dark) returned empty byte slice")
	}

	wordmarkLight, err := Wordmark(false)
	if err != nil {
		t.Fatalf("Wordmark(light): %v", err)
	}
	if len(wordmarkLight) == 0 {
		t.Error("Wordmark(light) returned empty byte slice")
	}
}

func TestAssetPathResolvesExistingFile(t *testing.T) {
	path, err := AssetPath(AssetDinosaur)
	if err != nil {
		t.Fatalf("AssetPath: %v", err)
	}
	if path == "" {
		t.Fatal("AssetPath returned empty string")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Errorf("asset file %s is empty", path)
	}
}

// TestAdvanceDisplaysEveryStepBeforeCompleting pins the distinction the view
// got wrong: a move onto the final step must hand that step back for display,
// not report completion because no step follows it.
func TestAdvanceDisplaysEveryStepBeforeCompleting(t *testing.T) {
	model := NewAssistantModel(func(page, group string) bool { return true })

	if _, dismissed, _ := model.SelectFlow(FlowChoiceConfigure); dismissed {
		t.Fatal("configure flow dismissed instead of advancing")
	}

	displayed := []string{model.CurrentStep().ID}
	for {
		step, done := model.Advance()
		if done {
			break
		}
		displayed = append(displayed, step.ID)
	}

	want := []string{StepIDTheme, StepIDApps, StepIDDeveloper, StepIDAI}
	if len(displayed) != len(want) {
		t.Fatalf("displayed steps = %v, want %v", displayed, want)
	}
	for i, id := range want {
		if displayed[i] != id {
			t.Errorf("displayed[%d] = %q, want %q", i, displayed[i], id)
		}
	}
}

// TestAdvanceFromTheOnlyStepCompletes covers the welcome-only sequence, where
// there is genuinely nothing to display next.
func TestAdvanceFromTheOnlyStepCompletes(t *testing.T) {
	model := NewAssistantModel(func(page, group string) bool { return false })

	if step, done := model.Advance(); !done || step.ID != "" {
		t.Errorf("Advance() = (%v, %v), want (zero step, true)", step, done)
	}
}

func TestGetDispositionReportsAMissingSchema(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	calls := 0
	runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		calls++
		return "No such schema “" + SchemaID + "”", errors.New("exit status 1")
	}

	disp, err := NewGSettingsStore().GetDisposition(context.Background())
	if !errors.Is(err, ErrSchemaMissing) {
		t.Errorf("err = %v, want ErrSchemaMissing", err)
	}
	if disp != DispositionNotAddressed {
		t.Errorf("disp = %v, want DispositionNotAddressed", disp)
	}
	if calls != 1 {
		t.Errorf("gsettings spawned %d times, want 1", calls)
	}
}

func TestGetDispositionReadsEveryKeyInOneSpawn(t *testing.T) {
	original := runCommand
	defer func() { runCommand = original }()

	calls := 0
	runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
		calls++
		if len(args) < 2 || args[0] != "list-recursively" {
			t.Errorf("unexpected gsettings invocation %v", args)
		}
		return SchemaID + " " + KeyCompletedVersion + " '1.2.3'\n" +
			SchemaID + " " + KeyDisposition + " ''\n", nil
	}

	disp, err := NewGSettingsStore().GetDisposition(context.Background())
	if err != nil {
		t.Fatalf("GetDisposition: %v", err)
	}
	if disp != DispositionCompleted {
		t.Errorf("disp = %v, want DispositionCompleted (completed-version set by an earlier build)", disp)
	}
	if calls != 1 {
		t.Errorf("gsettings spawned %d times, want 1", calls)
	}
}
