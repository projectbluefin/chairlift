package updateproviders

import (
	"context"
	"os"
	"path/filepath"
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
					"remote-ls) printf '%s\\n' '" + test.pending + "' ;;\n" +
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
