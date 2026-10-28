package homebrew

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// stubFlatpakDescriptions replaces the Flatpak lookup and records the IDs it
// was asked for, so tests never reach the host's flatpak.
func stubFlatpakDescriptions(t *testing.T, descriptions map[string]string, err error) *[][]string {
	t.Helper()
	var calls [][]string
	previous := describeFlatpakApps
	describeFlatpakApps = func(_ context.Context, ids []string) (map[string]string, error) {
		calls = append(calls, append([]string(nil), ids...))
		return descriptions, err
	}
	t.Cleanup(func() { describeFlatpakApps = previous })
	return &calls
}

func TestBundleDescriptions(t *testing.T) {
	flatpakCalls := stubFlatpakDescriptions(t, map[string]string{"org.gnome.Calculator": "Perform arithmetic"}, nil)
	log := fakeBrewOnPath(t, `case "$*" in
 *--formula*) echo '{"formulae":[{"name":"jq","full_name":"vendor/tools/jq","desc":"JSON processor"},{"name":"just","aliases":["just-alias"],"desc":"Save and run commands"}]}' ;;
 *--cask*) echo '{"casks":[{"name":["Editor"],"token":"editor","full_token":"vendor/apps/editor","desc":"Text editor"}]}' ;;
 esac`)
	items := []BundleItem{
		{Name: "vendor/tools/jq", Kind: BundleItemBrew},
		{Name: "just-alias", Kind: BundleItemBrew},
		{Name: "vendor/tools/jq", Kind: BundleItemBrew},
		{Name: "vendor/apps/editor", Kind: BundleItemCask},
		{Name: "org.gnome.Calculator", Kind: BundleItemFlatpak},
		{Name: "--eval=bad", Kind: BundleItemBrew},
		{Name: "123456", Kind: BundleItemMas},
	}
	got, err := DescribeBundleItems(context.Background(), items)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"JSON processor", "Save and run commands", "JSON processor", "Text editor", "Perform arithmetic", "", ""} {
		if got[i].Description != want {
			t.Errorf("item %d: %q, want %q", i, got[i].Description, want)
		}
		if items[i].Description != "" {
			t.Fatal("modified caller slice")
		}
	}
	assertArgv(t, log, []string{"info --json=v2 --formula vendor/tools/jq just-alias", "info --json=v2 --cask vendor/apps/editor"})
	if want := [][]string{{"org.gnome.Calculator"}}; !reflect.DeepEqual(*flatpakCalls, want) {
		t.Fatalf("flatpak lookups = %q, want %q", *flatpakCalls, want)
	}
}

func TestBundleDescriptionsFlatpakFailureKeepsOtherDescriptions(t *testing.T) {
	stubFlatpakDescriptions(t, map[string]string{"org.gnome.Maps": "Find places"}, errors.New("flatpak search failed"))
	fakeBrewOnPath(t, `echo '{"formulae":[{"name":"jq","desc":"JSON processor"}]}'`)
	got, err := DescribeBundleItems(context.Background(), []BundleItem{
		{Name: "jq", Kind: BundleItemBrew},
		{Name: "org.gnome.Maps", Kind: BundleItemFlatpak},
		{Name: "org.gnome.Broken", Kind: BundleItemFlatpak},
	})
	if err == nil || !strings.Contains(err.Error(), "flatpak search failed") {
		t.Fatalf("err = %v, want the Flatpak failure reported", err)
	}
	for i, want := range []string{"JSON processor", "Find places", ""} {
		if got[i].Description != want {
			t.Errorf("item %d: %q, want %q", i, got[i].Description, want)
		}
	}
}

func TestBundleDescriptionsSkipsFlatpakLookupWithoutFlatpakEntries(t *testing.T) {
	calls := stubFlatpakDescriptions(t, nil, nil)
	fakeBrewOnPath(t, `echo '{"formulae":[]}'`)
	if _, err := DescribeBundleItems(context.Background(), []BundleItem{{Name: "jq", Kind: BundleItemBrew}}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("flatpak lookups = %q, want none", *calls)
	}
}

func TestBundleDescriptionsFailure(t *testing.T) {
	for _, body := range []string{"exit 1", "echo invalid-json"} {
		t.Run(body, func(t *testing.T) {
			fakeBrewOnPath(t, `case "$*" in
 *--formula*) `+body+` ;;
 *--cask*) echo '{"casks":[{"token":"editor","desc":"Text editor"}]}' ;;
 esac`)
			got, err := DescribeBundleItems(context.Background(), []BundleItem{{Name: "jq", Kind: BundleItemBrew}, {Name: "editor", Kind: BundleItemCask}})
			if err == nil || got[0].Description != "" || got[1].Description != "Text editor" {
				t.Fatalf("got %#v, %v", got, err)
			}
		})
	}
}

func TestBundleDescriptionsRetriesNamesAfterBatchFailure(t *testing.T) {
	log := fakeBrewOnPath(t, `case "$*" in
 'info --json=v2 --formula jq') echo '{"formulae":[{"name":"jq","desc":"JSON processor"}]}' ;;
 'info --json=v2 --formula just') echo '{"formulae":[{"name":"just","desc":"Save and run commands"}]}' ;;
 *) echo 'Error: No available formula' >&2; exit 1 ;;
 esac`)
	got, err := DescribeBundleItems(context.Background(), []BundleItem{
		{Name: "jq", Kind: BundleItemBrew},
		{Name: "vendor/untapped/tool", Kind: BundleItemBrew},
		{Name: "just", Kind: BundleItemBrew},
	})
	if err == nil {
		t.Fatal("want error for the unresolvable name")
	}
	for i, want := range []string{"JSON processor", "", "Save and run commands"} {
		if got[i].Description != want {
			t.Errorf("item %d: %q, want %q", i, got[i].Description, want)
		}
	}
	assertArgv(t, log, []string{
		"info --json=v2 --formula jq vendor/untapped/tool just",
		"info --json=v2 --formula jq",
		"info --json=v2 --formula vendor/untapped/tool",
		"info --json=v2 --formula just",
	})
}

func TestBundleDescriptionsSingleNameFailureIsNotRetried(t *testing.T) {
	log := fakeBrewOnPath(t, "exit 1")
	if _, err := DescribeBundleItems(context.Background(), []BundleItem{{Name: "jq", Kind: BundleItemBrew}}); err == nil {
		t.Fatal("want error")
	}
	assertArgv(t, log, []string{"info --json=v2 --formula jq"})
}

func TestBundleDescriptionTokens(t *testing.T) {
	for _, name := range []string{"", "--help", "/tmp/formula", "../formula", "https://bad/formula", "bad.rb", "x\ny", "$(bad)"} {
		if validDescriptionToken(name) {
			t.Errorf("accepted %q", name)
		}
	}
	for _, name := range []string{"jq", "vendor/tap/tool", "python@3.14", "c++", "font-fira-code"} {
		if !validDescriptionToken(name) {
			t.Errorf("rejected %q", name)
		}
	}
}

func TestBundleDescriptionsCancellation(t *testing.T) {
	fakeBrewOnPath(t, "sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := DescribeBundleItems(ctx, []BundleItem{{Name: "jq", Kind: BundleItemBrew}})
	if err == nil || len(got) != 1 {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestBundleDescriptionsNoRequests(t *testing.T) {
	log := fakeBrewOnPath(t, "exit 9")
	_, err := DescribeBundleItems(context.Background(), []BundleItem{{Name: "extension", Kind: BundleItemVSCode}})
	if err != nil {
		t.Fatal(err)
	}
	if got := recordedArgv(t, log); len(got) != 0 {
		t.Fatal(strings.Join(got, "\n"))
	}
}
