package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// brokenRemoteStub is a flatpak stand-in for issue #471: the user
// installation has flathub and a leftover test-center remote whose host no
// longer resolves, the system installation has no remotes, and appOrigins is
// the origin remotes of the installed applications. A runtime from
// test-center is installed as well — the usual leftover of an application
// uninstalled without `--unused` — so an origin listing without `--app` also
// names test-center. flathub reports a Firefox update until `update` runs.
func brokenRemoteStub(appOrigins string) string {
	return `state="${0%/*}/updated"
case "$1" in
remotes) [ "$2" = --user ] && printf 'flathub\ntest-center\tno-gpg-verify\n' ;;
list)
	case "$*" in
	*--app*) printf '` + appOrigins + `' ;;
	*) printf '` + appOrigins + `test-center\n' ;;
	esac ;;
update) : > "$state" ;;
remote-ls)
	case "$6" in
	flathub) [ -e "$state" ] || printf 'Firefox\torg.mozilla.firefox\t131.0\n' ;;
	*) echo "error: Unable to load summary from remote $6: Could not resolve hostname" >&2; exit 1 ;;
	esac ;;
*) exit 1 ;;
esac
exit 0
`
}

// Issue #471: a leftover remote no installed application comes from — even
// one still serving a runtime — must not fail the Applications check or the
// post-apply reconciliation, and the healthy remote's update must still be
// offered and verified.
func TestFlatpakIgnoresABrokenUnusedRemoteEndToEnd(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	writeStub(t, "flatpak", brokenRemoteStub(`flathub\n`))
	provider := NewFlatpak()

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v, want the unused broken remote ignored", err)
	}
	want := []updateflow.Item{{ID: "org.mozilla.firefox", Name: "Firefox", AvailableVersion: "131.0", Scope: "user"}}
	if !reflect.DeepEqual(got.Items, want) {
		t.Fatalf("Check() items = %#v, want %#v", got.Items, want)
	}

	result, err := provider.Apply(context.Background(), got.Items, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v, want reconciliation to ignore the unused broken remote", err)
	}
	if !result.Changed || result.Preview {
		t.Fatalf("Apply() = %#v, want a verified change", result)
	}
}

// A broken remote an installed application still comes from fails the check
// with an error naming that remote, which the coordinator shows while keeping
// the source's last known inventory.
func TestFlatpakReportsABrokenRemoteInUse(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	writeStub(t, "flatpak", brokenRemoteStub(`flathub\ntest-center\n`))

	_, err := NewFlatpak().Check(context.Background())
	var remoteErr *flatpak.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Remote != "test-center" || remoteErr.Installation != "user" {
		t.Fatalf("Check() error = %v, want test-center reported in the user installation", err)
	}
}

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
		ListUpdates: func(ctx context.Context, user bool) ([]flatpak.UpdateInfo, error) {
			calls = append(calls, user)
			if user {
				return []flatpak.UpdateInfo{{
					Name:          "Firefox",
					ApplicationID: "org.mozilla.firefox",
					NewVersion:    "121",
					Installation:  "user",
				}}, nil
			}
			return []flatpak.UpdateInfo{{
				Name:          "Runtime",
				ApplicationID: "org.example.Runtime",
				NewVersion:    "42",
				Installation:  "system",
			}}, nil
		},
	})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	want := updateflow.CheckResult{Items: []updateflow.Item{
		{ID: "org.mozilla.firefox", Name: "Firefox", AvailableVersion: "121", Scope: "user"},
		{ID: "org.example.Runtime", Name: "Runtime", AvailableVersion: "42", Scope: "system"},
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
				ListUpdates: func(ctx context.Context, user bool) ([]flatpak.UpdateInfo, error) {
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
			applied := false
			provider := newFlatpak(FlatpakDeps{
				Installed: func() bool { return true },
				ListUpdates: func(ctx context.Context, user bool) ([]flatpak.UpdateInfo, error) {
					if applied {
						return nil, nil
					}
					if user {
						return test.userItems, nil
					}
					return test.systemItems, nil
				},
				Update: func(_ context.Context, _ string, user bool) error {
					calls = append(calls, user)
					applied = true
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
	applied := false
	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		ListUpdates: func(ctx context.Context, user bool) ([]flatpak.UpdateInfo, error) {
			if user {
				return nil, userErr
			}
			if applied {
				return nil, nil
			}
			return []flatpak.UpdateInfo{{Name: "System App", Installation: "system"}}, nil
		},
		Update: func(_ context.Context, _ string, user bool) error {
			calls = append(calls, user)
			applied = true
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

func TestFlatpakApplyReportsUnchangedWhenUpdatesRemain(t *testing.T) {
	// `flatpak update` exits 0 and prints "Nothing to update." on a path
	// that applies nothing. The re-list after the command still shows the
	// pending entry, so the provider must not claim the inventory landed.
	var updateCalls int
	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		ListUpdates: func(context.Context, bool) ([]flatpak.UpdateInfo, error) {
			return []flatpak.UpdateInfo{{Name: "Compass", Installation: "user"}}, nil
		},
		Update: func(context.Context, string, bool) error {
			updateCalls++
			return nil
		},
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{{
		Name:  "Compass",
		Scope: "user",
	}}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("Update calls = %d, want 1", updateCalls)
	}
	if result.Changed || result.Preview {
		t.Fatalf("Apply() result = %#v, want unchanged live result", result)
	}
}

func TestFlatpakApplyReportsUnchangedWhenEitherScopeRemains(t *testing.T) {
	// One scope applied and the other did not: the source as a whole is not
	// complete, so the coordinator must keep its entries pending.
	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		ListUpdates: func(_ context.Context, user bool) ([]flatpak.UpdateInfo, error) {
			if user {
				return nil, nil
			}
			return []flatpak.UpdateInfo{{Name: "System App", Installation: "system"}}, nil
		},
		Update: func(context.Context, string, bool) error { return nil },
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{
		{Name: "User App", Scope: "user"},
		{Name: "System App", Scope: "system"},
	}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Changed {
		t.Fatalf("Apply() result = %#v, want unchanged when a scope remains", result)
	}
}

func TestFlatpakApplyPropagatesReconcileFailure(t *testing.T) {
	listErr := errors.New("remote-ls failed")
	provider := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		ListUpdates: func(context.Context, bool) ([]flatpak.UpdateInfo, error) {
			return nil, listErr
		},
		Update: func(context.Context, string, bool) error { return nil },
	})

	_, err := provider.Apply(context.Background(), []updateflow.Item{
		{Name: "User App", Scope: "user"},
	}, nil)
	if !errors.Is(err, listErr) {
		t.Fatalf("Apply() error = %v, want it to wrap %v", err, listErr)
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
		ListUpdates: func(ctx context.Context, user bool) ([]flatpak.UpdateInfo, error) {
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

func TestFlatpakApplyReconciliation(t *testing.T) {
	listErr := errors.New("remote-ls failed")

	tests := []struct {
		name               string
		cancelDuringUpdate bool
		items              []updateflow.Item
		postUpdates        []flatpak.UpdateInfo
		listErr            error
		wantChanged        bool
		wantErr            error
	}{
		{
			name: "all applied cleared",
			items: []updateflow.Item{
				{ID: "org.mozilla.firefox", Name: "Firefox", Scope: "user"},
			},
			postUpdates: nil,
			wantChanged: true,
		},
		{
			name: "new update appeared after check but applied ones cleared",
			items: []updateflow.Item{
				{ID: "org.mozilla.firefox", Name: "Firefox", Scope: "user"},
			},
			postUpdates: []flatpak.UpdateInfo{
				{ApplicationID: "org.videolan.VLC", Name: "VLC", Installation: "user"},
			},
			wantChanged: true,
		},
		{
			name: "applied ref still listed",
			items: []updateflow.Item{
				{ID: "org.mozilla.firefox", Name: "Firefox", Scope: "user"},
			},
			postUpdates: []flatpak.UpdateInfo{
				{ApplicationID: "org.mozilla.firefox", Name: "Firefox", Installation: "user"},
			},
			wantChanged: false,
		},
		{
			name: "listing error",
			items: []updateflow.Item{
				{ID: "org.mozilla.firefox", Name: "Firefox", Scope: "user"},
			},
			listErr:     listErr,
			wantChanged: false,
			wantErr:     listErr,
		},
		{
			name:               "ctx cancelled",
			cancelDuringUpdate: true,
			items: []updateflow.Item{
				{ID: "org.mozilla.firefox", Name: "Firefox", Scope: "user"},
			},
			wantChanged: false,
			wantErr:     context.Canceled,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			provider := newFlatpak(FlatpakDeps{
				Installed: func() bool { return true },
				Update: func(ctx context.Context, _ string, _ bool) error {
					if test.cancelDuringUpdate {
						cancel()
					}
					return nil
				},
				ListUpdates: func(ctx context.Context, _ bool) ([]flatpak.UpdateInfo, error) {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if test.listErr != nil {
						return nil, test.listErr
					}
					return test.postUpdates, nil
				},
			})

			result, err := provider.Apply(ctx, test.items, nil)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Apply() error = %v, want error wrapping %v", err, test.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Apply() unexpected error: %v", err)
			}
			if result.Changed != test.wantChanged {
				t.Fatalf("Apply() Changed = %v, want %v", result.Changed, test.wantChanged)
			}
		})
	}
}
