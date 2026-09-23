package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

func TestHomebrewUnavailable(t *testing.T) {
	provider := newHomebrew(HomebrewDeps{
		Installed: func() bool { return false },
	})

	if provider.ID() != updateflow.DeveloperTools {
		t.Fatalf("provider ID = %q, want %q", provider.ID(), updateflow.DeveloperTools)
	}
	if provider.Available() {
		t.Fatal("provider is available when Homebrew is not installed")
	}
}

func TestHomebrewCheckMapsOutdatedPackages(t *testing.T) {
	provider := newHomebrew(HomebrewDeps{
		Installed: func() bool { return true },
		ListOutdated: func() ([]homebrew.Package, error) {
			return []homebrew.Package{
				{Name: "ripgrep", Version: "14.1.0"},
				{Name: "jq", Version: "1.7.1"},
			}, nil
		},
	})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	want := updateflow.CheckResult{Items: []updateflow.Item{
		{Name: "ripgrep", CurrentVersion: "14.1.0"},
		{Name: "jq", CurrentVersion: "1.7.1"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Check() = %#v, want %#v", got, want)
	}
}

func TestHomebrewCheckReturnsEmptyOutdatedList(t *testing.T) {
	provider := newHomebrew(HomebrewDeps{
		Installed:    func() bool { return true },
		ListOutdated: func() ([]homebrew.Package, error) { return nil, nil },
	})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("Check() items = %#v, want empty", got.Items)
	}
}

func TestHomebrewApplyUpdatesMetadataThenAllPackagesOnce(t *testing.T) {
	var calls []string
	provider := newHomebrew(HomebrewDeps{
		Update: func(context.Context) error {
			calls = append(calls, "update")
			return nil
		},
		Upgrade: func(ctx context.Context, name string) error {
			calls = append(calls, "upgrade:"+name)
			return nil
		},
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{
		{Name: "ripgrep"},
		{Name: "jq"},
	}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"update", "upgrade:"}) {
		t.Fatalf("Homebrew calls = %v, want metadata update then upgrade-all", calls)
	}
	if !result.Changed || result.Preview {
		t.Fatalf("Apply() result = %#v, want live changed result", result)
	}
}

func TestHomebrewApplyMetadataFailureStopsBeforeUpgrade(t *testing.T) {
	wantErr := errors.New("metadata update failed")
	upgradeCalls := 0
	provider := newHomebrew(HomebrewDeps{
		Update: func(context.Context) error { return wantErr },
		Upgrade: func(context.Context, string) error {
			upgradeCalls++
			return nil
		},
	})

	_, err := provider.Apply(context.Background(), []updateflow.Item{{Name: "ripgrep"}}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Apply() error = %v, want %v", err, wantErr)
	}
	if upgradeCalls != 0 {
		t.Fatalf("Upgrade calls = %d, want 0 after metadata failure", upgradeCalls)
	}
}

func TestHomebrewApplyUpgradeFailure(t *testing.T) {
	wantErr := errors.New("upgrade failed")
	var calls []string
	provider := newHomebrew(HomebrewDeps{
		Update: func(context.Context) error {
			calls = append(calls, "update")
			return nil
		},
		Upgrade: func(ctx context.Context, name string) error {
			calls = append(calls, "upgrade:"+name)
			return wantErr
		},
	})

	_, err := provider.Apply(context.Background(), []updateflow.Item{{Name: "ripgrep"}}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Apply() error = %v, want %v", err, wantErr)
	}
	if !reflect.DeepEqual(calls, []string{"update", "upgrade:"}) {
		t.Fatalf("Homebrew calls = %v, want metadata update then upgrade-all", calls)
	}
}

func TestHomebrewApplyDryRunReportsPreviewWithoutCompletedMutation(t *testing.T) {
	var calls []string
	provider := newHomebrew(HomebrewDeps{
		Update: func(context.Context) error {
			calls = append(calls, "update")
			return nil
		},
		Upgrade: func(ctx context.Context, name string) error {
			calls = append(calls, "upgrade:"+name)
			return nil
		},
		DryRun: func() bool { return true },
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{{Name: "ripgrep"}}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"update", "upgrade:"}) {
		t.Fatalf("Homebrew calls = %v, want metadata update then upgrade-all", calls)
	}
	if result.Changed {
		t.Fatal("dry-run Apply() reports a completed mutation")
	}
	if !result.Preview {
		t.Fatal("dry-run Apply() does not report preview")
	}
}
