package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

func TestFlatpakUnavailable(t *testing.T) {
	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return false },
	})

	if provider.ID() != updateflow.Applications {
		t.Fatalf("provider ID = %q, want %q", provider.ID(), updateflow.Applications)
	}
	if provider.Available() {
		t.Fatal("provider is available when Flatpak is not installed")
	}
}

func TestFlatpakCheckMapsUserAndSystemUpdatesInScopeOrder(t *testing.T) {
	var calls []bool
	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		ListUpdates: func(user bool) ([]flatpak.UpdateInfo, error) {
			calls = append(calls, user)
			if user {
				return []flatpak.UpdateInfo{{
					Name:         "Firefox",
					NewVersion:   "121",
					Installation: "user",
				}}, nil
			}
			return []flatpak.UpdateInfo{{
				Name:         "Runtime",
				NewVersion:   "42",
				Installation: "system",
			}}, nil
		},
	})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	want := updateflow.CheckResult{Items: []updateflow.Item{
		{Name: "Firefox", AvailableVersion: "121", Scope: "user"},
		{Name: "Runtime", AvailableVersion: "42", Scope: "system"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Check() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(calls, []bool{true, false}) {
		t.Fatalf("ListUpdates calls = %v, want user then system", calls)
	}
}

func TestFlatpakCheckPreservesSuccessfulScopeWhenOtherScopeFails(t *testing.T) {
	userErr := errors.New("user query failed")
	systemErr := errors.New("system query failed")
	tests := []struct {
		name      string
		user      []flatpak.UpdateInfo
		userError error
		system    []flatpak.UpdateInfo
		systemErr error
		want      []updateflow.Item
		wantErrs  []error
	}{
		{
			name:      "user fails",
			system:    []flatpak.UpdateInfo{{Name: "System App", NewVersion: "2", Installation: "system"}},
			userError: userErr,
			want:      []updateflow.Item{{Name: "System App", AvailableVersion: "2", Scope: "system"}},
			wantErrs:  []error{userErr},
		},
		{
			name:      "system fails",
			user:      []flatpak.UpdateInfo{{Name: "User App", NewVersion: "3", Installation: "user"}},
			systemErr: systemErr,
			want:      []updateflow.Item{{Name: "User App", AvailableVersion: "3", Scope: "user"}},
			wantErrs:  []error{systemErr},
		},
		{
			name:      "both fail",
			userError: userErr,
			systemErr: systemErr,
			wantErrs:  []error{userErr, systemErr},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newFlatpak(FlatpakDeps{
				Installed: func() bool { return true },
				ListUpdates: func(user bool) ([]flatpak.UpdateInfo, error) {
					if user {
						return test.user, test.userError
					}
					return test.system, test.systemErr
				},
			})

			got, err := provider.Check(context.Background())
			if !reflect.DeepEqual(got.Items, test.want) {
				t.Fatalf("Check() items = %#v, want %#v", got.Items, test.want)
			}
			for _, wantErr := range test.wantErrs {
				if !errors.Is(err, wantErr) {
					t.Errorf("Check() error = %v, want it to contain %v", err, wantErr)
				}
			}
			if len(test.wantErrs) == 0 && err != nil {
				t.Fatalf("Check() error = %v, want nil", err)
			}
		})
	}
}

func TestFlatpakApplyUpdatesOnlyScopesInCheckedSnapshot(t *testing.T) {
	tests := []struct {
		name        string
		userItems   []flatpak.UpdateInfo
		systemItems []flatpak.UpdateInfo
		wantCalls   []bool
	}{
		{
			name: "user only",
			userItems: []flatpak.UpdateInfo{{
				Name: "User App", Installation: "user",
			}},
			wantCalls: []bool{true},
		},
		{
			name: "system only",
			systemItems: []flatpak.UpdateInfo{{
				Name: "System App", Installation: "system",
			}},
			wantCalls: []bool{false},
		},
		{
			name: "both scopes",
			userItems: []flatpak.UpdateInfo{{
				Name: "User App", Installation: "user",
			}},
			systemItems: []flatpak.UpdateInfo{{
				Name: "System App", Installation: "system",
			}},
			wantCalls: []bool{true, false},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls []bool
			provider := newFlatpak(FlatpakDeps{
				Installed: func() bool { return true },
				ListUpdates: func(user bool) ([]flatpak.UpdateInfo, error) {
					if user {
						return test.userItems, nil
					}
					return test.systemItems, nil
				},
				Update: func(_ context.Context, _ string, user bool) error {
					calls = append(calls, user)
					return nil
				},
			})

			checked, err := provider.Check(context.Background())
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			result, err := provider.Apply(context.Background(), checked.Items, nil)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if !reflect.DeepEqual(calls, test.wantCalls) {
				t.Fatalf("Update calls = %v, want %v", calls, test.wantCalls)
			}
			if !result.Changed || result.Preview {
				t.Fatalf("Apply() result = %#v, want live changed result", result)
			}
		})
	}
}

func TestFlatpakApplyUsesSuccessfulScopeAfterPartialCheck(t *testing.T) {
	userErr := errors.New("user query failed")
	var calls []bool
	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		ListUpdates: func(user bool) ([]flatpak.UpdateInfo, error) {
			if user {
				return nil, userErr
			}
			return []flatpak.UpdateInfo{{Name: "System App", Installation: "system"}}, nil
		},
		Update: func(_ context.Context, _ string, user bool) error {
			calls = append(calls, user)
			return nil
		},
	})

	checked, err := provider.Check(context.Background())
	if !errors.Is(err, userErr) {
		t.Fatalf("Check() error = %v, want %v", err, userErr)
	}
	if _, err := provider.Apply(context.Background(), checked.Items, nil); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !reflect.DeepEqual(calls, []bool{false}) {
		t.Fatalf("Update calls = %v, want system scope only", calls)
	}
}

func TestFlatpakApplyUsesSystemScopeFromNewerCheckAfterOlderCheckCompletes(t *testing.T) {
	var userCalls atomic.Int32
	var systemCalls atomic.Int32
	olderSystemStarted := make(chan struct{})
	releaseOlderSystem := make(chan struct{})
	var updateCalls []bool

	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		ListUpdates: func(user bool) ([]flatpak.UpdateInfo, error) {
			if user {
				if userCalls.Add(1) == 1 {
					return []flatpak.UpdateInfo{{Name: "Older User App", Installation: "user"}}, nil
				}
				return nil, nil
			}
			if systemCalls.Add(1) == 1 {
				close(olderSystemStarted)
				<-releaseOlderSystem
				return nil, nil
			}
			return []flatpak.UpdateInfo{{Name: "Newer System App", Installation: "system"}}, nil
		},
		Update: func(_ context.Context, _ string, user bool) error {
			updateCalls = append(updateCalls, user)
			return nil
		},
	})

	olderResult := make(chan updateflow.CheckResult, 1)
	olderErr := make(chan error, 1)
	go func() {
		result, err := provider.Check(context.Background())
		olderResult <- result
		olderErr <- err
	}()
	<-olderSystemStarted

	newerResult := make(chan updateflow.CheckResult, 1)
	newerErr := make(chan error, 1)
	go func() {
		result, err := provider.Check(context.Background())
		newerResult <- result
		newerErr <- err
	}()

	newer := <-newerResult
	if err := <-newerErr; err != nil {
		t.Fatalf("newer Check() error = %v", err)
	}
	close(releaseOlderSystem)
	older := <-olderResult
	if err := <-olderErr; err != nil {
		t.Fatalf("older Check() error = %v", err)
	}

	if !reflect.DeepEqual(older.Items, []updateflow.Item{{
		Name:  "Older User App",
		Scope: "user",
	}}) {
		t.Fatalf("older Check() items = %#v, want older user item", older.Items)
	}
	if !reflect.DeepEqual(newer.Items, []updateflow.Item{{
		Name:  "Newer System App",
		Scope: "system",
	}}) {
		t.Fatalf("newer Check() items = %#v, want newer system item", newer.Items)
	}

	if _, err := provider.Apply(context.Background(), newer.Items, nil); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !reflect.DeepEqual(updateCalls, []bool{false}) {
		t.Fatalf("Update calls = %v, want newer system scope", updateCalls)
	}
}

func TestFlatpakApplyDryRunReportsPreviewWithoutCompletedMutation(t *testing.T) {
	var calls []bool
	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		Update: func(_ context.Context, _ string, user bool) error {
			calls = append(calls, user)
			return nil
		},
		DryRun: func() bool { return true },
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{{
		Name:  "User App",
		Scope: "user",
	}}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !reflect.DeepEqual(calls, []bool{true}) {
		t.Fatalf("Update calls = %v, want user scope", calls)
	}
	if result.Changed {
		t.Fatal("dry-run Apply() reports a completed mutation")
	}
	if !result.Preview {
		t.Fatal("dry-run Apply() does not report preview")
	}
}
