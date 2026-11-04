// Package devsetup owns the ordered, widget-free setup run for Features.
package devsetup

import (
	"context"
	"errors"

	"github.com/projectbluefin/chairlift/internal/pkexec"
)

const DevcontainerPackage = "devcontainer"

type Stage string

const (
	Access       Stage = "Developer access"
	Devcontainer Stage = "Dev Container CLI"
	Pulp         Stage = "Pulp reader"
	Feeds        Stage = "Developer feeds"
	Apps         Stage = "Developer apps"
	Tools        Stage = "Tools and service"
	Verify       Stage = "Verify readiness"
	VSCode       Stage = "VS Code setup"
)

var Stages = [...]Stage{Access, Tools, Verify, VSCode, Devcontainer, Pulp, Feeds, Apps}

type PostOptions struct {
	Homebrew    bool
	InstallPulp bool
	StageFeeds  bool
	InstallApps bool
	Collections bool
}

// PostStages is requested only for a promotion (or its failed-step retry).
// Run still requires the grant to succeed before dispatching any of them.
// Preview logs Dev Container CLI through Homebrew's own dry-run boundary;
// opted-in Flatpak, catalog and collection work is live-promotion-only.
func PostStages(preview, promotion bool, options PostOptions) []Stage {
	if !promotion {
		return nil
	}
	var stages []Stage
	if options.Homebrew {
		stages = append(stages, Devcontainer)
	}
	if preview {
		return stages
	}
	if options.InstallPulp {
		stages = append(stages, Pulp)
	}
	if options.StageFeeds {
		stages = append(stages, Feeds)
	}
	if options.Homebrew && options.InstallApps && options.Collections {
		stages = append(stages, Apps)
	}
	return stages
}

type Status string

const (
	Waiting       Status = "Waiting"
	Working       Status = "Working…"
	Complete      Status = "Complete"
	Failed        Status = "Failed"
	Cancelled     Status = "Canceled"
	Preview       Status = "Preview"
	AwaitingLogin Status = "Awaiting new login"
	Skipped       Status = "Skipped"
)

// Awaiting login is a real activation awaiting a new session, not a failure
// that another privileged enable can fix. Skipped tasks did not run.
var (
	ErrAwaitingLogin = errors.New("setup requires a new login")
	ErrSkipped       = errors.New("setup step does not apply")
)

type Task struct {
	Stage Stage
	// Optional setup success survives a retry; service/readiness work always
	// re-reads current state instead of trusting a previous observation.
	Optional bool
	Run      func() error
}

type Request struct {
	Developer       bool
	ChangeDeveloper bool
	EnableDeveloper bool
	Preview         bool
}

// ChildRequest promotes only for enable; stopping a child never demotes the
// account, and an already-granted account needs no additional authentication.
func ChildRequest(developer, enable, preview bool) Request {
	return Request{Developer: developer, ChangeDeveloper: enable && !developer, EnableDeveloper: true, Preview: preview}
}

type Result struct {
	Developer     bool
	Promoted      bool
	AwaitingLogin bool
	Completed     map[Stage]bool
	Err           error
}

// Run grants access before any child setup. A refused grant aborts everything;
// later failures never revoke access or prevent independent opt-ins, except
// cancellation, which stops dispatch. The caller owns callback thread dispatch.
func Run(ctx context.Context, request Request, change func() error, tasks []Task, previous map[Stage]bool, report func(Stage, Status)) Result {
	result := Result{Developer: request.Developer, Completed: make(map[Stage]bool)}
	if !request.Preview {
		for stage, done := range previous {
			result.Completed[stage] = done
		}
	}
	record := func(stage Stage, err error) {
		status := Complete
		switch {
		case errors.Is(err, ErrAwaitingLogin):
			delete(result.Completed, stage)
			status = AwaitingLogin
			result.AwaitingLogin = true
		case errors.Is(err, ErrSkipped):
			delete(result.Completed, stage)
			status = Skipped
		case err != nil:
			delete(result.Completed, stage)
			status = Failed
			if errors.Is(err, context.Canceled) || pkexec.IsAuthDismissed(err) {
				status = Cancelled
			}
			result.Err = errors.Join(result.Err, err)
		case request.Preview:
			delete(result.Completed, stage)
			status = Preview
		default:
			result.Completed[stage] = true
		}
		report(stage, status)
	}
	if request.ChangeDeveloper && request.Developer != request.EnableDeveloper {
		if err := ctx.Err(); err != nil {
			record(Access, err)
			return result
		}
		report(Access, Working)
		err := change()
		record(Access, err)
		if err != nil {
			return result
		}
		if !request.Preview {
			result.Developer = request.EnableDeveloper
			result.Promoted = request.EnableDeveloper && !request.Developer
		}
	}
	for _, task := range tasks {
		if task.Optional && result.Completed[task.Stage] {
			report(task.Stage, Complete)
			continue
		}
		if err := ctx.Err(); err != nil {
			record(task.Stage, err)
			break
		}
		report(task.Stage, Working)
		err := task.Run()
		record(task.Stage, err)
		if errors.Is(err, context.Canceled) || pkexec.IsAuthDismissed(err) {
			break
		}
	}
	return result
}
