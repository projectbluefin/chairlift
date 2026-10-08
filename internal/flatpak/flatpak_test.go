package flatpak

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
			// Flathub publishes a runtime extension in one branch per
			// Platform release, and several may be installed at once; the
			// branch column is what tells those rows apart.
			name:        "runtime extension rows carry their branch",
			output:      "MangoHud\torg.freedesktop.Platform.VulkanLayer.MangoHud\t0.8.4\t25.08\nMangoHud\torg.freedesktop.Platform.VulkanLayer.MangoHud\t0.8.4\t26.08\n",
			installFlag: "--system",
			kind:        KindRuntime,
			want: []Application{
				{Name: "MangoHud", ApplicationID: "org.freedesktop.Platform.VulkanLayer.MangoHud", Version: "0.8.4", Branch: "25.08", Installation: "system", Kind: KindRuntime},
				{Name: "MangoHud", ApplicationID: "org.freedesktop.Platform.VulkanLayer.MangoHud", Version: "0.8.4", Branch: "26.08", Installation: "system", Kind: KindRuntime},
			},
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

func TestRefBranchReadsTheLastRefSegment(t *testing.T) {
	for ref, want := range map[string]string{
		"org.freedesktop.Platform/x86_64/26.08\n":                            "26.08",
		"runtime/org.freedesktop.Platform.VulkanLayer.MangoHud/x86_64/25.08": "25.08",
	} {
		if got, err := RefBranch(ref); err != nil || got != want {
			t.Errorf("RefBranch(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
	for _, ref := range []string{"", "org.freedesktop.Platform", "org.freedesktop.Platform/x86_64/", "a/b/c/d/e"} {
		if got, err := RefBranch(ref); err == nil {
			t.Errorf("RefBranch(%q) = %q, want an error", ref, got)
		}
	}
}

func TestCommandWrappersUseExpectedArguments(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	body := `case "$1" in
list) printf 'Firefox\torg.mozilla.firefox\t120.0\n' ;;
remotes) printf 'flathub\n' ;;
remote-ls) printf 'Firefox\torg.mozilla.firefox\t121.0\n' ;;
info|remote-info) printf 'org.freedesktop.Platform/x86_64/26.08\n' ;;
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
			want: []string{"list", "--user", "--app", "--columns=name,application,version,branch"},
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
			want: []string{"list", "--system", "--app", "--columns=name,application,version,branch"},
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
			want: []string{"list", "--user", "--runtime", "--columns=name,application,version,branch"},
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
			want: []string{"list", "--system", "--runtime", "--columns=name,application,version,branch"},
		},
		{name: "install user", run: func() error { return Install("org.example.App", true) }, want: []string{"install", "-y", "--user", "org.example.App"}},
		{name: "install system", run: func() error { return Install("org.example.App", false) }, want: []string{"install", "-y", "--system", "org.example.App"}},
		{name: "install from remote user", run: func() error { return InstallFromRemote("org.example.App", "flathub", true) }, want: []string{"install", "-y", "--user", "flathub", "org.example.App"}},
		{name: "install from remote system default", run: func() error { return InstallFromRemote("org.example.App", "", false) }, want: []string{"install", "-y", "--system", "org.example.App"}},
		{name: "uninstall user", run: func() error { return Uninstall("org.example.App", true) }, want: []string{"uninstall", "-y", "--user", "org.example.App"}},
		{name: "uninstall system", run: func() error { return Uninstall("org.example.App", false) }, want: []string{"uninstall", "-y", "--system", "org.example.App"}},
		{
			name: "installed app runtime",
			run: func() error {
				runtime, err := AppRuntime("org.example.App")
				if err == nil && runtime != "org.freedesktop.Platform/x86_64/26.08" {
					return errors.New("installed runtime was not returned trimmed")
				}
				return err
			},
			want: []string{"info", "--show-runtime", "org.example.App"},
		},
		{
			name: "remote app runtime system",
			run: func() error {
				runtime, err := RemoteAppRuntime("flathub", "org.example.App", false)
				if err == nil && runtime != "org.freedesktop.Platform/x86_64/26.08" {
					return errors.New("remote runtime was not returned trimmed")
				}
				return err
			},
			want: []string{"remote-info", "--system", "--app", "--show-runtime", "flathub", "org.example.App"},
		},
		{name: "update one user app", run: func() error { return Update(context.Background(), "org.example.App", true) }, want: []string{"update", "-y", "--user", "org.example.App"}},
		{name: "update all system apps", run: func() error { return Update(context.Background(), "", false) }, want: []string{"update", "-y", "--system"}},
		{
			name: "list user app updates",
			run: func() error {
				updates, err := ListUpdates(context.Background(), true)
				if err == nil && (len(updates) != 1 || updates[0].Installation != "user") {
					return errors.New("user update result was not parsed")
				}
				return err
			},
			want: []string{"remote-ls", "--updates", "--app", "--columns=name,application,version", "--user", "flathub"},
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

// TestListUpdatesHonoursContextCancellation ensures the remote-ls query on the
// reconciliation path can be cancelled: a cancelled ctx must abort the flatpak
// invocation promptly instead of waiting out the read timeout, so an uncancellable
// network call can never linger on the mutation path.
func TestListUpdatesHonoursContextCancellation(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	// Fake flatpak that records its arguments then blocks until killed, so a
	// ctx that is not honoured would hang the test rather than return.
	dir := t.TempDir()
	capture := filepath.Join(dir, "args")
	script := filepath.Join(dir, "flatpak")
	source := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CHAIRLIFT_FLATPAK_ARGS\"\nsleep 30\n"
	if err := os.WriteFile(script, []byte(source), 0o755); err != nil {
		t.Fatalf("write fake flatpak: %v", err)
	}
	// Prepend the system PATH so the blocking command resolves; the isolated
	// temp dir alone has no shell utilities.
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("CHAIRLIFT_FLATPAK_ARGS", capture)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := ListUpdates(ctx, true)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("ListUpdates error = nil, want a cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListUpdates error = %v, want context.Canceled", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("ListUpdates took %v; cancelled ctx should abort well under the 30s read timeout", elapsed)
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
		{name: "updates", run: func() error { _, err := ListUpdates(context.Background(), false); return err }},
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
		name string
		user bool
		want []string
	}{
		{name: "user installation", user: true, want: []string{"remote-ls", "--updates", "--app", "--columns=name,application,version", "--user", "flathub"}},
		{name: "system installation", user: false, want: []string{"remote-ls", "--updates", "--app", "--columns=name,application,version", "--system", "flathub"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updateListArgs(tt.user, "flathub"); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("updateListArgs(%v, flathub) = %v, want %v", tt.user, got, tt.want)
			}
		})
	}
}

func TestParseUpdateRemotesSkipsOnlyDisabledRemotes(t *testing.T) {
	output := "flathub\n" +
		"testhub\toci,no-gpg-verify\n" +
		"goose-origin\tdisabled,no-enumerate,no-gpg-verify\n" +
		"flathub-beta\tno-enumerate\n" +
		"\n"
	want := []string{"flathub", "testhub", "flathub-beta"}
	if got := parseUpdateRemotes(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("parseUpdateRemotes = %q, want %q", got, want)
	}
}

// fakeRemotesFlatpak installs a flatpak stand-in for the per-remote update
// query of issue #471. It lists the user remotes flathub, test-center, and
// fedora; flathub offers a Firefox update, fedora a GIMP update, and
// test-center fails like a remote whose host no longer resolves. appOrigins
// and runtimeOrigins are the origin remotes of the installed applications
// and runtimes, which `list --columns=origin` reports according to its
// `--app`/`--runtime` filter, as flatpak does. Every invocation is appended
// to the returned log.
func fakeRemotesFlatpak(t *testing.T, appOrigins, runtimeOrigins string) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	source := `#!/bin/sh
printf '%s\n' "$*" >> "$CHAIRLIFT_FLATPAK_LOG"
case "$1" in
remotes) printf 'flathub\ntest-center\tno-gpg-verify\nfedora\n' ;;
list)
	case "$*" in
	*--app*) printf '` + appOrigins + `' ;;
	*--runtime*) printf '` + runtimeOrigins + `' ;;
	*) printf '` + appOrigins + runtimeOrigins + `' ;;
	esac ;;
remote-ls)
	case "$6" in
	flathub) printf 'Firefox\torg.mozilla.firefox\t131.0\n' ;;
	fedora) printf 'GIMP\torg.gimp.GIMP\t3.0\n' ;;
	*) echo "error: Unable to load summary from remote $6: Could not resolve hostname" >&2; exit 1 ;;
	esac ;;
*) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "flatpak"), []byte(source), 0o755); err != nil {
		t.Fatalf("write fake flatpak: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CHAIRLIFT_FLATPAK_LOG", logPath)
	return logPath
}

var healthyRemoteUpdates = []UpdateInfo{
	{Name: "Firefox", ApplicationID: "org.mozilla.firefox", NewVersion: "131.0", Installation: "user"},
	{Name: "GIMP", ApplicationID: "org.gimp.GIMP", NewVersion: "3.0", Installation: "user"},
}

// Issue #471: a remote left behind after its application was uninstalled
// made `remote-ls --updates` fail for the whole installation, so the update
// check failed. A broken remote nothing installed comes from is ignored, and
// every healthy remote's updates are still listed.
func TestListUpdatesIgnoresABrokenUnusedRemote(t *testing.T) {
	dryrun.Set(false)
	logPath := fakeRemotesFlatpak(t, `flathub\nflathub\nfedora\n`, `flathub\n`)

	updates, err := ListUpdates(context.Background(), true)
	if err != nil {
		t.Fatalf("ListUpdates error = %v, want the unused broken remote ignored", err)
	}
	if !reflect.DeepEqual(updates, healthyRemoteUpdates) {
		t.Fatalf("ListUpdates = %#v, want %#v", updates, healthyRemoteUpdates)
	}
	want := []string{
		"remotes --user --columns=name,options",
		"remote-ls --updates --app --columns=name,application,version --user flathub",
		"remote-ls --updates --app --columns=name,application,version --user test-center",
		"remote-ls --updates --app --columns=name,application,version --user fedora",
		"list --user --app --columns=origin",
	}
	if got := loggedCalls(t, logPath); !reflect.DeepEqual(got, want) {
		t.Fatalf("ListUpdates ran %q, want %q", got, want)
	}
}

// The usual leftover of an application uninstalled without `--unused` is a
// remote that still serves runtimes. The update inventory lists applications
// only, so no update it could show comes from that remote: it is ignored like
// an unused one rather than failing the check.
func TestListUpdatesIgnoresABrokenRemoteServingOnlyRuntimes(t *testing.T) {
	dryrun.Set(false)
	fakeRemotesFlatpak(t, `flathub\nfedora\n`, `flathub\ntest-center\n`)

	updates, err := ListUpdates(context.Background(), true)
	if err != nil {
		t.Fatalf("ListUpdates error = %v, want the runtime-only broken remote ignored", err)
	}
	if !reflect.DeepEqual(updates, healthyRemoteUpdates) {
		t.Fatalf("ListUpdates = %#v, want %#v", updates, healthyRemoteUpdates)
	}
}

// A broken remote an installed application still comes from hides that
// application's updates, so it stays an error naming that remote only,
// alongside the updates the healthy remotes reported.
func TestListUpdatesReportsABrokenRemoteInUse(t *testing.T) {
	dryrun.Set(false)
	fakeRemotesFlatpak(t, `flathub\ntest-center\nfedora\n`, `flathub\n`)

	updates, err := ListUpdates(context.Background(), true)
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) {
		t.Fatalf("ListUpdates error = %v, want a *RemoteError", err)
	}
	if remoteErr.Remote != "test-center" || remoteErr.Installation != "user" {
		t.Fatalf("RemoteError = %+v, want test-center in the user installation", remoteErr)
	}
	for _, want := range []string{`"test-center"`, "user installation", "Could not resolve hostname"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	for _, healthy := range []string{"flathub", "fedora"} {
		if strings.Contains(err.Error(), `"`+healthy+`"`) {
			t.Errorf("error %q names healthy remote %q", err, healthy)
		}
	}
	if !reflect.DeepEqual(updates, healthyRemoteUpdates) {
		t.Fatalf("ListUpdates = %#v, want the healthy remotes' updates %#v", updates, healthyRemoteUpdates)
	}
}

// When the origins cannot be read, no failed remote can be shown unused, so
// it is still reported.
func TestListUpdatesReportsABrokenRemoteWhenOriginsAreUnreadable(t *testing.T) {
	dryrun.Set(false)
	dir := t.TempDir()
	source := "#!/bin/sh\ncase \"$1\" in\nremotes) printf 'test-center\\n' ;;\n*) echo \"$1 failed\" >&2; exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "flatpak"), []byte(source), 0o755); err != nil {
		t.Fatalf("write fake flatpak: %v", err)
	}
	t.Setenv("PATH", dir)

	_, err := ListUpdates(context.Background(), false)
	var remoteErr *RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Remote != "test-center" || remoteErr.Installation != "system" {
		t.Fatalf("ListUpdates error = %v, want test-center reported in the system installation", err)
	}
	if !strings.Contains(err.Error(), "list failed") {
		t.Fatalf("ListUpdates error = %v, want the origin listing failure too", err)
	}
}

func TestListUpdatesWithNoRemotesQueriesNothing(t *testing.T) {
	dryrun.Set(false)
	logPath := fakeFlatpakLog(t, "")

	updates, err := ListUpdates(context.Background(), true)
	if err != nil || len(updates) != 0 {
		t.Fatalf("ListUpdates = %v, %v; want no updates and no error", updates, err)
	}
	if got := loggedCalls(t, logPath); !reflect.DeepEqual(got, []string{"remotes --user --columns=name,options"}) {
		t.Fatalf("ListUpdates ran %q, want only the remote listing", got)
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

// fakeFlatpakLog installs a flatpak stand-in that appends each invocation's
// argument line to a log and answers `remotes` with remotes.
func fakeFlatpakLog(t *testing.T, remotes string) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "log")
	source := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$CHAIRLIFT_FLATPAK_LOG\"\n" +
		"if [ \"$1\" = remotes ]; then printf '" + remotes + "'; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "flatpak"), []byte(source), 0o755); err != nil {
		t.Fatalf("write fake flatpak: %v", err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CHAIRLIFT_FLATPAK_LOG", logPath)
	return logPath
}

func loggedCalls(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
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
