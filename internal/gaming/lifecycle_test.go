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

// listRow renders one row of `flatpak list --columns=name,application,version,branch`.
// An entry may name its branch as "ID//BRANCH"; otherwise an application is on
// "stable" and a runtime on the Platform branch "25.08".
func listRow(entry, kindFlag string) string {
	id, branch, qualified := strings.Cut(entry, "//")
	if !qualified {
		branch = "stable"
		if kindFlag == "--runtime" {
			branch = "25.08"
		}
	}
	return strings.Join([]string{id, id, "1.0", branch}, "\t")
}

// steamRuntime is what `flatpak info --show-runtime` and `flatpak remote-info
// --show-runtime` print for Steam.
const steamRuntime = "org.freedesktop.Platform/x86_64/26.08"

// answersRuntime is a fake flatpak body that succeeds at every mutation and
// reports Steam's runtime, which is what Enable reads MangoHud's branch from.
const answersRuntime = "case \"$1\" in info|remote-info) echo '" + steamRuntime + "' ;; esac\nexit 0\n"

// installs returns the install commands among calls, dropping the runtime
// reads that precede a branch-qualified install.
func installs(calls []string) []string {
	var result []string
	for _, call := range calls {
		if strings.HasPrefix(call, "install ") {
			result = append(result, call)
		}
	}
	return result
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
	if !scope.User[refOf(protonUp)] || scope.System[refOf(protonUp)] {
		t.Errorf("scope of %q = user %v, system %v; want user only", protonUp, scope.User[refOf(protonUp)], scope.System[refOf(protonUp)])
	}
	for _, id := range []string{steam, mangohud} {
		if scope.User[refOf(id)] || !scope.System[refOf(id)] {
			t.Errorf("scope of %q = user %v, system %v; want system only", id, scope.User[refOf(id)], scope.System[refOf(id)])
		}
	}
}

// The bug this guards: MangoHud is a runtime extension, so an inventory built
// only from `flatpak list --app` never sees it however it was installed. It
// was therefore reported missing on every refresh — Enable reinstalled it
// each time it ran, and Disable never removed the ref an earlier, per-user
// ChairLift release had put there.
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
		t.Errorf("Status().UserInstalled = %v, want %q — a per-user copy stays visible and removable",
			state.UserInstalled, mangohud)
	}
}

// The other half of the same bug: an application listing must not be read as
// the runtime inventory. A runtime extension present only system-wide counts
// as installed in the system scope and nowhere else.
func TestGamingInventoryScopesASystemRuntimeExtensionCorrectly(t *testing.T) {
	fakeFlatpak(t, listingScript(listing{
		userApps:       []string{steam, protonUp},
		systemRuntimes: []string{mangohud},
	}))

	state, err := Status()
	if err != nil {
		t.Fatalf("Status() error = %v, want nil", err)
	}
	if !slices.Contains(state.SystemInstalled, mangohud) {
		t.Errorf("Status().SystemInstalled = %v, want %q", state.SystemInstalled, mangohud)
	}
	if slices.Contains(state.UserInstalled, mangohud) {
		t.Errorf("Status().UserInstalled = %v, want it not to contain the system-scope %q", state.UserInstalled, mangohud)
	}
}

func TestGamingInventoryFailsClosedWhenEitherScopeIsUnreadable(t *testing.T) {
	for _, scope := range []string{"--system", "--user"} {
		t.Run(scope, func(t *testing.T) {
			fakeFlatpak(t, listingScript(listing{userApps: []string{steam}, fail: []string{scope}}))
			if _, err := installedComponents(); err == nil {
				t.Fatal("an unreadable scope must not classify missing or system-only components")
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
// covered end to end: the user-scope MangoHud ref an earlier ChairLift release
// installed is removed from the user scope, without authorization.
func TestDisableRemovesTheUserScopeRuntimeExtension(t *testing.T) {
	log := fakeFlatpak(t, listingScript(listing{
		userApps:     []string{steam},
		userRuntimes: []string{mangohud},
	}))

	removed, _, failures := Disable(allComponentIDs())
	if len(failures) != 0 {
		t.Fatalf("Disable() failures = %v, want none", failures)
	}
	if !reflect.DeepEqual(removed, []string{steam, mangohud}) {
		t.Fatalf("Disable() removed = %v, want both user-scope refs including the runtime extension", removed)
	}
	calls := invocations(t, log)
	if !slices.Contains(calls, "uninstall -y --user "+mangohud+"//25.08") {
		t.Errorf("Disable() invocations = %v, want an unprivileged user-scope uninstall of %q's installed branch", calls, mangohud)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "uninstall -y --system ") {
			t.Errorf("Disable() ran %q, want no system uninstall for components installed only per-user", call)
		}
	}
}

// Issues #501 and #503: Bluefin and Dakota configure Flathub only as a system
// remote, so a `--user` install cannot resolve the ref at all. Enable installs
// system-wide, through Flatpak's own PolicyKit, and nothing else — no user
// remote is added and no user-scope command runs.
func TestEnableInstallsOnlyTheMissingComponentsIntoTheSystemScope(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam),
		User:      refsOf(steam),
	}, nil)
	log := fakeFlatpak(t, answersRuntime)

	installed, failures := Enable(allComponentIDs())
	if len(failures) != 0 {
		t.Fatalf("Enable() failures = %v, want none", failures)
	}

	want := []string{protonUp, protontrick, goverlay, mangohud, flatseal}
	if !reflect.DeepEqual(installed, want) {
		t.Errorf("Enable() installed = %v, want %v", installed, want)
	}

	calls := installs(invocations(t, log))
	if len(calls) != len(want) {
		t.Fatalf("Enable() ran %d flatpak installs (%v), want %d — the per-user copy must not be reinstalled system-wide", len(calls), calls, len(want))
	}
	for i, call := range calls {
		ref := want[i]
		if ref == mangohud {
			ref += "//26.08"
		}
		if call != "install -y --system "+ref {
			t.Errorf("Enable() call %d = %q, want %q", i, call, "install -y --system "+ref)
		}
	}
}

// fakeRealFlatpak behaves like flatpak 1.18 against Flathub, which publishes
// MangoHud in one branch per Platform release: a bare MangoHud ref is
// ambiguous, so install and uninstall stop at flatpak's "Which do you want to
// use?" prompt and, with no stdin, fail with "No ref chosen". info answers
// only when steamInstalled; remote-info answers when remoteAnswers.
func fakeRealFlatpak(t *testing.T, l listing, steamInstalled, remoteAnswers bool) string {
	t.Helper()
	info := "echo 'error: com.valvesoftware.Steam/*unspecified*/*unspecified* not installed' >&2; exit 1"
	if steamInstalled {
		info = "echo '" + steamRuntime + "'; exit 0"
	}
	remote := "echo 'error: Unable to load summary from remote flathub' >&2; exit 1"
	if remoteAnswers {
		remote = "echo 'org.freedesktop.Platform/x86_64/25.08'; exit 0"
	}
	return fakeFlatpak(t, "case \"$1\" in\n"+
		"info) "+info+" ;;\n"+
		"remote-info) "+remote+" ;;\n"+
		"install|uninstall) if [ \"$4\" = "+mangohud+" ]; then echo \"error: No ref chosen to resolve matches for '$4'\" >&2; exit 1; fi; exit 0 ;;\n"+
		"esac\n"+listingScript(l))
}

// Flathub publishes MangoHud in several branches, so the bare ID never
// installs. Enable installs the branch Steam runs on: the installed Steam's,
// or — when Steam is not installed — the one Flathub publishes Steam against.
func TestEnableInstallsMangoHudInTheBranchSteamRunsOn(t *testing.T) {
	for _, tt := range []struct {
		name           string
		steamInstalled bool
		want           string
	}{
		{name: "installed Steam", steamInstalled: true, want: mangohud + "//26.08"},
		{name: "Steam on Flathub", steamInstalled: false, want: mangohud + "//25.08"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			l := listing{}
			if tt.steamInstalled {
				l.systemApps = []string{steam}
			}
			log := fakeRealFlatpak(t, l, tt.steamInstalled, true)

			installed, failures := Enable([]string{mangohud})
			if len(failures) != 0 || !reflect.DeepEqual(installed, []string{mangohud}) {
				t.Fatalf("Enable(MangoHud) = %v, %v; want it installed", installed, failures)
			}
			if calls := installs(invocations(t, log)); !reflect.DeepEqual(calls, []string{"install -y --system " + tt.want}) {
				t.Errorf("Enable(MangoHud) installed %q, want %q", calls, "install -y --system "+tt.want)
			}
		})
	}
}

// A branch that cannot be read is a MangoHud failure, never a bare-ID install
// that would stop at flatpak's prompt anyway.
func TestEnableReportsAnUnreadableMangoHudBranchAsAFailure(t *testing.T) {
	log := fakeRealFlatpak(t, listing{}, false, false)

	installed, failures := Enable([]string{mangohud})
	if len(installed) != 0 || len(failures) != 1 || !strings.Contains(failures[0].Error(), mangohud) {
		t.Fatalf("Enable(MangoHud) = %v, %v; want one failure naming %q", installed, failures, mangohud)
	}
	if calls := installs(invocations(t, log)); len(calls) != 0 {
		t.Errorf("Enable(MangoHud) ran %q, want no install without a branch", calls)
	}
}

// Several MangoHud branches can be installed at once, and then the bare ID is
// ambiguous to uninstall as well. Disable removes every installed branch, in
// every scope, by qualified ref.
func TestDisableRemovesEveryInstalledMangoHudBranchByQualifiedRef(t *testing.T) {
	log := fakeRealFlatpak(t, listing{
		userRuntimes:   []string{mangohud + "//25.08", mangohud + "//26.08"},
		systemRuntimes: []string{mangohud + "//26.08"},
	}, false, false)

	removed, kept, failures := Disable([]string{mangohud})
	if len(failures) != 0 || len(kept) != 0 || !reflect.DeepEqual(removed, []string{mangohud}) {
		t.Fatalf("Disable(MangoHud) = %v, %v, %v; want it removed", removed, kept, failures)
	}
	want := []string{
		"uninstall -y --user " + mangohud + "//25.08",
		"uninstall -y --user " + mangohud + "//26.08",
		"uninstall -y --system " + mangohud + "//26.08",
	}
	var got []string
	for _, call := range invocations(t, log) {
		if strings.HasPrefix(call, "uninstall ") {
			got = append(got, call)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Disable(MangoHud) ran %q, want %q", got, want)
	}
}

// One unavailable Flathub app must not abort the rest of the stack.
func TestEnableIsolatesAPerComponentInstallFailure(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam, protontrick, goverlay, mangohud, flatseal),
		User:      refsOf(steam, protontrick, goverlay, mangohud, flatseal),
	}, nil)
	fakeFlatpak(t, "echo 'error: app not found' >&2\nexit 1\n")

	installed, failures := Enable(allComponentIDs())
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

	installed, failures := Enable(allComponentIDs())
	if len(installed) != 0 || len(failures) != 0 {
		t.Errorf("Enable() = (%v, %v), want (none, none)", installed, failures)
	}
	if calls := invocations(t, log); len(calls) != 0 {
		t.Errorf("Enable() ran %v, want no flatpak command at all", calls)
	}
}

// Disable removes a selected component from exactly the scopes it is
// installed in: both copies of a component present twice, the user copy of a
// per-user component, and the system copy of a system-wide one. A component
// that is not installed runs nothing.
func TestDisableRemovesEachComponentFromTheScopesItIsIn(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam, protonUp, flatseal),
		User:      refsOf(steam, protonUp),
		System:    refsOf(steam, flatseal),
	}, nil)
	log := fakeFlatpak(t, "exit 0")

	removed, kept, failures := Disable(allComponentIDs())
	if len(failures) != 0 {
		t.Fatalf("Disable() failures = %v, want none", failures)
	}
	if len(kept) != 0 {
		t.Errorf("Disable() kept = %v, want none — the image declares nothing here", kept)
	}
	if !reflect.DeepEqual(removed, []string{steam, protonUp, flatseal}) {
		t.Errorf("Disable() removed = %v, want every installed component", removed)
	}

	want := []string{
		"uninstall -y --user " + steam,
		"uninstall -y --system " + steam,
		"uninstall -y --user " + protonUp,
		"uninstall -y --system " + flatseal,
	}
	if calls := invocations(t, log); !reflect.DeepEqual(calls, want) {
		t.Errorf("Disable() ran %q, want %q", calls, want)
	}
}

// A component whose system copy could not be removed — a dismissed Flatpak
// authorization, say — is still installed, so it is a failure naming the
// scope, not a removal, even though its user copy went.
func TestDisableReportsAComponentWithACopyLeftAsFailed(t *testing.T) {
	stubScope(t, Scope{
		Installed: refsOf(steam),
		User:      refsOf(steam),
		System:    refsOf(steam),
	}, nil)
	log := fakeFlatpak(t, "if [ \"$3\" = --system ]; then echo 'error: not allowed' >&2; exit 1; fi\nexit 0\n")

	removed, _, failures := Disable([]string{steam})
	if len(removed) != 0 {
		t.Errorf("Disable() removed = %v, want none — the system copy is still installed", removed)
	}
	if len(failures) != 1 || !strings.Contains(failures[0].Error(), steam) || !strings.Contains(failures[0].Error(), "system scope") {
		t.Fatalf("Disable() failures = %v, want one naming %q and the system scope", failures, steam)
	}
	if calls := invocations(t, log); !slices.Contains(calls, "uninstall -y --user "+steam) {
		t.Errorf("Disable() ran %v, want the user copy removed regardless", calls)
	}
}

func TestDisableIsolatesAPerComponentRemovalFailure(t *testing.T) {
	stubInstalled(t, []string{steam, protonUp}, nil)
	fakeFlatpak(t, "echo 'error: app is running' >&2\nexit 1\n")

	removed, _, failures := Disable(allComponentIDs())
	if len(removed) != 0 {
		t.Errorf("Disable() removed = %v, want none", removed)
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

func TestGamingSelectionMutatesOnlyChosenComponents(t *testing.T) {
	for _, component := range components {
		t.Run(component.ID, func(t *testing.T) {
			stubInstalled(t, nil, nil)
			log := fakeFlatpak(t, answersRuntime)
			installed, failures := Enable([]string{component.ID, component.ID})
			if len(failures) != 0 || !reflect.DeepEqual(installed, []string{component.ID}) {
				t.Fatalf("selection result = %v %v", installed, failures)
			}
			want := component.ID
			if component.BranchOf != "" {
				want += "//26.08"
			}
			if calls := installs(invocations(t, log)); !reflect.DeepEqual(calls, []string{"install -y --system " + want}) {
				t.Fatalf("selection installed unchosen or duplicate components: %v", calls)
			}
		})
	}
}

func TestGamingSelectionRejectsUnknownBeforeAnyMutation(t *testing.T) {
	stubInstalled(t, nil, nil)
	log := fakeFlatpak(t, "exit 0")
	if _, failures := Enable([]string{steam, "org.example.Unknown"}); len(failures) != 1 {
		t.Fatal("unknown selection must fail")
	}
	if calls := invocations(t, log); len(calls) != 0 {
		t.Fatalf("unknown selection partially installed: %v", calls)
	}
}

func TestGamingRemovalLeavesUnselectedComponents(t *testing.T) {
	stubInstalled(t, []string{steam, protonUp}, nil)
	log := fakeFlatpak(t, "exit 0")
	removed, _, failures := Disable([]string{protonUp})
	if len(failures) != 0 || !reflect.DeepEqual(removed, []string{protonUp}) {
		t.Fatalf("removal = %v %v", removed, failures)
	}
	if calls := invocations(t, log); !reflect.DeepEqual(calls, []string{"uninstall -y --system " + protonUp}) {
		t.Fatalf("removed unselected component: %v", calls)
	}
}
