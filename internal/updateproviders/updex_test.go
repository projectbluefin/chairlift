package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/updex"
)

func TestSystemComponentsUnavailable(t *testing.T) {
	provider := newSystemComponents(UpdexDeps{
		Installed: func() bool { return false },
	})

	if provider.ID() != updateflow.SystemComponents {
		t.Fatalf("provider ID = %q, want %q", provider.ID(), updateflow.SystemComponents)
	}
	if provider.Available() {
		t.Fatal("provider is available when updex is not installed")
	}
}

func TestSystemComponentsCheckCountsEachAffectedFeatureOnce(t *testing.T) {
	provider := newSystemComponents(UpdexDeps{
		Installed: func() bool { return true },
		Check: func(context.Context) ([]updex.FeatureCheck, []string, error) {
			return []updex.FeatureCheck{
				{
					Feature: "graphics",
					Results: []updex.CheckResult{
						{Component: "mesa", UpdateAvailable: true},
						{Component: "vulkan", UpdateAvailable: true},
					},
				},
				{
					Feature: "audio",
					Results: []updex.CheckResult{
						{Component: "pipewire", UpdateAvailable: false},
					},
				},
				{
					Feature: "fonts",
					Results: []updex.CheckResult{
						{Component: "noto", UpdateAvailable: true},
					},
				},
			}, nil, nil
		},
	})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	want := updateflow.CheckResult{Items: []updateflow.Item{
		{Name: "graphics"},
		{Name: "fonts"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Check() = %#v, want %#v", got, want)
	}
}

func TestSystemComponentsCheckPropagatesError(t *testing.T) {
	wantErr := errors.New("feature check failed")
	provider := newSystemComponents(UpdexDeps{
		Installed: func() bool { return true },
		Check: func(context.Context) ([]updex.FeatureCheck, []string, error) {
			return nil, nil, wantErr
		},
	})

	_, err := provider.Check(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Check() error = %v, want %v", err, wantErr)
	}
}

func TestSystemComponentsApplyUsesUpdateOnlyAndReportsPreview(t *testing.T) {
	var calls int
	provider := newSystemComponents(UpdexDeps{
		Update: func(context.Context) error {
			calls++
			return nil
		},
		DryRun: func() bool { return true },
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{{Name: "graphics"}}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("Update calls = %d, want 1", calls)
	}
	if result != (updateflow.ApplyResult{Preview: true}) {
		t.Fatalf("Apply() result = %#v, want preview only", result)
	}
}
