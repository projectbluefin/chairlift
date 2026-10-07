package homebrew

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// The messages below are Homebrew's own, verbatim from
// Library/Homebrew/test/cask/uninstall_spec.rb and the formula form of
// dependents_message.rb, which prints Cellar paths for the refused kegs.
func TestUninstallDependentsReadsHomebrewsRefusal(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   []string
	}{
		{
			name: "one dependent",
			stderr: `Error: Refusing to uninstall /home/linuxbrew/.linuxbrew/Cellar/node/22.9.0
because it is required by yarn, which is currently installed.
You can override this and force removal with:
  brew uninstall --ignore-dependencies node
`,
			want: []string{"yarn"},
		},
		{
			name: "two dependents",
			stderr: `Error: Refusing to uninstall local-transmission-zip
because it is required by with-depends-on-cask and with-depends-on-cask-multiple, which are currently installed.
You can override this and force removal with:
  brew uninstall --ignore-dependencies local-transmission-zip
`,
			want: []string{"with-depends-on-cask", "with-depends-on-cask-multiple"},
		},
		{
			name: "several dependents of several packages",
			stderr: `Error: Refusing to uninstall /home/linuxbrew/.linuxbrew/Cellar/python@3.13/3.13.7 and /home/linuxbrew/.linuxbrew/Cellar/openssl@3/3.5.2
because they are required by glib, ublue-os/tap/linux-mcp-server and pipx, which are currently installed.
You can override this and force removal with:
  brew uninstall --ignore-dependencies python@3.13 openssl@3
`,
			want: []string{"glib", "ublue-os/tap/linux-mcp-server", "pipx"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := uninstallDependents(c.stderr)
			if !ok || !reflect.DeepEqual(got, c.want) {
				t.Fatalf("uninstallDependents = (%q, %v), want (%q, true)", got, ok, c.want)
			}
		})
	}
}

func TestUninstallDependentsRejectsOtherFailures(t *testing.T) {
	for _, stderr := range []string{
		"",
		"Error: No such keg: /home/linuxbrew/.linuxbrew/Cellar/jq\n",
		// The refusal line alone is not the message.
		"Error: Refusing to uninstall jq\n",
		// A name outside a package name's characters is not Homebrew's.
		"Error: Refusing to uninstall jq\nbecause it is required by $(rm -rf ~), which is currently installed.\n",
		// Not at the start of a line: replayed, not Homebrew's own error.
		"note: Error: Refusing to uninstall jq\nbecause it is required by yq, which is currently installed.\n",
	} {
		if got, ok := uninstallDependents(stderr); ok {
			t.Errorf("uninstallDependents(%q) = (%q, true), want no classification", stderr, got)
		}
	}
}

func TestRunBrewCommandClassifiesUninstallDependentsOnlyForUninstall(t *testing.T) {
	script := fakeBrew(t, `echo "Error: Refusing to uninstall /home/linuxbrew/.linuxbrew/Cellar/node/22.9.0" >&2
echo "because it is required by yarn, which is currently installed." >&2
exit 1`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := runBrewCommandAt(ctx, script, "uninstall", "node")
	var depErr *DependentsError
	if !errors.As(err, &depErr) {
		t.Fatalf("uninstall err = %T (%v), want *DependentsError", err, err)
	}
	if !reflect.DeepEqual(depErr.Dependents, []string{"yarn"}) {
		t.Fatalf("Dependents = %q, want [yarn]", depErr.Dependents)
	}

	_, err = runBrewCommandAt(ctx, script, "upgrade", "node")
	if errors.As(err, &depErr) {
		t.Fatalf("upgrade err = %T (%v), want the refusal unclassified outside uninstall", err, err)
	}
	var brewErr *Error
	if !errors.As(err, &brewErr) {
		t.Fatalf("upgrade err = %T (%v), want *Error", err, err)
	}
}
