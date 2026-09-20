package gaming

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// fakeFlatpak puts a scripted `flatpak` on PATH and returns the path of the
// file into which every invocation appends its argument line. body runs with
// the flatpak arguments in "$@" and must produce the stdout the real command
// would.
func fakeFlatpak(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	log := filepath.Join(dir, "invocations")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$CHAIRLIFT_GAMING_LOG\"\n" +
		body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "flatpak"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake flatpak: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CHAIRLIFT_GAMING_LOG", log)
	return log
}

func invocations(t *testing.T, log string) []string {
	t.Helper()

	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read fake flatpak log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

// listRow renders one row of `flatpak list --columns=name,application,version,branch,origin,ref`.
func listRow(id, kindFlag string) string {
	prefix := "app"
	if kindFlag == "--runtime" {
		prefix = "runtime"
	}
	return strings.Join([]string{id, id, "1.0", "stable", "flathub", prefix + "/" + id + "/x86_64/stable"}, "\t")
}

// listing is what the fake `flatpak list` answers for each of the four
// (scope, kind) queries the inventory runs. It has four fields rather than
// two because `--app` and `--runtime` are mutually exclusive filters: an ID
// placed in the application listing is genuinely invisible to the runtime
// query and the other way round, exactly as on a real host.
type listing struct {
	userApps       []string
	systemApps     []string
	userRuntimes   []string
	systemRuntimes []string
	// fail names queries that exit non-zero instead of answering. An entry
	// is a scope ("--user"), a kind ("--runtime"), or an exact pair
	// ("--user --runtime").
	fail []string
}

// listingScript renders listing as a `flatpak` stand-in that dispatches on
// the scope and kind flags the inventory passes.
func listingScript(l listing) string {
	// PATH holds only the fake binary's directory, so the script may use
	// nothing but shell builtins.
	rows := func(ids []string, kindFlag string) string {
		quoted := make([]string, 0, len(ids))
		for _, id := range ids {
			quoted = append(quoted, "'"+listRow(id, kindFlag)+"'")
		}
		return strings.Join(quoted, " ")
	}

	fails := func(scope, kindFlag string) bool {
		for _, entry := range l.fail {
			if entry == scope || entry == kindFlag || entry == scope+" "+kindFlag {
				return true
			}
		}
		return false
	}

	emit := func(scope, kindFlag string, ids []string) string {
		if fails(scope, kindFlag) {
			return "  echo 'error: no remote configured' >&2\n  exit 1\n"
		}
		if len(ids) == 0 {
			return "  exit 0\n"
		}
		return "  printf '%s\\n' " + rows(ids, kindFlag) + "\n  exit 0\n"
	}

	queries := []struct {
		scope    string
		kindFlag string
		ids      []string
	}{
		{"--user", "--app", l.userApps},
		{"--user", "--runtime", l.userRuntimes},
		{"--system", "--app", l.systemApps},
		{"--system", "--runtime", l.systemRuntimes},
	}

	// `flatpak list <scope> <kind> --columns=…` puts the scope in $2 and
	// the kind in $3; a mutation ("install -y --user …") matches no case
	// and falls through to the trailing success.
	script := "case \"$2 $3\" in\n"
	for _, query := range queries {
		script += "'" + query.scope + " " + query.kindFlag + "')\n" +
			emit(query.scope, query.kindFlag, query.ids) +
			"  ;;\n"
	}
	return script + "esac\nexit 0\n"
}

// installedComponents is the production value of the listInstalled seam and
// the only place the Flatpak queries are merged. Nothing else exercises it,
// so the user/system split the whole Enable/Disable contract rests on is
// established here.
func TestGamingInventoryMergesUserAndSystemScopes(t *testing.T) {
	fakeFlatpak(t, listingScript(listing{
		userApps:       []string{protonUp},
		systemApps:     []string{steam},
		systemRuntimes: []string{mangohud},
	}))

	scope, err := installedComponents()
	if err != nil {
		t.Fatalf("installedComponents() error = %v, want nil", err)
	}

	for _, id := range []string{steam, protonUp, mangohud} {
		if !scope.Installed[refOf(id)] {
			t.Errorf("scope.Installed[%q] = false, want true — present in either scope counts as installed", id)
		}
	}
	if !scope.User[refOf(protonUp)] {
		t.Errorf("scope.User[%q] = false, want true", protonUp)
	}
	for _, id := range []string{steam, mangohud} {
		if scope.User[refOf(id)] {
			t.Errorf("scope.User[%q] = true, want false — a system-wide component is not ChairLift's to remove", id)
		}
	}
}

// The bug this guards: MangoHud is a runtime extension, so an inventory built
// only from `flatpak list --app` never sees it however it was installed. It
// was therefore reported missing on every refresh — Enable reinstalled it
// each time it ran, and Disable never removed the ref ChairLift had put there.
func TestGamingInventorySeesAUserInstalledRuntimeExtension(t *testing.T) {
	fakeFlatpak(t, listingScript(listing{
		userApps:     []string{steam, protonUp},
		userRuntimes: []string{mangohud},
	}))

	state, err := Status()
	if err != nil {
		t.Fatalf("Status() error = %v, want nil", err)
	}
	if slices.Contains(state.Missing, mangohud) {
		t.Errorf("Status().Missing = %v, want it not to contain the installed runtime extension %q", state.Missing, mangohud)
	}
	if !slices.Contains(state.Installed, mangohud) {
		t.Errorf("Status().Installed = %v, want it to contain %q", state.Installed, mangohud)
	}
	if !slices.Contains(state.UserInstalled, mangohud) {
		t.Errorf("Status().UserInstalled = %v, want %q — ChairLift installed it user-scoped and can remove it",
			state.UserInstalled, mangohud)
	}
}

// The other half of the same bug: an application listing must not be read as
// the runtime inventory. A runtime extension present only system-wide counts
// as installed but is not ChairLift's to remove.
func TestGamingInventoryScopesASystemRuntimeExtensionCorrectly(t *testing.T) {
	fakeFlatpak(t, listingScript(listing{
		userApps:       []string{steam, protonUp},
		systemRuntimes: []string{mangohud},
	}))

	state, err := Status()
	if err != nil {
		t.Fatalf("Status() error = %v, want nil", err)
	}
	if !slices.Contains(state.SystemOnly, mangohud) {
		t.Errorf("Status().SystemOnly = %v, want %q", state.SystemOnly, mangohud)
	}
	if slices.Contains(state.UserInstalled, mangohud) {
		t.Errorf("Status().UserInstalled = %v, want it not to contain the system-scope %q", state.UserInstalled, mangohud)
	}
}

func TestGamingInventorySurvivesOneUnavailableScope(t *testing.T) {
	tests := []struct {
		name     string
		listing  listing
		wantUser bool
	}{
		{
			name:     "system scope unavailable",
			listing:  listing{userApps: []string{steam}, fail: []string{"--system"}},
			wantUser: true,
		},
		{
			name:    "user scope unavailable",
			listing: listing{systemApps: []string{steam}, fail: []string{"--user"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeFlatpak(t, listingScript(test.listing))

			scope, err := installedComponents()
			if err != nil {
				t.Fatalf("installedComponents() error = %v, want nil — one usable scope still yields an answer", err)
			}
			if !scope.Installed[refOf(steam)] {
				t.Errorf("scope.Installed[%q] = false, want the component from the scope that answered", steam)
			}
			if scope.User[refOf(steam)] != test.wantUser {
				t.Errorf("scope.User[%q] = %v, want %v", steam, scope.User[refOf(steam)], test.wantUser)
			}
		})
	}
}

// A kind that answered in neither scope cannot be classified at all, and
// reporting its components "missing" is precisely the reinstall loop this
// change removes. Surface the failure instead.
func TestGamingInventoryFailsWhenARequiredKindAnswersInNeitherScope(t *testing.T) {
	fakeFlatpak(t, listingScript(listing{
		userApps:   []string{steam, protonUp},
		systemApps: []string{flatseal},
		fail:       []string{"--runtime"},
	}))

	scope, err := installedComponents()
	if err == nil {
		t.Fatal("installedComponents() error = nil, want the runtime query's failure surfaced rather than MangoHud reported missing")
	}
	if !strings.Contains(err.Error(), "runtime") {
		t.Errorf("installedComponents() error = %q, want it to name the kind that could not be listed", err)
	}
	if len(scope.Installed) != 0 || len(scope.User) != 0 {
		t.Errorf("installedComponents() scope = %+v, want the zero Scope on error", scope)
	}
}

func TestGamingInventoryFailsWhenBothScopesFail(t *testing.T) {
	fakeFlatpak(t, listingScript(listing{fail: []string{"--user", "--system"}}))

	scope, err := installedComponents()
	if err == nil {
		t.Fatal("installedComponents() error = nil, want the failure surfaced rather than reported as 'nothing installed'")
	}
	if len(scope.Installed) != 0 || len(scope.User) != 0 {
		t.Errorf("installedComponents() scope = %+v, want the zero Scope on error", scope)
	}
}

// Status runs the real seam end to end: a host with only one core component
// present must report gaming mode off.
func TestStatusDerivesFromTheRealFlatpakQuery(t *testing.T) {
	fakeFlatpak(t, listingScript(listing{userApps: []string{steam}}))

	state, err := Status()
	if err != nil {
		t.Fatalf("Status() error = %v, want nil", err)
	}
	if state.Enabled {
		t.Error("Status().Enabled = true, want false — one core component is missing")
	}
	if !reflect.DeepEqual(state.MissingCore, []string{protonUp}) {
		t.Errorf("Status().MissingCore = %v, want %v", state.MissingCore, []string{protonUp})
	}
}

// Disable runs against the real inventory seam so the reported symptom is
// covered end to end: the user-scope MangoHud ref ChairLift installed is the
// one it removes.
func TestDisableRemovesTheUserScopeRuntimeExtension(t *testing.T) {
	log := fakeFlatpak(t, listingScript(listing{
		userApps:     []string{steam},
		userRuntimes: []string{mangohud},
	}))

	removed, skipped, failures := Disable()
	if len(failures) != 0 {
		t.Fatalf("Disable() failures = %v, want none", failures)
	}
	if len(skipped) != 0 {
		t.Errorf("Disable() skipped = %v, want none — both refs are user-scope", skipped)
	}
	if !reflect.DeepEqual(removed, []string{steam, mangohud}) {
		t.Fatalf("Disable() removed = %v, want both user-scope refs including the runtime extension", removed)
	}
	if !slices.Contains(invocations(t, log), "uninstall -y --user "+mangohud) {
		t.Errorf("Disable() invocations = %v, want an unprivileged user-scope uninstall of %q",
			invocations(t, log), mangohud)
	}
}

func TestEnableInstallsOnlyTheMissingComponentsIntoTheUserScope(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam),
		User:      refsOf(steam),
	}, nil)
	log := fakeFlatpak(t, "exit 0")

	installed, failures := Enable()
	if len(failures) != 0 {
		t.Fatalf("Enable() failures = %v, want none", failures)
	}

	want := []string{protonUp, protontrick, goverlay, mangohud, flatseal}
	if !reflect.DeepEqual(installed, want) {
		t.Errorf("Enable() installed = %v, want %v", installed, want)
	}

	calls := invocations(t, log)
	if len(calls) != len(want) {
		t.Fatalf("Enable() ran %d flatpak commands (%v), want %d — the present component must not be reinstalled", len(calls), calls, len(want))
	}
	for i, call := range calls {
		if !strings.HasPrefix(call, "install -y --user ") {
			t.Errorf("Enable() call %d = %q, want an unprivileged user-scope install", i, call)
		}
		if !strings.HasSuffix(call, want[i]) {
			t.Errorf("Enable() call %d = %q, want it to install %q", i, call, want[i])
		}
	}
}

// One unavailable Flathub app must not abort the rest of the stack.
func TestEnableIsolatesAPerComponentInstallFailure(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam, protontrick, goverlay, mangohud, flatseal),
		User:      refsOf(steam, protontrick, goverlay, mangohud, flatseal),
	}, nil)
	fakeFlatpak(t, "echo 'error: app not found' >&2\nexit 1\n")

	installed, failures := Enable()
	if len(installed) != 0 {
		t.Errorf("Enable() installed = %v, want none", installed)
	}
	if len(failures) != 1 {
		t.Fatalf("Enable() failures = %v, want exactly one — the single missing component", failures)
	}
	if !strings.Contains(failures[0].Error(), protonUp) {
		t.Errorf("Enable() failure = %q, want it to name the component %q", failures[0], protonUp)
	}
}

func TestEnableReportsNothingWhenEveryComponentIsPresent(t *testing.T) {
	stubInstalled(t, allComponentIDs(), nil)
	log := fakeFlatpak(t, "exit 0")

	installed, failures := Enable()
	if len(installed) != 0 || len(failures) != 0 {
		t.Errorf("Enable() = (%v, %v), want (none, none)", installed, failures)
	}
	if calls := invocations(t, log); len(calls) != 0 {
		t.Errorf("Enable() ran %v, want no flatpak command at all", calls)
	}
}

func TestDisableRemovesUserScopeComponentsUnprivileged(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam, protonUp, flatseal),
		User:      refsOf(steam, protonUp),
	}, nil)
	log := fakeFlatpak(t, "exit 0")

	removed, skipped, failures := Disable()
	if len(failures) != 0 {
		t.Fatalf("Disable() failures = %v, want none", failures)
	}
	if !reflect.DeepEqual(removed, []string{steam, protonUp}) {
		t.Errorf("Disable() removed = %v, want the user-scope components", removed)
	}
	if !reflect.DeepEqual(skipped, []string{flatseal}) {
		t.Errorf("Disable() skipped = %v, want the system-scope component", skipped)
	}

	calls := invocations(t, log)
	if len(calls) != 2 {
		t.Fatalf("Disable() ran %v, want one uninstall per user-scope component", calls)
	}
	for i, call := range calls {
		if !strings.HasPrefix(call, "uninstall -y --user ") {
			t.Errorf("Disable() call %d = %q, want an unprivileged user-scope uninstall", i, call)
		}
	}
}

func TestDisableIsolatesAPerComponentRemovalFailure(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam, protonUp),
		User:      refsOf(steam, protonUp),
	}, nil)
	fakeFlatpak(t, "echo 'error: app is running' >&2\nexit 1\n")

	removed, skipped, failures := Disable()
	if len(removed) != 0 {
		t.Errorf("Disable() removed = %v, want none", removed)
	}
	if len(skipped) != 0 {
		t.Errorf("Disable() skipped = %v, want none — both components are user-scope", skipped)
	}
	if len(failures) != 2 {
		t.Fatalf("Disable() failures = %v, want one per attempted component", failures)
	}
	for _, id := range []string{steam, protonUp} {
		found := false
		for _, failure := range failures {
			if strings.Contains(failure.Error(), id) {
				found = true
			}
		}
		if !found {
			t.Errorf("Disable() failures = %v, want one naming %q", failures, id)
		}
	}
}
