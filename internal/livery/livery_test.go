package livery

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// fakeCommands replaces the external-call seam with a recorder, so the
// install and revert paths are exercised without a live session.
type fakeCommands struct {
	calls []string
	// reply maps a command prefix to its stdout.
	reply map[string]string
	// fail maps a command prefix to an error.
	fail map[string]error
}

func newFakeCommands(t *testing.T) *fakeCommands {
	t.Helper()
	f := &fakeCommands{reply: map[string]string{}, fail: map[string]error{}}
	fakeBinDir := t.TempDir()
	dconfPath := filepath.Join(fakeBinDir, "dconf")
	if err := os.WriteFile(dconfPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBinDir+":"+os.Getenv("PATH"))
	original := runCommand
	runCommand = func(_ context.Context, name string, args ...string) (string, error) {
		line := strings.TrimSpace(name + " " + strings.Join(args, " "))
		f.calls = append(f.calls, line)
		// Longest prefix wins, deterministically. Map iteration order is
		// random and "dconf read" is a prefix of "dconf read -d", so a
		// shortest-match-first fake would answer the default-layer probe
		// with the current-value reply at random.
		if prefix := longestMatch(line, keysOf(f.fail)); prefix != "" {
			return f.reply[prefix], f.fail[prefix]
		}
		if prefix := longestMatch(line, keysOf(f.reply)); prefix != "" {
			return f.reply[prefix], nil
		}
		return "", nil
	}
	t.Cleanup(func() { runCommand = original })
	return f
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func longestMatch(line string, prefixes []string) string {
	best := ""
	for _, p := range prefixes {
		if strings.HasPrefix(line, p) && len(p) > len(best) {
			best = p
		}
	}
	return best
}

func (f *fakeCommands) sawPrefix(prefix string) bool {
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}
	return false
}

// useTempDataHome points the icon writes at a temporary directory.
func useTempDataHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	original := dataHome
	dataHome = func() (string, error) { return dir, nil }
	t.Cleanup(func() { dataHome = original })
	return dir
}

// TestEveryFoundationShipsASymbolicMark walks the whole catalog rather than
// spot-checking one entry: a foundation added without its color asset would
// otherwise fail only when a user selected it.
func TestEveryFoundationShipsASymbolicMark(t *testing.T) {
	foundations := Foundations()
	if len(foundations) == 0 {
		t.Fatal("Foundations() is empty: the gate would be vacuous")
	}
	for _, f := range foundations {
		t.Run(f.ID, func(t *testing.T) {
			if f.Name == "" {
				t.Errorf("foundation %q has no display name", f.ID)
			}
			data, err := Asset(f.ID)
			if err != nil {
				t.Fatalf("Asset(%q): %v", f.ID, err)
			}
			if !looksLikeSVG(data) {
				t.Errorf("Asset(%q) is not an SVG", f.ID)
			}
			symbolic := data
			if !strings.Contains(string(symbolic), "currentColor") {
				t.Errorf("symbolic %q must use fill=currentColor so GTK recolors it to the theme foreground", f.ID)
			}
		})
	}
}

// TestUnknownFoundationHasNoAsset asserts Asset rejects an id outside the
// catalog instead of reaching into the embedded filesystem with it.
func TestUnknownFoundationHasNoAsset(t *testing.T) {
	if _, err := Asset("not-a-foundation"); err == nil {
		t.Fatal("Asset accepted an id the catalog does not contain")
	}
}

// TestNextIDWalksTheCatalogAndWraps covers every entry, so a catalog whose
// last element failed to wrap could not pass.
func TestNextIDWalksTheCatalogAndWraps(t *testing.T) {
	foundations := Foundations()
	for i, f := range foundations {
		want := foundations[(i+1)%len(foundations)].ID
		if got := NextID(f.ID); got != want {
			t.Errorf("NextID(%q) = %q, want %q", f.ID, got, want)
		}
	}
}

// TestNextIDIsTotalForSelectionsOutsideTheCatalog asserts rotation can never
// leave the selection unset: a custom or stale id advances to the first
// entry rather than erroring or returning empty.
func TestNextIDIsTotalForSelectionsOutsideTheCatalog(t *testing.T) {
	first := Foundations()[0].ID
	for _, id := range []string{CustomID, "", "retired-foundation"} {
		if got := NextID(id); got != first {
			t.Errorf("NextID(%q) = %q, want %q", id, got, first)
		}
	}
}

// TestSchemaDeclaresEveryKey holds the Keys inventory and the shipped
// gschema to each other in both directions, so neither can gain a key the
// other lacks.
func TestSchemaDeclaresEveryKey(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "data", "io.projectbluefin.chairlift.livery.gschema.xml"))
	if err != nil {
		t.Fatalf("reading schema: %v", err)
	}
	schema := string(raw)

	if !strings.Contains(schema, `id="`+SchemaID+`"`) {
		t.Errorf("schema does not declare id %q", SchemaID)
	}
	for _, key := range Keys {
		if !strings.Contains(schema, `name="`+key+`"`) {
			t.Errorf("schema does not declare key %q", key)
		}
	}

	declared := strings.Count(schema, "<key name=")
	if declared != len(Keys) {
		t.Errorf("schema declares %d keys, Keys lists %d: a key exists on one side only", declared, len(Keys))
	}
}

// TestAppGridOverrideTargetsTheAdwaitaTheme is the regression test for the
// subtlest failure mode this feature has.
//
// XDG resolves the current theme and its parents before falling back to
// hicolor. view-app-grid-symbolic is a name Adwaita ships, so an override
// written under hicolor loses and the icon silently never changes, while
// every write succeeds. org.gnome.Nautilus is not an Adwaita name, which is
// why that surface does work from hicolor.
func TestAppGridOverrideTargetsTheAdwaitaTheme(t *testing.T) {
	dir := useTempDataHome(t)

	appGrid, err := IconPath(AppGrid, "")
	if err != nil {
		t.Fatalf("IconPath(AppGrid): %v", err)
	}
	want := filepath.Join(dir, "icons", "Adwaita", "symbolic", "actions", "view-app-grid-symbolic.svg")
	if appGrid != want {
		t.Errorf("app-grid override path = %q, want %q", appGrid, want)
	}

	dock, err := IconPath(Dock, "")
	if err != nil {
		t.Fatalf("IconPath(Dock): %v", err)
	}
	wantDock := filepath.Join(dir, "icons", "hicolor", "scalable", "apps", "org.gnome.Nautilus.svg")
	if dock != wantDock {
		t.Errorf("dock override path = %q, want %q", dock, wantDock)
	}
}

// TestEverySurfaceInstallsAndRemovesItsFile covers all three surfaces, so a
// surface added without a spec entry cannot pass.
func TestEverySurfaceInstallsAndRemovesItsFile(t *testing.T) {
	for name, surface := range map[string]Surface{"app-grid": AppGrid, "panel": Panel, "dock": Dock} {
		t.Run(name, func(t *testing.T) {
			useTempDataHome(t)
			newFakeCommands(t)

			if err := Apply(context.Background(), surface, Source{Kind: FromCatalog, Value: DefaultID}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			path, err := IconPath(surface, DefaultID)
			if err != nil {
				t.Fatalf("IconPath: %v", err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("Apply did not install %s: %v", path, err)
			}

			if err := Clear(context.Background(), surface); err != nil {
				t.Fatalf("Clear: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("Clear left %s behind", path)
			}
		})
	}
}

// TestApplyPanelSetsBothExtensionKeys asserts the display-mode key is written
// alongside the icon key. Writing only the icon is a silent no-op on a host
// whose menu is left showing text.
func TestApplyPanelSetsBothExtensionKeys(t *testing.T) {
	useTempDataHome(t)
	fake := newFakeCommands(t)

	if err := Apply(context.Background(), Panel, Source{Kind: FromCatalog, Value: DefaultID}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !fake.sawPrefix("gsettings set " + extensionSchema + " " + extensionIconKey + " " + PanelIconName(DefaultID)) {
		t.Error("Apply did not set the extension icon key")
	}
	if !fake.sawPrefix("gsettings set " + extensionSchema + " " + extensionModeKey) {
		t.Error("Apply did not set the extension display-mode key, so the icon would not be drawn")
	}
}

// TestApplyOnlyTouchesExtensionSettingsForThePanel asserts the other two
// surfaces shadow a name their consumer already asks for and write no dconf
// value at all.
func TestApplyOnlyTouchesExtensionSettingsForThePanel(t *testing.T) {
	for name, surface := range map[string]Surface{"app-grid": AppGrid, "dock": Dock} {
		t.Run(name, func(t *testing.T) {
			useTempDataHome(t)
			fake := newFakeCommands(t)

			if err := Apply(context.Background(), surface, Source{Kind: FromCatalog, Value: DefaultID}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if fake.sawPrefix("gsettings set " + extensionSchema) {
				t.Errorf("%s surface wrote an extension setting it does not own", name)
			}
		})
	}
}

// TestClearPanelSettingsResetsWhenTheUserHadNoValue is the regression test
// for the revert bug the dconf layering exposed.
//
// On Bluefin the panel icon comes from a distro default in
// /etc/dconf/db/distro.d, so the user layer is empty even though `gsettings
// get` answers a name. Writing that name back as a user value would pin it
// forever and override any later change to the distro default; the key must
// be reset so the lower layer answers again.
func TestClearPanelSettingsResetsWhenTheUserHadNoValue(t *testing.T) {
	fake := newFakeCommands(t)

	if err := ClearPanelSettings(context.Background(), "", ""); err != nil {
		t.Fatalf("ClearPanelSettings: %v", err)
	}
	if !fake.sawPrefix("gsettings reset " + extensionSchema + " " + extensionIconKey) {
		t.Error("an empty saved value must reset the key, not write one")
	}
	if fake.sawPrefix("gsettings set " + extensionSchema + " " + extensionIconKey) {
		t.Error("ClearPanelSettings wrote a user value where it should have reset")
	}
}

// TestClearPanelSettingsRestoresAUserValue covers the other half: a user who
// did have their own icon gets exactly it back.
func TestClearPanelSettingsRestoresAUserValue(t *testing.T) {
	fake := newFakeCommands(t)

	if err := ClearPanelSettings(context.Background(), "my-own-symbolic", "1"); err != nil {
		t.Fatalf("ClearPanelSettings: %v", err)
	}
	if !fake.sawPrefix("gsettings set " + extensionSchema + " " + extensionIconKey + " my-own-symbolic") {
		t.Errorf("did not restore the captured icon; calls: %v", fake.calls)
	}
	if fake.sawPrefix("gsettings reset") {
		t.Error("reset a key the user had their own value for")
	}
}

// TestClearPanelSettingsForgetsTheCapture pins the other half of revert: the
// recorded capture has to go once it has been put back.
//
// The capture is only taken when both saved keys are empty, so a restore that
// left them populated would make every later enable reuse the first one —
// enable, disable, set an icon by hand, enable, disable would then restore
// the pre-first-enable value and throw away the newer manual choice.
func TestClearPanelSettingsForgetsTheCapture(t *testing.T) {
	fake := newFakeCommands(t)

	if err := ClearPanelSettings(context.Background(), "my-own-symbolic", "1"); err != nil {
		t.Fatalf("ClearPanelSettings: %v", err)
	}
	for _, key := range []string{KeySavedPanelIcon, KeySavedPanelMode} {
		if !fake.sawPrefix("gsettings set " + SchemaID + " " + key + ` ""`) {
			t.Errorf("did not clear %s after restoring it; calls: %v", key, fake.calls)
		}
	}
}

// TestMissingCustomFileNamesTheFile asserts an unreadable custom SVG is
// reported rather than silently substituted, and that the message contains
// the path so the user can fix it.
func TestMissingCustomFileNamesTheFile(t *testing.T) {
	useTempDataHome(t)
	newFakeCommands(t)

	missing := filepath.Join(t.TempDir(), "gone.svg")
	err := Apply(context.Background(), Dock, Source{Kind: FromFile, Value: missing})
	if err == nil {
		t.Fatal("Apply accepted a custom file that does not exist")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the missing file", err)
	}
}

// TestNonSVGCustomFileIsRejected asserts a PNG chosen by mistake produces a
// message instead of an icon that renders blank.
func TestNonSVGCustomFileIsRejected(t *testing.T) {
	useTempDataHome(t)
	newFakeCommands(t)

	path := filepath.Join(t.TempDir(), "photo.png")
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\n not an svg"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Apply(context.Background(), Dock, Source{Kind: FromFile, Value: path})
	if err == nil || !strings.Contains(err.Error(), "not an SVG") {
		t.Fatalf("err = %v, want a not-an-SVG error", err)
	}
}

// TestSimpleIconFetchRecolorsForSymbolicUse asserts a brand-colored mark is
// converted to currentColor, which is what a symbolic surface needs.
func TestSimpleIconFetchRecolorsForSymbolicUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg fill="#6364FF" viewBox="0 0 24 24"><path d="M1 1h2v2H1z"/></svg>`))
	}))
	defer server.Close()
	useLoopbackFetch(t, server.URL)

	data, err := FetchSimpleIcon(context.Background(), "mastodon")
	if err != nil {
		t.Fatalf("FetchSimpleIcon: %v", err)
	}
	if strings.Contains(string(data), "#6364FF") {
		t.Error("brand fill survived; a symbolic surface needs currentColor")
	}
	if !strings.Contains(string(data), "currentColor") {
		t.Errorf("mark was not recolored: %s", data)
	}
}

func TestSimpleIconFetchRejectsOversizedPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		// Prefix with valid <svg so looksLikeSVG would pass if read, followed by bytes exceeding 256KB
		_, _ = w.Write([]byte(`<svg>`))
		extra := bytes.Repeat([]byte("a"), 256*1024)
		_, _ = w.Write(extra)
	}))
	defer server.Close()
	useLoopbackFetch(t, server.URL)

	_, err := FetchSimpleIcon(context.Background(), "oversized")
	if err == nil {
		t.Fatal("FetchSimpleIcon accepted an SVG exceeding the 256KB limit")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected error mentioning size limit exceeded, got: %v", err)
	}
}

// TestSimpleIconFetchReportsAnUnknownName asserts a typo produces the
// dedicated not-found error rather than a generic HTTP failure.
func TestSimpleIconFetchReportsAnUnknownName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	useLoopbackFetch(t, server.URL)

	_, err := FetchSimpleIcon(context.Background(), "nosuchbrand")
	if !errors.Is(err, ErrIconNotFound) {
		t.Fatalf("err = %v, want ErrIconNotFound", err)
	}
}

// TestSimpleIconFetchRejectsANonSVGBody asserts a captive portal or error
// page is reported rather than installed as an icon.
func TestSimpleIconFetchRejectsANonSVGBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>Sign in to continue</body></html>"))
	}))
	defer server.Close()
	useLoopbackFetch(t, server.URL)

	_, err := FetchSimpleIcon(context.Background(), "mastodon")
	if err == nil || !strings.Contains(err.Error(), "did not return an SVG") {
		t.Fatalf("err = %v, want a not-an-SVG error", err)
	}
}

// TestSlugValidationHappensBeforeAnyRequest asserts user text never reaches
// the URL path, and that a bad entry fails immediately.
func TestSlugValidationHappensBeforeAnyRequest(t *testing.T) {
	var requested bool
	original := Fetch
	Fetch = func(context.Context, string) ([]byte, error) {
		requested = true
		return nil, nil
	}
	t.Cleanup(func() { Fetch = original })

	for _, entry := range []string{"../../etc/passwd", "hello/world", "", "a b?c"} {
		if _, err := FetchSimpleIcon(context.Background(), entry); err == nil {
			t.Errorf("FetchSimpleIcon(%q) was accepted", entry)
		}
	}
	if requested {
		t.Error("an invalid name reached the network")
	}
}

// TestSlugNormalizationMatchesTheServiceConvention covers the entries a user
// is likely to type for a brand whose slug differs from its display name.
func TestSlugNormalizationMatchesTheServiceConvention(t *testing.T) {
	cases := map[string]string{
		"Home Assistant": "homeassistant",
		"  GitHub  ":     "github",
		"Red_Hat":        "red-hat",
	}
	for entry, want := range cases {
		if got := NormalizeSlug(entry); got != want {
			t.Errorf("NormalizeSlug(%q) = %q, want %q", entry, got, want)
		}
	}
}

// useLoopbackFetch points the network seam at a local test server, so no
// gated test makes an outbound request.
func useLoopbackFetch(t *testing.T, base string) {
	t.Helper()
	original := Fetch
	Fetch = func(ctx context.Context, url string) ([]byte, error) {
		return httpFetch(ctx, base+"/"+strings.TrimPrefix(url, simpleIconsCDN))
	}
	t.Cleanup(func() { Fetch = original })
}

// TestDryRunMutatesNothing is the regression test for the contract every
// provider in this repository shares: --dry-run must not touch the machine.
//
// It matters most here because `make screenshots` launches the real
// application with --dry-run, so an ungated write would reach into the
// operator's own icon theme and panel settings while capturing the
// walkthrough.
func TestDryRunMutatesNothing(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	useTempDataHome(t)
	fake := newFakeCommands(t)

	for name, surface := range map[string]Surface{"app-grid": AppGrid, "panel": Panel, "dock": Dock} {
		t.Run(name, func(t *testing.T) {
			if err := Apply(context.Background(), surface, Source{Kind: FromCatalog, Value: DefaultID}); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			path, err := IconPath(surface, DefaultID)
			if err != nil {
				t.Fatalf("IconPath: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("dry-run installed %s", path)
			}
		})
	}

	if err := SetString(context.Background(), KeyPanelID, "rust"); err != nil {
		t.Fatalf("SetString: %v", err)
	}
	if err := ClearPanelSettings(context.Background(), "", ""); err != nil {
		t.Fatalf("ClearPanelSettings: %v", err)
	}

	for _, call := range fake.calls {
		if strings.HasPrefix(call, "gsettings set") ||
			strings.HasPrefix(call, "gsettings reset") ||
			strings.HasPrefix(call, "gtk-update-icon-cache") ||
			strings.HasPrefix(call, "systemctl") {
			t.Errorf("dry-run ran a mutating command: %q", call)
		}
	}
}

// TestDryRunStillReportsAnUnreadableCustomFile asserts the checks a user
// needs still run: a preview that silently accepted a missing file would
// hide the one error worth seeing.
func TestDryRunStillReportsAnUnreadableCustomFile(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	useTempDataHome(t)
	newFakeCommands(t)

	missing := filepath.Join(t.TempDir(), "gone.svg")
	if err := Apply(context.Background(), Dock, Source{Kind: FromFile, Value: missing}); err == nil {
		t.Fatal("dry-run accepted a custom file that does not exist")
	}
}

// TestPanelIconNameVariesWithSelection is the regression test for the defect
// that would have made the panel appear frozen.
//
// GSettings emits no `changed::` when a key is written with the value it
// already holds, and the extension refreshes its indicator only on that
// signal. A single fixed icon name with swapped file contents therefore
// leaves the previous mark on screen until the shell restarts. Every
// selection must produce a distinct setting value.
func TestPanelIconNameVariesWithSelection(t *testing.T) {
	seen := map[string]string{}
	for _, f := range Foundations() {
		name := PanelIconName(f.ID)
		if !strings.HasPrefix(name, PanelIconPrefix) {
			t.Errorf("panel icon name %q lacks the ChairLift prefix that identifies our own icons", name)
		}
		if other, clash := seen[name]; clash {
			t.Errorf("foundations %q and %q share the panel icon name %q, so switching between them would emit no change signal", other, f.ID, name)
		}
		seen[name] = f.ID
	}
}

// TestSwitchingPanelSelectionsLeavesOneIcon asserts previous marks are swept,
// so trying several does not accumulate an SVG per mark ever chosen.
func TestSwitchingPanelSelectionsLeavesOneIcon(t *testing.T) {
	useTempDataHome(t)
	newFakeCommands(t)

	for _, id := range []string{"cncf", "rust", "gnome"} {
		if err := Apply(context.Background(), Panel, Source{Kind: FromCatalog, Value: id}); err != nil {
			t.Fatalf("Apply(%q): %v", id, err)
		}
	}

	dir, err := panelIconDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ours []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), PanelIconPrefix) {
			ours = append(ours, e.Name())
		}
	}
	if len(ours) != 1 {
		t.Fatalf("after three selections the theme holds %v, want exactly the last one", ours)
	}
	if ours[0] != PanelIconName("gnome")+".svg" {
		t.Errorf("installed icon = %q, want the most recent selection", ours[0])
	}
}

// TestCaptureNeverRecordsOurOwnIconName asserts a second enable cannot record
// ChairLift's own mark as "the user's previous icon", which revert would then
// restore as a file that no longer exists.
func TestCaptureNeverRecordsOurOwnIconNameOrMode(t *testing.T) {
	fake := newFakeCommands(t)
	iconPath := "/" + strings.ReplaceAll(extensionSchema, ".", "/") + "/" + extensionIconKey
	modePath := "/" + strings.ReplaceAll(extensionSchema, ".", "/") + "/" + extensionModeKey

	// Icon has ChairLift's own prefix; mode is set to "2".
	fake.reply["dconf read -d "+iconPath] = "'ublue-logo-symbolic'"
	fake.reply["dconf read "+iconPath] = "'" + PanelIconName("rust") + "'"
	fake.reply["dconf read -d "+modePath] = "'1'"
	fake.reply["dconf read "+modePath] = "'2'"

	icon, mode, ok := CapturePanelOverrides(context.Background())
	if !ok {
		t.Fatal("CapturePanelOverrides failed to read user layer")
	}
	if icon != "" {
		t.Errorf("captured icon %q, want empty: it is one of our own names", icon)
	}
	if mode != "" {
		t.Errorf("captured mode %q, want empty when icon is ChairLift's", mode)
	}
}

func TestCapturePreservesUserModeWhenIconIsNotOurs(t *testing.T) {
	fake := newFakeCommands(t)
	iconPath := "/" + strings.ReplaceAll(extensionSchema, ".", "/") + "/" + extensionIconKey
	modePath := "/" + strings.ReplaceAll(extensionSchema, ".", "/") + "/" + extensionModeKey

	// Icon has no user override (current == default); user explicitly overrode mode to "2".
	fake.reply["dconf read -d "+iconPath] = "'ublue-logo-symbolic'"
	fake.reply["dconf read "+iconPath] = "'ublue-logo-symbolic'"
	fake.reply["dconf read -d "+modePath] = "'1'"
	fake.reply["dconf read "+modePath] = "'2'"

	icon, mode, ok := CapturePanelOverrides(context.Background())
	if !ok {
		t.Fatal("CapturePanelOverrides failed to read user layer")
	}
	if icon != "" {
		t.Errorf("captured icon %q, want empty", icon)
	}
	if mode != "2" {
		t.Errorf("captured mode %q, want '2' preserved when icon has no ChairLift prefix", mode)
	}
}

// TestCaptureReportsAnUnreadableUserLayer asserts a dconf failure is not
// mistaken for "the user had no value", which would make revert reset a key
// the user had set.
func TestCaptureReportsAnUnreadableUserLayer(t *testing.T) {
	fake := newFakeCommands(t)
	fake.fail["dconf read"] = errors.New("dconf exploded")

	if _, _, ok := CapturePanelOverrides(context.Background()); ok {
		t.Error("a failed dconf read was reported as a known-empty user layer")
	}
}

// TestOnlyKnownCommandsMayRun asserts the closed command set the
// journal-contract gate's unprivileged classification depends on.
func TestOnlyKnownCommandsMayRun(t *testing.T) {
	if _, err := execCommand(context.Background(), "pkexec", "rm", "-rf", "/"); err == nil {
		t.Fatal("execCommand ran a program outside its allowed set")
	}
	for name := range allowedCommands {
		if !allowedCommands[name] {
			t.Errorf("%s should be allowed", name)
		}
	}
}

// TestUserValueIgnoresADistroDefault is the regression test for a defect a
// live experiment exposed: `dconf read` resolves the whole stack, so on a
// Bluefin host it returns the distro's panel icon even when the user has set
// nothing. Capturing that as "the user's previous value" made revert write it
// back as a user override, pinning it permanently.
func TestUserValueIgnoresADistroDefault(t *testing.T) {
	fake := newFakeCommands(t)
	// Current and default agree: the value comes from a lower layer.
	fake.reply["dconf read -d"] = "'ublue-logo-symbolic'"
	fake.reply["dconf read"] = "'ublue-logo-symbolic'"

	value, known := dconfUserValue(context.Background(), extensionSchema, extensionIconKey)
	if !known {
		t.Skip("dconf is not installed on this host")
	}
	if value != "" {
		t.Errorf("captured %q as a user value, but it is the distro default", value)
	}
}

// TestUserValueDetectsARealOverride covers the other half: a value that
// differs from the default layer is genuinely the user's and must come back
// on revert.
func TestUserValueDetectsARealOverride(t *testing.T) {
	fake := newFakeCommands(t)
	fake.reply["dconf read -d"] = "'ublue-logo-symbolic'"
	fake.reply["dconf read"] = "'my-own-symbolic'"

	value, known := dconfUserValue(context.Background(), extensionSchema, extensionIconKey)
	if !known {
		t.Skip("dconf is not installed on this host")
	}
	if value != "my-own-symbolic" {
		t.Errorf("value = %q, want the user's own icon", value)
	}
}

// TestGamingImageDefaultsToTheOpenGamingCollective asserts a gaming image
// adopts the collective's mark, and that an ordinary image does not.
func TestGamingImageDefaultsToTheOpenGamingCollective(t *testing.T) {
	if got := DefaultFoundationID(true); got != GamingDefaultID {
		t.Errorf("gaming default = %q, want %q", got, GamingDefaultID)
	}
	if got := DefaultFoundationID(false); got != DefaultID {
		t.Errorf("ordinary default = %q, want %q", got, DefaultID)
	}
	for _, id := range []string{GamingDefaultID, DefaultID} {
		if _, ok := Lookup(id); !ok {
			t.Errorf("default %q is not in the catalog", id)
		}
	}
}

// TestADeliberateSelectionSurvivesOnAGamingImage asserts the gaming default
// applies only where the user has not chosen: adopting a new default must
// never overwrite a mark someone picked.
func TestADeliberateSelectionSurvivesOnAGamingImage(t *testing.T) {
	if got := resolveID("rust", true); got != "rust" {
		t.Errorf("resolveID kept %q, want the user's own selection", got)
	}
	if got := resolveID("", true); got != GamingDefaultID {
		t.Errorf("unset selection on a gaming image = %q, want %q", got, GamingDefaultID)
	}
	if got := resolveID("retired-foundation", false); got != DefaultID {
		t.Errorf("stale selection = %q, want %q", got, DefaultID)
	}
}

// TestEverySymbolicMarkIsThemeColored asserts the contract every shipped
// foundation asset must meet: recolored by the theme, with no literal color
// left to override it.
//
// This exists because converting third-party artwork is exactly where a
// silent no-op hides. The marks arrive in three different shapes —
// fill attributes, fills inside `style="…"`, and strokes — so a transform
// written for one shape passes review while leaving another untouched, and
// the asset ships looking correct in a browser and wrong in the panel. Two
// of the three marks added in one sitting had such a leftover.
func TestEverySymbolicMarkIsThemeColored(t *testing.T) {
	literalColor := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)

	for _, f := range Foundations() {
		t.Run(f.ID, func(t *testing.T) {
			data, err := Asset(f.ID)
			if err != nil {
				t.Fatalf("Asset(%q): %v", f.ID, err)
			}
			svg := string(data)

			if !strings.Contains(svg, "currentColor") {
				t.Errorf("%q has no currentColor, so the theme cannot recolor it", f.ID)
			}
			if found := literalColor.FindAllString(svg, -1); len(found) > 0 {
				t.Errorf("%q still carries literal colors %v, which override the theme", f.ID, found)
			}
			for _, named := range []string{`fill="white"`, `fill="black"`, `fill:white`, `fill:black`} {
				if strings.Contains(svg, named) {
					t.Errorf("%q contains %s, which the theme cannot recolor", f.ID, named)
				}
			}
		})
	}
}

// TestLoadAppliesTheGamingDefaultForTheBootedImage asserts the image probe
// actually reaches the resolved selection, on any host.
//
// The seam matters: without it this would assert nothing on a non-gaming
// machine and silently pass, which is the same as having no test.
func TestLoadAppliesTheGamingDefaultForTheBootedImage(t *testing.T) {
	for name, gaming := range map[string]bool{"gaming image": true, "ordinary image": false} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeCommands(t)
			// An unset selection, which is what a fresh install has.
			fake.reply["gsettings list-recursively"] = SchemaID + " " + KeyPanelID + " ''\n" +
				SchemaID + " " + KeyDockID + " 'kubernetes'\n"

			original := detectGaming
			detectGaming = func() bool { return gaming }
			t.Cleanup(func() { detectGaming = original })

			state, err := Load(context.Background())
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			want := DefaultFoundationID(gaming)
			if state.PanelID != want {
				t.Errorf("panel selection = %q, want %q", state.PanelID, want)
			}
		})
	}
}

// TestShellIconRefreshTouchesTheApplicationsDirectory asserts the mechanism
// that actually makes a new mark appear.
//
// GNOME Shell caches icon textures by name, so installing the file is not
// enough on its own. Bumping the applications directory's timestamp fires
// GAppInfoMonitor, which makes the shell re-resolve application icons — and
// verified live, that covers the app-grid glyph as well as the Files mark.
func TestShellIconRefreshTouchesTheApplicationsDirectory(t *testing.T) {
	dir := useTempDataHome(t)
	apps := filepath.Join(dir, "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(apps, old, old); err != nil {
		t.Fatal(err)
	}

	if err := RefreshShellIcons(); err != nil {
		t.Fatalf("RefreshShellIcons: %v", err)
	}

	info, err := os.Stat(apps)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().After(old) {
		t.Error("the applications directory's timestamp did not move, so the shell has nothing to notice")
	}
}

// TestShellIconRefreshCreatesAMissingDirectory asserts a user who has never
// installed a desktop file still gets the refresh, rather than an error.
func TestShellIconRefreshCreatesAMissingDirectory(t *testing.T) {
	dir := useTempDataHome(t)
	if err := RefreshShellIcons(); err != nil {
		t.Fatalf("RefreshShellIcons: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "applications")); err != nil {
		t.Errorf("applications directory was not created: %v", err)
	}
}

// TestDryRunDoesNotTouchTheShell asserts a preview changes nothing on disk.
func TestDryRunDoesNotTouchTheShell(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	dir := useTempDataHome(t)
	if err := RefreshShellIcons(); err != nil {
		t.Fatalf("RefreshShellIcons: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "applications")); !os.IsNotExist(err) {
		t.Error("dry-run created the applications directory")
	}
}
