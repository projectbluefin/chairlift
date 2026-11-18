package updateproviders

import (
	"context"
	"fmt"
	"testing"

	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/userprefs"
)

func TestRuntimeOnlyCheckAdmitsUpdateAllInObservedScope(t *testing.T) {
	for _, user := range []bool{true, false} {
		t.Run(fmt.Sprint(user), func(t *testing.T) {
			scope := "system"
			if user {
				scope = "user"
			}
			applied := false
			p := newFlatpak(FlatpakDeps{
				Installed: func() bool { return true },
				ListUpdates: func(_ context.Context, isUser bool) ([]flatpak.UpdateInfo, error) {
					if isUser != user || applied {
						return nil, nil
					}
					return []flatpak.UpdateInfo{{Name: "Platform", ApplicationID: "org.freedesktop.Platform", Arch: "x86_64", Branch: "26.08", Installation: scope}}, nil
				},
				Update: func(_ context.Context, id string, isUser bool) error {
					if id != "" || isUser != user {
						t.Fatalf("updated wrong scope: id=%q user=%v", id, isUser)
					}
					applied = true
					return nil
				},
			})
			c := updateflow.New([]updateflow.Provider{p}, nil)
			prefs := userprefs.Values{Applications: true}
			checked := c.Check(context.Background(), prefs, map[updateflow.SourceID]updateflow.Policy{updateflow.Applications: {Configured: true, Supported: true}}, nil)
			if checked.TotalUpdates != 1 || checked.Action != updateflow.ActionUpdateAll {
				t.Fatalf("runtime check = %+v", checked)
			}
			done := c.UpdateAll(context.Background(), checked, prefs, nil)
			if !applied || done.TotalUpdates != 0 || !done.Sources[0].Completed {
				t.Fatalf("runtime apply = %+v", done)
			}
		})
	}
}

func TestRuntimeBranchesHaveDistinctUpdateIdentities(t *testing.T) {
	updates := []flatpak.UpdateInfo{
		{Name: "Platform", ApplicationID: "org.freedesktop.Platform", Arch: "x86_64", Branch: "25.08", Installation: "user"},
		{Name: "Platform", ApplicationID: "org.freedesktop.Platform", Arch: "x86_64", Branch: "26.08", Installation: "user"},
	}
	items := flatpakItems(updates)
	if items[0].ID == items[1].ID {
		t.Fatal("runtime branches share an execution identity")
	}
	refs := appliedRefsForScope(items[:1], "user")
	if !updateMatchesApplied(updates[0], refs) || updateMatchesApplied(updates[1], refs) {
		t.Fatal("reconciliation conflates runtime branches")
	}
}

func TestRuntimeRowUpdateTargetsAndVerifiesItsBranch(t *testing.T) {
	for _, branch := range []string{"25.08", "26.08"} {
		t.Run(branch, func(t *testing.T) {
			writeStub(t, "flatpak", `case "$1" in
update) [ "$3" = --user ] && [ "$4" = org.freedesktop.Platform/x86_64/25.08 ] || exit 1 ;;
remotes) printf 'flathub\n' ;;
remote-ls) printf 'Platform\torg.freedesktop.Platform\t\tnew\tx86_64\t`+branch+`\n' ;;
list) printf 'org.freedesktop.Platform\tx86_64\t`+branch+`\told\t\n' ;;
esac`)
			result, err := UpdateItem(context.Background(), updateflow.Applications, updateflow.Item{ID: "org.freedesktop.Platform/x86_64/25.08", Name: "Platform", Scope: "user"})
			if err != nil || result.Changed != (branch != "25.08") {
				t.Fatalf("branch %s verification = %+v, %v", branch, result, err)
			}
		})
	}
}
