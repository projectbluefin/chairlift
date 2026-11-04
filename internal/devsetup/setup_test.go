package devsetup

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"testing"
)

func TestParentGrantPrecedesEveryChildAndOptIn(t *testing.T) {
	for _, child := range []string{"docker", "wsl"} {
		t.Run(child, func(t *testing.T) {
			var calls []string
			var statuses []Status
			result := Run(context.Background(), ChildRequest(false, true, false), func() error {
				calls = append(calls, "dx-enable")
				return nil
			}, []Task{
				{Stage: Tools, Run: func() error { calls = append(calls, child); return nil }},
				{Stage: Verify, Run: func() error { calls = append(calls, "observe"); return nil }},
				{Stage: Devcontainer, Optional: true, Run: func() error { calls = append(calls, "devcontainer"); return nil }},
			}, nil, func(_ Stage, status Status) { statuses = append(statuses, status) })
			if !reflect.DeepEqual(calls, []string{"dx-enable", child, "observe", "devcontainer"}) {
				t.Fatalf("calls = %v", calls)
			}
			if !result.Developer || !result.Promoted || result.Err != nil || len(result.Completed) != 4 {
				t.Fatalf("result = %+v", result)
			}
			if !reflect.DeepEqual(statuses, []Status{Working, Complete, Working, Complete, Working, Complete, Working, Complete}) {
				t.Fatalf("statuses = %v", statuses)
			}
		})
	}
}

func TestGrantRefusalAbortsChildrenAndOptIns(t *testing.T) {
	// The actual exit-126 classifier matters as well as dismissal text: a
	// helper may supply only a wrapped ExitError, with no stderr attached.
	_, exit126 := exec.Command("sh", "-c", "exit 126").Output()
	for _, tc := range []struct {
		name   string
		err    error
		status Status
	}{
		{"auth failed", errors.New("authentication failed"), Failed},
		{"auth dismissed", errors.New("Request dismissed"), Cancelled},
		{"auth exit", exit126, Cancelled},
		{"context canceled", context.Canceled, Cancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var childCalls int
			var statuses []Status
			result := Run(context.Background(), Request{ChangeDeveloper: true, EnableDeveloper: true}, func() error { return tc.err }, []Task{
				{Stage: Tools, Run: func() error { childCalls++; return nil }},
				{Stage: Pulp, Optional: true, Run: func() error { childCalls++; return nil }},
			}, nil, func(_ Stage, status Status) { statuses = append(statuses, status) })
			if result.Developer || result.Promoted || !errors.Is(result.Err, tc.err) || childCalls != 0 || len(result.Completed) != 0 {
				t.Fatalf("result = %+v, children = %d", result, childCalls)
			}
			if !reflect.DeepEqual(statuses, []Status{Working, tc.status}) {
				t.Fatalf("statuses = %v", statuses)
			}
		})
	}
}

func TestAlreadyGrantedAndChildDisableNeverChangeParent(t *testing.T) {
	for _, developer := range []bool{false, true} {
		for _, enable := range []bool{false, true} {
			var grants, children int
			result := Run(context.Background(), ChildRequest(developer, enable, false), func() error { grants++; return nil }, []Task{
				{Stage: Tools, Run: func() error { children++; return nil }},
			}, nil, func(Stage, Status) {})
			wantGrants := 0
			if enable && !developer {
				wantGrants = 1
			}
			if grants != wantGrants || children != 1 || result.Developer != (developer || enable) || result.Promoted != (wantGrants == 1) || result.Err != nil {
				t.Fatalf("developer=%v enable=%v: grants=%d children=%d result=%+v", developer, enable, grants, children, result)
			}
		}
	}
}

func TestChildFailureKeepsParentAndIndependentOptIns(t *testing.T) {
	failure := errors.New("socket not ready")
	var opted bool
	result := Run(context.Background(), ChildRequest(false, true, false), func() error { return nil }, []Task{
		{Stage: Tools, Run: func() error { return failure }},
		{Stage: Feeds, Optional: true, Run: func() error { opted = true; return nil }},
	}, nil, func(Stage, Status) {})
	if !result.Developer || !result.Promoted || !opted || !errors.Is(result.Err, failure) || result.Completed[Tools] || !result.Completed[Feeds] {
		t.Fatalf("result = %+v, opted=%v", result, opted)
	}
}

func TestRetryKeepsCompletedOptInsButReobservesService(t *testing.T) {
	var grants, service, successful, flaky int
	failure := errors.New("download failed")
	tasks := []Task{
		{Stage: Tools, Run: func() error { service++; return nil }},
		{Stage: Devcontainer, Optional: true, Run: func() error { successful++; return nil }},
		{Stage: Apps, Optional: true, Run: func() error {
			flaky++
			if flaky == 1 {
				return failure
			}
			return nil
		}},
	}
	grant := func() error { grants++; return nil }
	first := Run(context.Background(), Request{ChangeDeveloper: true, EnableDeveloper: true}, grant, tasks, nil, func(Stage, Status) {})
	second := Run(context.Background(), Request{Developer: first.Developer, ChangeDeveloper: true, EnableDeveloper: true}, grant, tasks, first.Completed, func(Stage, Status) {})
	if grants != 1 || service != 2 || successful != 1 || flaky != 2 || second.Err != nil || !second.Developer || second.Promoted || !second.Completed[Apps] {
		t.Fatalf("grants=%d service=%d successful=%d flaky=%d result=%+v", grants, service, successful, flaky, second)
	}
	if !first.Completed[Devcontainer] || first.Completed[Apps] {
		t.Fatal("retry mutated the previous outcome")
	}
}

func TestPreviewDoesNotConfirmParentOrCompletedStages(t *testing.T) {
	for _, developer := range []bool{false, true} {
		var calls int
		var statuses []Status
		result := Run(context.Background(), Request{Developer: developer, ChangeDeveloper: true, EnableDeveloper: !developer, Preview: true}, func() error { calls++; return nil }, []Task{
			{Stage: Tools, Run: func() error { calls++; return nil }},
		}, nil, func(_ Stage, status Status) { statuses = append(statuses, status) })
		if result.Developer != developer || result.Promoted || len(result.Completed) != 0 || result.Err != nil || calls != 2 {
			t.Fatalf("result = %+v, calls=%d", result, calls)
		}
		if !reflect.DeepEqual(statuses, []Status{Working, Preview, Working, Preview}) {
			t.Fatalf("statuses = %v", statuses)
		}
	}
}

func TestCanceledContextDoesNotDispatchGrant(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := Run(ctx, Request{ChangeDeveloper: true, EnableDeveloper: true}, func() error { t.Fatal("grant called"); return nil }, nil, nil, func(Stage, Status) {})
	if !errors.Is(result.Err, context.Canceled) || result.Developer {
		t.Fatalf("result = %+v", result)
	}
}

func TestPostStagesRespectsPromotionPreviewAndOptIns(t *testing.T) {
	all := PostOptions{Homebrew: true, InstallPulp: true, StageFeeds: true, InstallApps: true, Collections: true}
	for _, tc := range []struct {
		name               string
		preview, promotion bool
		options            PostOptions
		want               []Stage
	}{
		{"confirmed promotion", false, true, all, []Stage{Devcontainer, Pulp, Feeds, Apps}},
		{"no promotion", false, false, all, nil},
		{"preview", true, true, all, []Stage{Devcontainer}},
		{"defaults", false, true, PostOptions{Homebrew: true, Collections: true}, []Stage{Devcontainer}},
		{"no tools", false, true, PostOptions{}, nil},
		{"Pulp only", false, true, PostOptions{InstallPulp: true}, []Stage{Pulp}},
		{"feeds only", false, true, PostOptions{StageFeeds: true}, []Stage{Feeds}},
		{"apps unavailable", false, true, PostOptions{InstallApps: true}, nil},
		{"apps available", false, true, PostOptions{Homebrew: true, InstallApps: true, Collections: true}, []Stage{Devcontainer, Apps}},
		{"apps without Homebrew", false, true, PostOptions{InstallApps: true, Collections: true}, nil},
		{"apps with collections disabled", false, true, PostOptions{Homebrew: true, InstallApps: true}, []Stage{Devcontainer}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PostStages(tc.preview, tc.promotion, tc.options); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("PostStages = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOptionalFailuresNeverUndoGrantedAccess(t *testing.T) {
	for _, failed := range []Stage{Devcontainer, Pulp, Feeds, Apps} {
		t.Run(string(failed), func(t *testing.T) {
			var grants int
			failure := errors.New("optional install failed")
			var tasks []Task
			for _, stage := range PostStages(false, true, PostOptions{Homebrew: true, InstallPulp: true, StageFeeds: true, InstallApps: true, Collections: true}) {
				tasks = append(tasks, Task{Stage: stage, Optional: true, Run: func() error {
					if stage == failed {
						return failure
					}
					return nil
				}})
			}
			result := Run(context.Background(), Request{ChangeDeveloper: true, EnableDeveloper: true}, func() error { grants++; return nil }, tasks, nil, func(Stage, Status) {})
			if grants != 1 || !result.Developer || !result.Promoted || !errors.Is(result.Err, failure) || len(result.Completed) != 4 || result.Completed[failed] {
				t.Fatalf("grants=%d result=%+v", grants, result)
			}
		})
	}
}

func TestRetryReplacesStaleServiceSuccessWithFailure(t *testing.T) {
	failure := errors.New("service stopped externally")
	optionalFailure := errors.New("optional download failed")
	first := Run(context.Background(), ChildRequest(true, true, false), nil, []Task{
		{Stage: Tools, Run: func() error { return nil }},
		{Stage: Verify, Run: func() error { return nil }},
		{Stage: Devcontainer, Optional: true, Run: func() error { return nil }},
		{Stage: Apps, Optional: true, Run: func() error { return optionalFailure }},
	}, nil, func(Stage, Status) {})
	if !errors.Is(first.Err, optionalFailure) || !first.Completed[Tools] || !first.Completed[Verify] {
		t.Fatalf("first result = %+v", first)
	}
	result := Run(context.Background(), ChildRequest(true, true, false), nil, []Task{
		{Stage: Tools, Run: func() error { return failure }},
		{Stage: Verify, Run: func() error { return failure }},
		{Stage: Devcontainer, Optional: true, Run: func() error { t.Fatal("completed install repeated"); return nil }},
		{Stage: Apps, Optional: true, Run: func() error { return nil }},
	}, first.Completed, func(Stage, Status) {})
	if !errors.Is(result.Err, failure) || result.Completed[Tools] || result.Completed[Verify] || !result.Completed[Devcontainer] || !result.Completed[Apps] || !first.Completed[Tools] || !first.Completed[Verify] || first.Completed[Apps] {
		t.Fatalf("result = %+v, previous=%v", result, first.Completed)
	}
}

func TestCancellationAfterGrantPreservesAccessAndStopsRemainingWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := Run(ctx, Request{ChangeDeveloper: true, EnableDeveloper: true}, func() error { return nil }, []Task{
		{Stage: Tools, Run: func() error { cancel(); return context.Canceled }},
		{Stage: Feeds, Optional: true, Run: func() error { t.Fatal("work continued after cancellation"); return nil }},
	}, nil, func(Stage, Status) {})
	if !result.Developer || !errors.Is(result.Err, context.Canceled) || result.Completed[Tools] || result.Completed[Feeds] {
		t.Fatalf("result = %+v", result)
	}
}

func TestCanceledTaskStopsWithoutCancelingOuterContext(t *testing.T) {
	_, exit126 := exec.Command("sh", "-c", "exit 126").Output()
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"auth dismissed", errors.New("Request dismissed")},
		{"auth exit", exit126},
		{"context canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			statuses := make(map[Stage]Status)
			result := Run(ctx, ChildRequest(false, true, false), func() error { return nil }, []Task{
				{Stage: Tools, Run: func() error { return nil }},
				{Stage: Verify, Run: func() error { return tc.err }},
				{Stage: VSCode, Optional: true, Run: func() error { t.Fatal("child continued after cancellation"); return nil }},
				{Stage: Feeds, Optional: true, Run: func() error { t.Fatal("opt-in continued after cancellation"); return nil }},
			}, nil, func(stage Stage, status Status) { statuses[stage] = status })
			if ctx.Err() != nil || !result.Developer || !result.Promoted || !errors.Is(result.Err, tc.err) || !result.Completed[Access] || !result.Completed[Tools] || result.Completed[Verify] || result.Completed[VSCode] || result.Completed[Feeds] {
				t.Fatalf("result=%+v context=%v", result, ctx.Err())
			}
			if statuses[Verify] != Cancelled || len(statuses) != 3 {
				t.Fatalf("statuses = %v", statuses)
			}
		})
	}
}

func TestPreviewDoesNotReuseCompletedHistory(t *testing.T) {
	previous := map[Stage]bool{Tools: true, Devcontainer: true}
	var calls int
	result := Run(context.Background(), ChildRequest(true, true, true), nil, []Task{
		{Stage: Tools, Run: func() error { calls++; return nil }},
		{Stage: Devcontainer, Optional: true, Run: func() error { calls++; return nil }},
	}, previous, func(_ Stage, status Status) {
		if status == Complete {
			t.Fatal("preview reported complete")
		}
	})
	if result.Err != nil || calls != 2 || len(result.Completed) != 0 || !previous[Tools] || !previous[Devcontainer] {
		t.Fatalf("result=%+v calls=%d previous=%v", result, calls, previous)
	}
}

func TestAwaitingLoginKeepsActivationAndIndependentOptIns(t *testing.T) {
	for _, editorFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "editor complete", true: "editor failure"}[editorFails], func(t *testing.T) {
			editorFailure := errors.New("editor diagnostic")
			var stages []Stage
			statuses := make(map[Stage]Status)
			result := Run(context.Background(), ChildRequest(false, true, false), func() error { return nil }, []Task{
				{Stage: Tools, Run: func() error { stages = append(stages, Tools); return nil }},
				{Stage: Verify, Run: func() error { stages = append(stages, Verify); return ErrAwaitingLogin }},
				{Stage: VSCode, Optional: true, Run: func() error {
					stages = append(stages, VSCode)
					if editorFails {
						return editorFailure
					}
					return nil
				}},
				{Stage: Feeds, Optional: true, Run: func() error { stages = append(stages, Feeds); return nil }},
			}, nil, func(stage Stage, status Status) { statuses[stage] = status })
			if !result.Developer || !result.Promoted || !result.AwaitingLogin || !result.Completed[Tools] || result.Completed[Verify] || !result.Completed[Feeds] {
				t.Fatalf("result = %+v", result)
			}
			if statuses[Verify] != AwaitingLogin || result.Completed[VSCode] == editorFails || errors.Is(result.Err, editorFailure) != editorFails {
				t.Fatalf("result = %+v, statuses=%v", result, statuses)
			}
			if errors.Is(result.Err, ErrAwaitingLogin) || (!editorFails && result.Err != nil) {
				t.Fatalf("login alone became a retryable error: %v", result.Err)
			}
			if !reflect.DeepEqual(stages, []Stage{Tools, Verify, VSCode, Feeds}) {
				t.Fatalf("stages = %v", stages)
			}
		})
	}
}

func TestAwaitingLoginRetryRetainsCompletedActivationButNotReadiness(t *testing.T) {
	previous := map[Stage]bool{Access: true, Tools: true, VSCode: true, Verify: true}
	var verified, independent int
	result := Run(context.Background(), ChildRequest(true, true, false), func() error {
		t.Fatal("parent grant repeated")
		return nil
	}, []Task{
		{Stage: Tools, Run: func() error {
			if !previous[Tools] {
				t.Fatal("completed activation lost before retry")
			}
			return nil
		}},
		{Stage: Verify, Run: func() error { verified++; return ErrAwaitingLogin }},
		{Stage: VSCode, Optional: true, Run: func() error { t.Fatal("successful editor setup repeated"); return nil }},
		{Stage: Apps, Optional: true, Run: func() error { independent++; return nil }},
	}, previous, func(Stage, Status) {})
	if result.Err != nil || !result.Developer || result.Promoted || !result.AwaitingLogin || !result.Completed[Tools] || result.Completed[Verify] || !result.Completed[VSCode] || !result.Completed[Apps] || verified != 1 || independent != 1 {
		t.Fatalf("result=%+v verified=%d independent=%d", result, verified, independent)
	}
	if !previous[Verify] {
		t.Fatal("retry mutated prior completion map")
	}
}

func TestWSLPendingLoginRetainsParentAndIndependentOptInOutcomes(t *testing.T) {
	for _, optInFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "opt-in complete", true: "opt-in failure"}[optInFails], func(t *testing.T) {
			failure := errors.New("developer apps failed")
			var grants, apps, feeds int
			statuses := make(map[Stage]Status)
			tasks := []Task{
				{Stage: Tools, Run: func() error { return ErrAwaitingLogin }},
				{Stage: Verify, Run: func() error { return ErrAwaitingLogin }},
				{Stage: Apps, Optional: true, Run: func() error {
					apps++
					if optInFails && apps == 1 {
						return failure
					}
					return nil
				}},
				{Stage: Feeds, Optional: true, Run: func() error { feeds++; return nil }},
			}
			grant := func() error { grants++; return nil }
			first := Run(context.Background(), ChildRequest(false, true, false), grant, tasks, nil, func(stage Stage, status Status) { statuses[stage] = status })
			if !first.Developer || !first.Promoted || !first.AwaitingLogin || !first.Completed[Access] || first.Completed[Tools] || first.Completed[Verify] || !first.Completed[Feeds] || first.Completed[Apps] == optInFails || errors.Is(first.Err, failure) != optInFails {
				t.Fatalf("first = %+v", first)
			}
			if statuses[Tools] != AwaitingLogin || statuses[Verify] != AwaitingLogin || errors.Is(first.Err, ErrAwaitingLogin) || (!optInFails && first.Err != nil) {
				t.Fatalf("first=%+v statuses=%v", first, statuses)
			}
			second := Run(context.Background(), ChildRequest(first.Developer, true, false), grant, tasks, first.Completed, func(Stage, Status) {})
			wantApps := 1
			if optInFails {
				wantApps = 2
			}
			if grants != 1 || feeds != 1 || apps != wantApps || !second.Developer || second.Promoted || !second.AwaitingLogin || second.Err != nil || second.Completed[Tools] || second.Completed[Verify] || !second.Completed[Apps] || !second.Completed[Feeds] {
				t.Fatalf("second=%+v grants=%d apps=%d feeds=%d", second, grants, apps, feeds)
			}
		})
	}
}

func TestSkippedEditorNeverClaimsCompletionOrBlocksIndependentTasks(t *testing.T) {
	activationFailure := errors.New("Docker helper refused")
	var statuses []Status
	result := Run(context.Background(), ChildRequest(true, true, false), nil, []Task{
		{Stage: Tools, Run: func() error { return activationFailure }},
		{Stage: VSCode, Optional: true, Run: func() error { return ErrSkipped }},
		{Stage: Feeds, Optional: true, Run: func() error { return nil }},
	}, nil, func(_ Stage, status Status) { statuses = append(statuses, status) })
	if !result.Developer || !errors.Is(result.Err, activationFailure) || errors.Is(result.Err, ErrSkipped) || result.Completed[Tools] || result.Completed[VSCode] || !result.Completed[Feeds] {
		t.Fatalf("result = %+v", result)
	}
	if !reflect.DeepEqual(statuses, []Status{Working, Failed, Working, Skipped, Working, Complete}) {
		t.Fatalf("statuses = %v", statuses)
	}
}
