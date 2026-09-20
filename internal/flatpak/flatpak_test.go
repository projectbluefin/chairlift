package flatpak

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func installCapturingFlatpak(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "args")
	script := filepath.Join(dir, "flatpak")
	source := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CHAIRLIFT_FLATPAK_ARGS\"\n" + body + "\n"
	if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
		t.Fatalf("write fake flatpak: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CHAIRLIFT_FLATPAK_ARGS", capture)
	return capture
}

func capturedFlatpakArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read captured flatpak arguments: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestParseRefList(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		installFlag string
		kind        Kind
		want        []Application
	}{
		{
			name:        "tab-separated system applications",
			output:      "Firefox\torg.mozilla.firefox\t120.0\n",
			installFlag: "--system",
			kind:        KindApplication,
			want: []Application{{
				Name: "Firefox", ApplicationID: "org.mozilla.firefox", Version: "120.0",
				Installation: "system", Kind: KindApplication,
			}},
		},
		{
			name:        "space-separated user application",
			output:      "GIMP org.gimp.GIMP 2.10",
			installFlag: "--user",
			kind:        KindApplication,
			want: []Application{{
				Name: "GIMP", ApplicationID: "org.gimp.GIMP", Version: "2.10",
				Installation: "user", Kind: KindApplication,
			}},
		},
		{
			// A runtime extension is the shape `--app` never reports. The
			// kind comes from the requested filter, not from the ref
			// column, so the classification survives a row that has to
			// fall back to whitespace splitting.
			name:        "user runtime extension",
			output:      "MangoHud\torg.freedesktop.Platform.VulkanLayer.MangoHud\t0.8.1\n",
			installFlag: "--user",
			kind:        KindRuntime,
			want: []Application{{
				Name: "MangoHud", ApplicationID: "org.freedesktop.Platform.VulkanLayer.MangoHud",
				Version: "0.8.1", Installation: "user", Kind: KindRuntime,
			}},
		},
		{
			name:        "malformed and blank rows are skipped",
			output:      "\nnot-enough-fields\n",
			installFlag: "--user",
			kind:        KindApplication,
			want:        nil,
		},
		{name: "empty output", output: "", installFlag: "--system", kind: KindApplication, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRefList(tt.output, tt.installFlag, tt.kind)
			if err != nil {
				t.Fatalf("parseRefList() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseRefList() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCommandWrappersUseExpectedArguments(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	body := `case "$1" in
list) printf 'Firefox\torg.mozilla.firefox\t120.0\n' ;;
remote-ls) printf 'Firefox\torg.mozilla.firefox\t121.0\n' ;;
esac`
	capture := installCapturingFlatpak(t, body)

	tests := []struct {
		name string
		run  func() error
		want []string
	}{
		{
			name: "list user applications",
			run: func() error {
				apps, err := ListUserApplications()
				if err == nil && (len(apps) != 1 || apps[0].Installation != "user") {
					return errors.New("user application result was not parsed")
				}
				return err
			},
			want: []string{"list", "--user", "--app", "--columns=name,application,version"},
		},
		{
			name: "list system applications",
			run: func() error {
				apps, err := ListSystemApplications()
				if err == nil && (len(apps) != 1 || apps[0].Installation != "system") {
					return errors.New("system application result was not parsed")
				}
				return err
			},
			want: []string{"list", "--system", "--app", "--columns=name,application,version"},
		},
		{
			// The regression this guards: a runtime extension is invisible
			// to `list --app`, so the runtime listings must ask for
			// `--runtime` rather than reusing the application filter.
			name: "list user runtimes",
			run: func() error {
				runtimes, err := ListUserRuntimes()
				if err == nil && (len(runtimes) != 1 || runtimes[0].Kind != KindRuntime || runtimes[0].Installation != "user") {
					return errors.New("user runtime result was not parsed")
				}
				return err
			},
			want: []string{"list", "--user", "--runtime", "--columns=name,application,version"},
		},
		{
			name: "list system runtimes",
			run: func() error {
				runtimes, err := ListSystemRuntimes()
				if err == nil && (len(runtimes) != 1 || runtimes[0].Kind != KindRuntime || runtimes[0].Installation != "system") {
					return errors.New("system runtime result was not parsed")
				}
				return err
			},
			want: []string{"list", "--system", "--runtime", "--columns=name,application,version"},
		},
		{name: "install user", run: func() error { return Install("org.example.App", true) }, want: []string{"install", "-y", "--user", "org.example.App"}},
		{name: "install system", run: func() error { return Install("org.example.App", false) }, want: []string{"install", "-y", "--system", "org.example.App"}},
		{name: "uninstall user", run: func() error { return Uninstall("org.example.App", true) }, want: []string{"uninstall", "-y", "--user", "org.example.App"}},
		{name: "uninstall system", run: func() error { return Uninstall("org.example.App", false) }, want: []string{"uninstall", "-y", "--system", "org.example.App"}},
		{name: "update one user app", run: func() error { return Update(context.Background(), "org.example.App", true) }, want: []string{"update", "-y", "--user", "org.example.App"}},
		{name: "update all system apps", run: func() error { return Update(context.Background(), "", false) }, want: []string{"update", "-y", "--system"}},
		{
			name: "list user app updates",
			run: func() error {
				updates, err := ListUpdates(true)
				if err == nil && (len(updates) != 1 || updates[0].Installation != "user") {
					return errors.New("user update result was not parsed")
				}
				return err
			},
			want: []string{"remote-ls", "--updates", "--app", "--columns=name,application,version", "--user"},
		},
		{name: "remove all user apps", run: RemoveAllUser, want: []string{"uninstall", "--user", "--all", "-y"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); err != nil {
				t.Fatalf("wrapper returned error: %v", err)
			}
			if got := capturedFlatpakArgs(t, capture); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("flatpak arguments = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUninstallUnusedRunsBothScopes(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	dir := t.TempDir()
	capture := filepath.Join(dir, "args")
	script := filepath.Join(dir, "flatpak")
	// Append (not overwrite) so both scope invocations are recorded.
	source := "#!/bin/sh\necho \"$@\" >> \"$CHAIRLIFT_FLATPAK_ARGS\"\n"
	if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
		t.Fatalf("write fake flatpak: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CHAIRLIFT_FLATPAK_ARGS", capture)

	_, err := UninstallUnused()
	if err != nil {
		t.Fatalf("UninstallUnused() error = %v", err)
	}

	got := capturedFlatpakArgs(t, capture)
	want := []string{
		"uninstall --unused -y --user",
		"uninstall --unused -y --system",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flatpak arguments = %v, want %v", got, want)
	}
}

// TestUninstallUnusedReportsScopeError ensures an error from either scope is
// surfaced (wrapped with its scope) instead of being swallowed, so the UI can
// report that cleanup did not fully succeed.
func TestUninstallUnusedReportsScopeError(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	// Fail only the system-scope invocation.
	installCapturingFlatpak(t, `case "$@" in *--system*) echo 'system failed' >&2; exit 1;; esac`)

	_, err := UninstallUnused()
	if err == nil || !strings.Contains(err.Error(), "system scope") || !strings.Contains(err.Error(), "system failed") {
		t.Fatalf("error = %v, want it to mention the system scope and its failure", err)
	}
}

func TestQueryFailuresPropagate(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	installCapturingFlatpak(t, `echo 'query failed' >&2; exit 7`)

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "applications", run: func() error { _, err := ListUserApplications(); return err }},
		{name: "updates", run: func() error { _, err := ListUpdates(false); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil || !strings.Contains(err.Error(), "query failed") {
				t.Fatalf("query error = %v, want propagated stderr", err)
			}
		})
	}
}

func TestDryRunSkipsEveryStateChangingCommand(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	t.Setenv("PATH", t.TempDir())

	for command := range stateChangingCommands {
		t.Run(command, func(t *testing.T) {
			output, err := runFlatpakCommand(command, "--example")
			if err != nil {
				t.Fatalf("runFlatpakCommand dry-run error = %v", err)
			}
			want := "[DRY-RUN] Would execute: flatpak " + command + " --example"
			if output != want {
				t.Fatalf("dry-run output = %q, want %q", output, want)
			}
		})
	}
	if !dryrun.Enabled() {
		t.Fatal("dryrun.Enabled() = false after dryrun.Set(true)")
	}
}

func TestUpdateListArgs(t *testing.T) {
	tests := []struct {
		name        string
		user        bool
		wantFlag    string
		notWantFlag string
	}{
		{name: "user installation", user: true, wantFlag: "--user", notWantFlag: "--system"},
		{name: "system installation", user: false, wantFlag: "--system", notWantFlag: "--user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := updateListArgs(tt.user)

			for _, want := range []string{"remote-ls", "--updates", "--app", tt.wantFlag} {
				if !slices.Contains(args, want) {
					t.Errorf("updateListArgs(%v) = %v, missing %q", tt.user, args, want)
				}
			}
			if slices.Contains(args, tt.notWantFlag) {
				t.Errorf("updateListArgs(%v) = %v, must not contain %q", tt.user, args, tt.notWantFlag)
			}
		})
	}
}

func TestParseUpdateList(t *testing.T) {
	tests := []struct {
		name   string
		output string
		user   bool
		want   []UpdateInfo
	}{
		{
			name:   "tab separated rows",
			output: "Firefox\torg.mozilla.firefox\t120.0\nGIMP\torg.gimp.GIMP\t2.10.36\n",
			user:   false,
			want: []UpdateInfo{
				{Name: "Firefox", ApplicationID: "org.mozilla.firefox", NewVersion: "120.0", Installation: "system"},
				{Name: "GIMP", ApplicationID: "org.gimp.GIMP", NewVersion: "2.10.36", Installation: "system"},
			},
		},
		{
			name:   "whitespace separated fallback",
			output: "Firefox   org.mozilla.firefox   120.0",
			user:   false,
			want: []UpdateInfo{
				{Name: "Firefox", ApplicationID: "org.mozilla.firefox", NewVersion: "120.0", Installation: "system"},
			},
		},
		{
			name:   "short row is partially parsed",
			output: "Firefox org.mozilla.firefox",
			user:   false,
			want: []UpdateInfo{
				{Name: "Firefox", ApplicationID: "org.mozilla.firefox", Installation: "system"},
			},
		},
		{
			name:   "row with fewer than two fields is skipped",
			output: "Firefox\nGIMP\torg.gimp.GIMP\t2.10.36",
			user:   false,
			want: []UpdateInfo{
				{Name: "GIMP", ApplicationID: "org.gimp.GIMP", NewVersion: "2.10.36", Installation: "system"},
			},
		},
		{
			name:   "blank and whitespace-only lines are skipped",
			output: "\n   \nFirefox\torg.mozilla.firefox\t120.0\n\t\n",
			user:   false,
			want: []UpdateInfo{
				{Name: "Firefox", ApplicationID: "org.mozilla.firefox", NewVersion: "120.0", Installation: "system"},
			},
		},
		{
			name:   "empty output yields no updates",
			output: "",
			user:   false,
			want:   nil,
		},
		{
			name:   "user installation label",
			output: "Firefox\torg.mozilla.firefox\t120.0",
			user:   true,
			want: []UpdateInfo{
				{Name: "Firefox", ApplicationID: "org.mozilla.firefox", NewVersion: "120.0", Installation: "user"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUpdateList(tt.output, tt.user)
			if err != nil {
				t.Fatalf("parseUpdateList() unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseUpdateList() = %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("parseUpdateList()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCommandTimeout(t *testing.T) {
	if len(stateChangingCommands) != 4 {
		t.Fatalf("stateChangingCommands has %d entries, want 4: update this test when the map changes", len(stateChangingCommands))
	}

	for cmd := range stateChangingCommands {
		t.Run("state-changing/"+cmd, func(t *testing.T) {
			if got := commandTimeout([]string{cmd, "-y", "org.example.App"}); got != mutationTimeout {
				t.Errorf("commandTimeout(%q) = %v, want %v", cmd, got, mutationTimeout)
			}
		})
	}

	readCases := []struct {
		name string
		args []string
	}{
		{name: "read/list", args: []string{"list", "--user", "--app"}},
		{name: "read/remote-ls", args: []string{"remote-ls", "--updates", "--app"}},
		{name: "empty args", args: nil},
	}

	for _, tc := range readCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandTimeout(tc.args); got != readTimeout {
				t.Errorf("commandTimeout(%v) = %v, want %v", tc.args, got, readTimeout)
			}
		})
	}
}

func TestTimeoutConstants(t *testing.T) {
	if readTimeout != 30*time.Second {
		t.Errorf("readTimeout = %v, want 30s", readTimeout)
	}
	if mutationTimeout != 30*time.Minute {
		t.Errorf("mutationTimeout = %v, want 30m", mutationTimeout)
	}
}

// TestUpdateDryRunReturnsNilWithoutRunning proves the dry-run branch of Update:
// a state-changing command is skipped (not executed) under --dry-run, so the
// in-flight-Update All cancellation path never runs the command in a preview.
func TestUpdateDryRunReturnsNilWithoutRunning(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := Update(context.Background(), "", true); err != nil {
		t.Fatalf("Update dry-run error = %v, want nil (command must not run)", err)
	}
}
