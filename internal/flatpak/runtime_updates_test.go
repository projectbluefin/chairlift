package flatpak

import (
	"context"
	"fmt"
	"testing"
)

func TestRuntimeOnlyUpdatesRemainVisible(t *testing.T) {
	for _, user := range []bool{true, false} {
		for _, id := range []string{"org.freedesktop.Platform", "org.freedesktop.Platform.Locale"} {
			t.Run(fmt.Sprintf("%t/%s", user, id), func(t *testing.T) {
				scope := installationFlag(user)
				installCapturingFlatpak(t, `case "$1" in
remotes) [ "$2" = `+scope+` ] || exit 1; printf 'flathub\n' ;;
remote-ls) case " $* " in *' --all '*) printf 'Freedesktop Platform\t`+id+`\t26.08\tnew\tx86_64\t26.08\n' ;; esac ;;
list) case " $* " in *' --all '*) printf '`+id+`\tx86_64\t26.08\told\t\n' ;; esac ;;
esac`)
				updates, err := ListUpdates(context.Background(), user)
				if err != nil || len(updates) != 1 {
					t.Fatalf("runtime-only check: updates=%+v err=%v", updates, err)
				}
				if updates[0].UpdateRef() != id+"/x86_64/26.08" {
					t.Fatalf("lost runtime branch: %+v", updates[0])
				}
			})
		}
	}
}

func TestRuntimePhantomUpdateIsFiltered(t *testing.T) {
	installCapturingFlatpak(t, `case "$1" in
remotes) printf 'flathub\n' ;;
remote-ls) printf 'Platform\torg.freedesktop.Platform\t\tcurrent\tx86_64\t26.08\n' ;;
list) case " $* " in *' --all '*) printf 'org.freedesktop.Platform\tx86_64\t26.08\tcurrent\t\n' ;; esac ;;
esac`)
	updates, err := ListUpdates(context.Background(), true)
	if err != nil || len(updates) != 0 {
		t.Fatalf("phantom runtime update: updates=%+v err=%v", updates, err)
	}
}
