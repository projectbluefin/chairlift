package gaming

import (
	"os"
	"path/filepath"
	"reflect"
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
func listRow(id string) string {
	return strings.Join([]string{id, id, "1.0", "stable", "flathub", "app/" + id + "/x86_64/stable"}, "\t")
}

// listingScript answers `list --user` with userIDs and `list --system` with
// systemIDs; a scope named in failScopes exits non-zero instead.
func listingScript(userIDs, systemIDs []string, failScopes ...string) string {
	// PATH holds only the fake binary's directory, so the script may use
	// nothing but shell builtins.
	rows := func(ids []string) string {
		quoted := make([]string, 0, len(ids))
		for _, id := range ids {
			quoted = append(quoted, "'"+listRow(id)+"'")
		}
		return strings.Join(quoted, " ")
	}

	fails := map[string]bool{}
	for _, scope := range failScopes {
		fails[scope] = true
	}

	emit := func(scope string, ids []string) string {
		if fails[scope] {
			return "  echo 'error: no remote configured' >&2\n  exit 1\n"
		}
		if len(ids) == 0 {
			return "  exit 0\n"
		}
		return "  printf '%s\\n' " + rows(ids) + "\n  exit 0\n"
	}

	return "case \"$2\" in\n" +
		"--user)\n" + emit("--user", userIDs) +
		"  ;;\n" +
		"--system)\n" + emit("--system", systemIDs) +
		"  ;;\n" +
		"esac\nexit 0\n"
}

// installedApplications is the production value of the listInstalled seam and
// the only place the two Flatpak scopes are merged. Nothing else exercises it,
// so the user/system split the whole Enable/Disable contract rests on is
// established here.
func TestInstalledApplicationsMergesUserAndSystemScopes(t *testing.T) {
	fakeFlatpak(t, listingScript([]string{protonUp}, []string{steam, mangohud}))

	scope, err := installedApplications()
	if err != nil {
		t.Fatalf("installedApplications() error = %v, want nil", err)
	}

	for _, id := range []string{steam, protonUp, mangohud} {
		if !scope.Installed[id] {
			t.Errorf("scope.Installed[%q] = false, want true — present in either scope counts as installed", id)
		}
	}
	if !scope.User[protonUp] {
		t.Errorf("scope.User[%q] = false, want true", protonUp)
	}
	for _, id := range []string{steam, mangohud} {
		if scope.User[id] {
			t.Errorf("scope.User[%q] = true, want false — a system-wide component is not ChairLift's to remove", id)
		}
	}
}

func TestInstalledApplicationsSurvivesOneUnavailableScope(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		wantUser bool
	}{
		{
			name:     "system scope unavailable",
			script:   listingScript([]string{steam}, nil, "--system"),
			wantUser: true,
		},
		{
			name:   "user scope unavailable",
			script: listingScript(nil, []string{steam}, "--user"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeFlatpak(t, test.script)

			scope, err := installedApplications()
			if err != nil {
				t.Fatalf("installedApplications() error = %v, want nil — one usable scope still yields an answer", err)
			}
			if !scope.Installed[steam] {
				t.Errorf("scope.Installed[%q] = false, want the component from the scope that answered", steam)
			}
			if scope.User[steam] != test.wantUser {
				t.Errorf("scope.User[%q] = %v, want %v", steam, scope.User[steam], test.wantUser)
			}
		})
	}
}

func TestInstalledApplicationsFailsWhenBothScopesFail(t *testing.T) {
	fakeFlatpak(t, listingScript(nil, nil, "--user", "--system"))

	scope, err := installedApplications()
	if err == nil {
		t.Fatal("installedApplications() error = nil, want the failure surfaced rather than reported as 'nothing installed'")
	}
	if len(scope.Installed) != 0 || len(scope.User) != 0 {
		t.Errorf("installedApplications() scope = %+v, want the zero Scope on error", scope)
	}
}

// Status runs the real seam end to end: a host with only one core component
// present must report gaming mode off.
func TestStatusDerivesFromTheRealFlatpakQuery(t *testing.T) {
	fakeFlatpak(t, listingScript([]string{steam}, nil))

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

func TestEnableInstallsOnlyTheMissingComponentsIntoTheUserScope(t *testing.T) {
	stubScope(t, Scope{
		Installed: map[string]bool{steam: true},
		User:      map[string]bool{steam: true},
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
		Installed: map[string]bool{steam: true, protontrick: true, goverlay: true, mangohud: true, flatseal: true},
		User:      map[string]bool{steam: true, protontrick: true, goverlay: true, mangohud: true, flatseal: true},
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
		Installed: map[string]bool{steam: true, protonUp: true, flatseal: true},
		User:      map[string]bool{steam: true, protonUp: true},
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
		Installed: map[string]bool{steam: true, protonUp: true},
		User:      map[string]bool{steam: true, protonUp: true},
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
