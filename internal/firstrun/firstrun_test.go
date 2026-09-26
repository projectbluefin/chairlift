package firstrun

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

// errStore answers every read with a fixed error.
type errStore struct{ err error }

func (e errStore) GetDisposition(ctx context.Context) (Disposition, error) {
	return DispositionNotAddressed, e.err
}

func (e errStore) SetDisposition(ctx context.Context, d Disposition) error { return e.err }

// TestShouldPresentStaysSilentWhenTheSchemaIsMissing covers the one error a
// user cannot settle: with no compiled schema, "Get Moving" writes nothing,
// so presenting anyway means presenting on every launch forever.
func TestShouldPresentStaysSilentWhenTheSchemaIsMissing(t *testing.T) {
	ctx := context.Background()
	store := errStore{err: ErrSchemaMissing}

	if ShouldPresent(ctx, false, false, store) {
		t.Error("ShouldPresent = true for a missing schema, want false")
	}
	if !ShouldPresent(ctx, false, true, store) {
		t.Error("explicit setup must present even with a missing schema")
	}
	if !ShouldPresent(ctx, false, false, errStore{err: errors.New("gsettings exploded")}) {
		t.Error("an ordinary read failure must still present")
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
	wordmarkDark, err := Wordmark(true)
	if err != nil {
		t.Fatalf("Wordmark(dark): %v", err)
	}
	if len(wordmarkDark) == 0 {
		t.Error("Wordmark(dark) returned empty byte slice")
	}
	if !strings.Contains(string(wordmarkDark), "<svg") {
		t.Error("Wordmark(dark) does not contain SVG root element")
	}
	wordmarkLight, err := Wordmark(false)
	if err != nil {
		t.Fatalf("Wordmark(light): %v", err)
	}
	if len(wordmarkLight) == 0 {
		t.Error("Wordmark(light) returned empty byte slice")
	}
	if !strings.Contains(string(wordmarkLight), "<svg") {
		t.Error("Wordmark(light) does not contain SVG root element")
	}
}
func TestAssetPathResolvesExistingFile(t *testing.T) {
	path, err := AssetPath(AssetWordmarkDark)
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

	want := []string{StepIDTheme, StepIDApps, StepIDUpdates}
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

// TestForwardFinishesOnlyOnTheFinalStep pins the answer the view labels its
// forward button with: "Finish" on an intermediate step misdescribes a click
// that merely shows the next step.
func TestForwardFinishesOnlyOnTheFinalStep(t *testing.T) {
	model := NewAssistantModel(func(page, group string) bool { return true })

	if _, dismissed, _ := model.SelectFlow(FlowChoiceConfigure); dismissed {
		t.Fatal("configure flow dismissed instead of advancing")
	}

	for {
		step := model.CurrentStep()
		finishes := model.ForwardFinishes()

		next, done := model.Advance()
		if finishes != done {
			t.Errorf("step %q: ForwardFinishes() = %v, but Advance() done = %v",
				step.ID, finishes, done)
		}
		if done {
			break
		}
		_ = next
	}
}

// TestForwardFinishesOnWelcomeOnlySequence covers the sequence with no
// configuration steps, where the first forward click genuinely finishes.
func TestForwardFinishesOnWelcomeOnlySequence(t *testing.T) {
	model := NewAssistantModel(func(page, group string) bool { return false })
	if !model.ForwardFinishes() {
		t.Error("ForwardFinishes() = false on a welcome-only sequence, want true")
	}
}

// TestSkipPreservingKeepsARecordedCompletion holds the re-entry case: the
// assistant reopens from the menu and from --setup after setup finished, and
// GetDisposition prefers the disposition key over the completed version, so
// writing skipped there would regress a finished setup permanently.
func TestSkipPreservingKeepsARecordedCompletion(t *testing.T) {
	for _, tc := range []struct {
		current Disposition
		want    Disposition
	}{
		{DispositionNotAddressed, DispositionSkipped},
		{DispositionSkipped, DispositionSkipped},
		{DispositionCompleted, DispositionCompleted},
	} {
		if got := SkipPreserving(tc.current); got != tc.want {
			t.Errorf("SkipPreserving(%q) = %q, want %q", tc.current, got, tc.want)
		}
	}
}

// TestCleanupAssetsRemovesTheExtractionDirectory keeps the extraction
// directory from outliving the process that created it.
func TestCleanupAssetsRemovesTheExtractionDirectory(t *testing.T) {
	path, err := AssetPath(AssetWordmarkDark)
	if err != nil {
		t.Fatalf("AssetPath: %v", err)
	}
	dir := filepath.Dir(path)

	if err := CleanupAssets(); err != nil {
		t.Fatalf("CleanupAssets: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("asset directory %s still present after cleanup (err = %v)", dir, err)
	}

	// A second call has nothing to remove and must not report failure.
	if err := CleanupAssets(); err != nil {
		t.Errorf("second CleanupAssets: %v", err)
	}

	// Cleanup does not disable extraction: a later presentation re-extracts.
	again, err := AssetPath(AssetWordmarkDark)
	if err != nil {
		t.Fatalf("AssetPath after cleanup: %v", err)
	}
	if _, err := os.Stat(again); err != nil {
		t.Errorf("stat re-extracted asset %s: %v", again, err)
	}
	t.Cleanup(func() { _ = CleanupAssets() })
}

// TestWelcomeCopyHasOneOwner keeps the hero screen's copy from being restated
// in the presentation package, where the two would silently drift apart.
func TestWelcomeCopyHasOneOwner(t *testing.T) {
	if StepWelcome.Title != WelcomeStepTitle {
		t.Errorf("StepWelcome.Title = %q, want %q", StepWelcome.Title, WelcomeStepTitle)
	}
	if StepWelcome.Description != WelcomeStepDescription {
		t.Errorf("StepWelcome.Description = %q, want %q", StepWelcome.Description, WelcomeStepDescription)
	}
}
