package updateproviders

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

func TestSingleAppUpdateVerifiesTheRequestedIdentityInItsScope(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	for _, scope := range []string{"user", "system"} {
		for _, test := range []struct {
			name    string
			pending string
			changed bool
		}{
			{"no-op leaves requested app pending", "Firefox\torg.mozilla.firefox\t131.0", false},
			{"other pending app does not block completion", "Other\torg.example.Other\t2.0", true},
		} {
			t.Run(scope+"/"+test.name, func(t *testing.T) {
				dir := t.TempDir()
				script := "#!/bin/sh\ncase \"$1\" in\n" +
					"update) [ \"$3\" = \"--" + scope + "\" ] && [ \"$4\" = org.mozilla.firefox ] || exit 1 ;;\n" +
					"remotes) [ \"$2\" = \"--" + scope + "\" ] && echo flathub || exit 1 ;;\n" +
					"remote-ls) [ \"$6\" = flathub ] && printf '%s\\n' '" + test.pending + "' ;;\n" +
					"*) exit 1 ;;\nesac\n"
				if err := os.WriteFile(filepath.Join(dir, "flatpak"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", dir)
				result, err := UpdateItem(context.Background(), updateflow.Applications,
					updateflow.Item{ID: "org.mozilla.firefox", Name: "Firefox", Scope: scope})
				if err != nil {
					t.Fatal(err)
				}
				if result.Changed != test.changed || result.Preview {
					t.Fatalf("individual result = %#v, want Changed=%v and no preview", result, test.changed)
				}
			})
		}
	}
}

// writeStub installs an executable stand-in for name in a temporary directory
// and puts that directory alone on $PATH, which is how both integrations
// resolve their binary: flatpak through exec.LookPath, brew through
// homebrew.ExecutablePath's lookPath seam.
func writeStub(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// failingStub is a stand-in that refuses every invocation, so the caller
// observes the integration's failure rather than its success.
const failingStub = "echo 'stub refuses' >&2\nexit 1\n"

func TestUpdateItemRefusesAnApplicationWithoutAnExecutionIdentity(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	for _, test := range []struct {
		name string
		item updateflow.Item
	}{
		{"no application ID", updateflow.Item{Name: "Firefox", Scope: "user"}},
		{"no installation", updateflow.Item{ID: "org.mozilla.firefox", Name: "Firefox"}},
		{"unknown installation", updateflow.Item{ID: "org.mozilla.firefox", Scope: "both"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The guard has to refuse before anything executes, so the only
			// flatpak on $PATH is one that fails the test if it is reached.
			writeStub(t, "flatpak", "echo 'guard ran flatpak' >&2\nexit 9\n")

			result, err := UpdateItem(context.Background(), updateflow.Applications, test.item)
			if err == nil {
				t.Fatalf("UpdateItem() = %#v, want a refusal", result)
			}
			if !strings.Contains(err.Error(), "execution identity or installation") {
				t.Fatalf("UpdateItem() error = %v, want it to name the missing identity or installation", err)
			}
			if result.Changed || result.Preview {
				t.Fatalf("refused update reported %#v, want the zero result", result)
			}
		})
	}
}

func TestUpdateItemReportsAFailedApplicationMutation(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	writeStub(t, "flatpak", failingStub)

	result, err := UpdateItem(context.Background(), updateflow.Applications,
		updateflow.Item{ID: "org.mozilla.firefox", Name: "Firefox", Scope: "user"})
	if err == nil {
		t.Fatalf("UpdateItem() = %#v, want the flatpak failure", result)
	}
	if result.Changed || result.Preview {
		t.Fatalf("failed update reported %#v, want the zero result", result)
	}
}

func TestUpdateItemReportsAFailedApplicationVerification(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	// The mutation succeeds and only the verifying query fails, which is the
	// case that must not be reported to the user as an applied update.
	writeStub(t, "flatpak", "case \"$1\" in\nupdate) exit 0 ;;\n*) exit 1 ;;\nesac\n")

	result, err := UpdateItem(context.Background(), updateflow.Applications,
		updateflow.Item{ID: "org.mozilla.firefox", Name: "Firefox", Scope: "user"})
	if err == nil {
		t.Fatalf("UpdateItem() = %#v, want the verification failure", result)
	}
	if !strings.Contains(err.Error(), "verify application update") {
		t.Fatalf("UpdateItem() error = %v, want it to name the failed verification", err)
	}
	if result.Changed || result.Preview {
		t.Fatalf("unverified update reported %#v, want the zero result", result)
	}
}

func TestUpdateItemPreviewsAnApplicationUnderDryRun(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	// Dry-run must return before the verifying query runs, so a flatpak that
	// fails every invocation still yields a clean preview: the state-changing
	// command is skipped by the dry-run gate and remote-ls is never reached.
	writeStub(t, "flatpak", failingStub)

	result, err := UpdateItem(context.Background(), updateflow.Applications,
		updateflow.Item{ID: "org.mozilla.firefox", Name: "Firefox", Scope: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Preview || result.Changed {
		t.Fatalf("dry-run result = %#v, want a preview that changed nothing", result)
	}
}

func TestUpdateItemRefusesAToolWithoutAPackageName(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	writeStub(t, "brew", "echo 'guard ran brew' >&2\nexit 9\n")

	result, err := UpdateItem(context.Background(), updateflow.DeveloperTools,
		updateflow.Item{ID: "ripgrep"})
	if err == nil {
		t.Fatalf("UpdateItem() = %#v, want a refusal", result)
	}
	if !strings.Contains(err.Error(), "package name") {
		t.Fatalf("UpdateItem() error = %v, want it to name the missing package", err)
	}
	if result.Changed || result.Preview {
		t.Fatalf("refused update reported %#v, want the zero result", result)
	}
}

func TestSingleToolUpdateVerifiesTheRequestedPackage(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	for _, test := range []struct {
		name     string
		outdated string
		changed  bool
	}{
		{"no-op leaves requested tool outdated", `{"formulae":[{"name":"ripgrep","installed_versions":["14.1.0"],"current_version":"14.1.1"}],"casks":[]}`, false},
		{"another outdated tool does not block completion", `{"formulae":[{"name":"fd","installed_versions":["10.2.0"],"current_version":"10.3.0"}],"casks":[]}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeStub(t, "brew", "case \"$1\" in\n"+
				"upgrade) [ \"$2\" = ripgrep ] || exit 1 ;;\n"+
				"outdated) printf '%s' '"+test.outdated+"' ;;\n"+
				"*) exit 1 ;;\nesac\n")

			result, err := UpdateItem(context.Background(), updateflow.DeveloperTools,
				updateflow.Item{Name: "ripgrep"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Changed != test.changed || result.Preview {
				t.Fatalf("individual result = %#v, want Changed=%v and no preview", result, test.changed)
			}
		})
	}
}

func TestUpdateItemReportsAFailedToolMutation(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	writeStub(t, "brew", failingStub)

	result, err := UpdateItem(context.Background(), updateflow.DeveloperTools,
		updateflow.Item{Name: "ripgrep"})
	if err == nil {
		t.Fatalf("UpdateItem() = %#v, want the brew failure", result)
	}
	if result.Changed || result.Preview {
		t.Fatalf("failed update reported %#v, want the zero result", result)
	}
}

func TestUpdateItemReportsAFailedToolVerification(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	writeStub(t, "brew", "case \"$1\" in\nupgrade) exit 0 ;;\n*) exit 1 ;;\nesac\n")

	result, err := UpdateItem(context.Background(), updateflow.DeveloperTools,
		updateflow.Item{Name: "ripgrep"})
	if err == nil {
		t.Fatalf("UpdateItem() = %#v, want the verification failure", result)
	}
	if !strings.Contains(err.Error(), "verify tool update") {
		t.Fatalf("UpdateItem() error = %v, want it to name the failed verification", err)
	}
	if result.Changed || result.Preview {
		t.Fatalf("unverified update reported %#v, want the zero result", result)
	}
}

func TestUpdateItemPreviewsAToolUnderDryRun(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	writeStub(t, "brew", failingStub)

	result, err := UpdateItem(context.Background(), updateflow.DeveloperTools,
		updateflow.Item{Name: "ripgrep"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Preview || result.Changed {
		t.Fatalf("dry-run result = %#v, want a preview that changed nothing", result)
	}
}

func TestUpdateItemRefusesASourceWithoutIndividualUpdates(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	for _, source := range []updateflow.SourceID{updateflow.OperatingSystem, updateflow.SystemComponents, updateflow.SourceID("invented")} {
		t.Run(string(source), func(t *testing.T) {
			result, err := UpdateItem(context.Background(), source, updateflow.Item{ID: "x", Name: "x", Scope: "user"})
			if err == nil {
				t.Fatalf("UpdateItem(%q) = %#v, want a refusal", source, result)
			}
			if !strings.Contains(err.Error(), "individual updates") {
				t.Fatalf("UpdateItem(%q) error = %v, want it to name the unsupported source", source, err)
			}
			if result.Changed || result.Preview {
				t.Fatalf("refused update reported %#v, want the zero result", result)
			}
		})
	}
}

func TestRefreshDeveloperToolsReportsTheRefreshOutcome(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	t.Run("a completed refresh is a change", func(t *testing.T) {
		dryrun.Set(false)
		writeStub(t, "brew", "[ \"$1\" = update ] || exit 1\n")

		result, err := RefreshDeveloperTools(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !result.Changed || result.Preview {
			t.Fatalf("refresh result = %#v, want Changed and no preview", result)
		}
	})

	t.Run("dry-run previews without refreshing", func(t *testing.T) {
		dryrun.Set(true)
		t.Cleanup(func() { dryrun.Set(false) })
		writeStub(t, "brew", failingStub)

		result, err := RefreshDeveloperTools(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !result.Preview || result.Changed {
			t.Fatalf("dry-run refresh result = %#v, want a preview that changed nothing", result)
		}
	})

	t.Run("a failed refresh is not a change", func(t *testing.T) {
		dryrun.Set(false)
		writeStub(t, "brew", failingStub)

		result, err := RefreshDeveloperTools(context.Background())
		if err == nil {
			t.Fatalf("RefreshDeveloperTools() = %#v, want the brew failure", result)
		}
		if result.Changed || result.Preview {
			t.Fatalf("failed refresh reported %#v, want the zero result", result)
		}
	})
}
