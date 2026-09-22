package updateproviders

import (
	"context"
	"errors"
	"strings"

	"github.com/projectbluefin/chairlift/internal/config"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// CleanupGroup is the single configuration group governing routine cleanup.
// It gates both surfaces: the Maintenance page's one "Free up space" button
// and the post-update cleanup phase of an Update All run. One key for both
// is deliberate — a user who turns cleanup off has turned cleanup off, and
// two independent keys would let one surface clean while the other claimed
// the feature was disabled.
const CleanupGroup = "maintenance_freespace_group"

// MaintenanceDeps contains the typed cleanup operations used by automatic
// post-update maintenance and by the manual cleanup action.
type MaintenanceDeps struct {
	HomebrewInstalled func() bool
	HomebrewCleanup   func() (string, error)
	FlatpakInstalled  func() bool
	FlatpakCleanup    func() error
}

// StepID identifies one unit of the cleanup inventory. The inventory is
// closed on purpose: this is the whole of what "Free up space" does, so an
// administrator-configured script can never be folded into it.
type StepID string

const (
	// StepOldDownloads removes package archives and stale versions the
	// developer-tools package manager has cached.
	StepOldDownloads StepID = "old-downloads"
	// StepUnusedSupport removes application runtimes and extensions that no
	// installed application still depends on.
	StepUnusedSupport StepID = "unused-support"
)

// StepOutcome is what happened to one step.
type StepOutcome string

const (
	// OutcomeCleaned means the step ran and completed.
	OutcomeCleaned StepOutcome = "cleaned"
	// OutcomeSkipped means there was nothing for the step to do — the tool
	// it cleans up after is not installed, or cleanup is disabled. That is
	// not a failure and not a success.
	OutcomeSkipped StepOutcome = "skipped"
	// OutcomeFailed means the step ran and did not complete.
	OutcomeFailed StepOutcome = "failed"
	// OutcomeCancelled means the step was abandoned rather than refused —
	// the user dismissed an authentication prompt, or the run's context was
	// cancelled. It is reported apart from OutcomeFailed because nothing
	// went wrong, but nothing was cleaned either.
	OutcomeCancelled StepOutcome = "cancelled"
)

// StepResult is one step's outcome. Detail is diagnostic text for the log,
// never for a user: it carries raw tool output and error strings. Err is
// the underlying failure, kept so the update-flow phase can return the
// provider's own error rather than a re-wrapped copy of its text.
type StepResult struct {
	ID      StepID
	Outcome StepOutcome
	Detail  string
	Err     error
}

// cleanupSteps is the canonical, ordered inventory.
var cleanupSteps = []StepID{StepOldDownloads, StepUnusedSupport}

// Cleanup is the typed cleanup runner. It satisfies updateflow.Maintenance
// through Run, and exposes the same inventory per step through RunSteps so
// the Maintenance page can report each provider honestly instead of
// collapsing a partial failure into one error.
type Cleanup struct {
	cfg  *config.Config
	deps MaintenanceDeps
}

// NewMaintenance constructs the production typed maintenance runner.
func NewMaintenance(cfg *config.Config) updateflow.Maintenance {
	return NewCleanup(cfg)
}

// NewCleanup constructs the production cleanup runner.
func NewCleanup(cfg *config.Config) *Cleanup {
	return newMaintenance(cfg, MaintenanceDeps{
		HomebrewInstalled: homebrew.IsInstalledCached,
		HomebrewCleanup:   homebrew.Cleanup,
		FlatpakInstalled:  flatpak.IsInstalledCached,
		FlatpakCleanup: func() error {
			_, err := flatpak.UninstallUnused()
			return err
		},
	})
}

func newMaintenance(cfg *config.Config, deps MaintenanceDeps) *Cleanup {
	return &Cleanup{cfg: cfg, deps: deps}
}

// Enabled reports whether routine cleanup is available at all.
func (c *Cleanup) Enabled() bool {
	if c.cfg == nil {
		return false
	}
	settings, ok := c.cfg.MaintenancePage[CleanupGroup]
	return ok && settings.Enabled
}

// Run executes the cleanup inventory as one update-flow phase. It stops at
// the first step that fails, because a post-update run reports a single
// error to the flow coordinator and continuing past a failure would bury it.
func (c *Cleanup) Run(ctx context.Context, progress func(updateflow.Progress)) error {
	for _, id := range cleanupSteps {
		result := c.runStep(ctx, id, progress)
		if result.Outcome != OutcomeFailed && result.Outcome != OutcomeCancelled {
			continue
		}
		if result.Err != nil {
			return result.Err
		}
		return errors.New(result.Detail)
	}
	return nil
}

// RunSteps executes every step and returns one result each. Unlike Run a
// failing step does not abort the run: the two steps are independent, so a
// failure cleaning up old downloads must not leave unused supporting
// software in place, and the caller needs both outcomes to tell the user
// which half actually happened.
func (c *Cleanup) RunSteps(ctx context.Context) []StepResult {
	results := make([]StepResult, 0, len(cleanupSteps))
	for _, id := range cleanupSteps {
		results = append(results, c.runStep(ctx, id, nil))
	}
	return results
}

func (c *Cleanup) runStep(ctx context.Context, id StepID, progress func(updateflow.Progress)) StepResult {
	if err := ctx.Err(); err != nil {
		return StepResult{ID: id, Outcome: OutcomeCancelled, Detail: err.Error(), Err: err}
	}
	if !c.Enabled() {
		return StepResult{ID: id, Outcome: OutcomeSkipped, Detail: "cleanup is disabled in configuration"}
	}

	switch id {
	case StepOldDownloads:
		if c.deps.HomebrewInstalled == nil || !c.deps.HomebrewInstalled() {
			return StepResult{ID: id, Outcome: OutcomeSkipped, Detail: "homebrew is not installed"}
		}
		if c.deps.HomebrewCleanup == nil {
			return StepResult{ID: id, Outcome: OutcomeFailed, Detail: "homebrew cleanup is unavailable"}
		}
		report(progress, updateflow.Progress{
			Source:  updateflow.DeveloperTools,
			Message: "Removing old downloads",
		})
		output, err := c.deps.HomebrewCleanup()
		if err != nil {
			return StepResult{ID: id, Outcome: classify(err), Detail: err.Error(), Err: err}
		}
		return StepResult{ID: id, Outcome: OutcomeCleaned, Detail: output}

	case StepUnusedSupport:
		if c.deps.FlatpakInstalled == nil || !c.deps.FlatpakInstalled() {
			return StepResult{ID: id, Outcome: OutcomeSkipped, Detail: "flatpak is not installed"}
		}
		if c.deps.FlatpakCleanup == nil {
			return StepResult{ID: id, Outcome: OutcomeFailed, Detail: "flatpak cleanup is unavailable"}
		}
		report(progress, updateflow.Progress{
			Source:  updateflow.Applications,
			Message: "Removing unused supporting software",
		})
		if err := c.deps.FlatpakCleanup(); err != nil {
			return StepResult{ID: id, Outcome: classify(err), Detail: err.Error(), Err: err}
		}
		return StepResult{ID: id, Outcome: OutcomeCleaned, Detail: ""}

	default:
		return StepResult{ID: id, Outcome: OutcomeFailed, Detail: "unknown cleanup step"}
	}
}

func report(progress func(updateflow.Progress), event updateflow.Progress) {
	if progress != nil {
		progress(event)
	}
}

// classify separates an abandoned step from a failed one. A user who
// dismisses an authentication prompt has not hit a bug, and reporting that
// as a failure is as dishonest as reporting it as a success.
func classify(err error) StepOutcome {
	if err == nil {
		return OutcomeCleaned
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return OutcomeCancelled
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"cancel", "dismissed", "not authorized", "authentication"} {
		if strings.Contains(text, marker) {
			return OutcomeCancelled
		}
	}
	return OutcomeFailed
}
