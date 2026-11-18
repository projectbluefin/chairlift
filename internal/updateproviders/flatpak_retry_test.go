package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/userprefs"
)

func TestFlatpakRetryDoesNotReplayVerifiedScope(t *testing.T) {
	var calls []bool
	failSystem := true
	applied := map[bool]bool{}
	p := newFlatpak(FlatpakDeps{
		Installed: func() bool { return true },
		ListUpdates: func(_ context.Context, user bool) ([]flatpak.UpdateInfo, error) {
			if applied[user] {
				return nil, nil
			}
			scope := "system"
			if user {
				scope = "user"
			}
			return []flatpak.UpdateInfo{{ApplicationID: "org.example." + scope, Installation: scope}}, nil
		},
		Update: func(_ context.Context, _ string, user bool) error {
			calls = append(calls, user)
			if !user && failSystem {
				return errors.New("system scope failed")
			}
			applied[user] = true
			return nil
		},
	})
	c := updateflow.New([]updateflow.Provider{p}, nil)
	prefs := userprefs.Values{Applications: true}
	checked := c.Check(context.Background(), prefs, map[updateflow.SourceID]updateflow.Policy{updateflow.Applications: {Configured: true, Supported: true}}, nil)
	failed := c.UpdateAll(context.Background(), checked, prefs, nil)
	if failed.Action != updateflow.ActionRetryFailed || failed.TotalUpdates != 1 || failed.Sources[0].Items[0].Scope != "system" {
		t.Fatalf("partial result = %+v", failed)
	}
	failSystem = false
	retried := c.UpdateAll(context.Background(), failed, prefs, nil)
	if !reflect.DeepEqual(calls, []bool{true, false, false}) {
		t.Fatalf("retry replayed work: %v", calls)
	}
	if retried.TotalUpdates != 0 || !retried.Sources[0].Completed {
		t.Fatalf("retry result = %+v", retried)
	}
}

func TestFlatpakPartialFailurePreservesUnverifiedWork(t *testing.T) {
	items := []updateflow.Item{{ID: "org.example.User", Scope: "user"}, {ID: "org.example.System", Scope: "system"}}
	failure := errors.New("scope failed")
	for _, tt := range []struct {
		name                                      string
		failUser, failRead, cancelSystem, preview bool
		want                                      []updateflow.Item
		calls                                     []bool
	}{
		{name: "first mutation fails", failUser: true, want: items, calls: []bool{true}},
		{name: "first verification fails", failRead: true, want: items, calls: []bool{true}},
		{name: "second mutation fails", want: items[1:], calls: []bool{true, false}},
		{name: "second mutation cancels", cancelSystem: true, want: items[1:], calls: []bool{true, false}},
		{name: "preview never clears first scope", preview: true, want: items, calls: []bool{true, false}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls []bool
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := newFlatpak(FlatpakDeps{
				DryRun: func() bool { return tt.preview },
				Update: func(_ context.Context, _ string, user bool) error {
					calls = append(calls, user)
					if tt.cancelSystem && !user {
						cancel()
						return ctx.Err()
					}
					if !user || tt.failUser {
						return failure
					}
					return nil
				},
				ListUpdates: func(context.Context, bool) ([]flatpak.UpdateInfo, error) {
					if tt.preview {
						t.Fatal("preview performed reconciliation")
					}
					if tt.failRead {
						return nil, failure
					}
					return nil, nil
				},
			})
			result, err := p.Apply(ctx, items, nil)
			if err == nil || result.Changed || !reflect.DeepEqual(result.PendingItems, tt.want) {
				t.Fatalf("partial result = %+v, %v; want %+v", result, err, tt.want)
			}
			if tt.cancelSystem && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if !reflect.DeepEqual(calls, tt.calls) {
				t.Fatalf("calls = %v, want %v", calls, tt.calls)
			}
		})
	}
}

func TestFlatpakPartialNoOpRetainsOnlyStillListedItems(t *testing.T) {
	items := []updateflow.Item{{ID: "org.example.Done", Scope: "user"}, {ID: "org.example.Pending", Scope: "user"}}
	p := newFlatpak(FlatpakDeps{
		Update: func(context.Context, string, bool) error { return nil },
		ListUpdates: func(context.Context, bool) ([]flatpak.UpdateInfo, error) {
			return []flatpak.UpdateInfo{{ApplicationID: "org.example.Pending", Installation: "user"}}, nil
		},
	})
	result, err := p.Apply(context.Background(), items, nil)
	if err == nil || result.Changed || !reflect.DeepEqual(result.PendingItems, items[1:]) {
		t.Fatalf("partial no-op = %+v, %v", result, err)
	}
}
