package views

import (
	"context"
	"fmt"
	"log"
	"time"

	"codeberg.org/puregotk/puregotk/v4/adw"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/shellextensions"
)

// buildShellExtensionsGroup never writes on load. GNOME owns saved preferences;
// defaults are used only until the real state has been read successfully.
func (uh *UserHome) buildShellExtensionsGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Desktop integrations")
	group.SetDescription("Choose what appears in Quick Settings.")
	page.Add(group)
	for _, extension := range shellextensions.Catalog() {
		row := adw.NewActionRow()
		row.SetTitle(extension.Title)
		row.SetSubtitle("Checking…")
		var toggle *guardedSwitch
		toggle = newGuardedSwitch(extension.DefaultEnabled, func(enabled bool) {
			toggle.widget.SetSensitive(false)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				err := shellextensions.SetEnabled(ctx, extension.UUID, enabled)
				states, readErr := shellextensions.Load(ctx)
				sgtk.RunOnMainThread(func() {
					if readErr != nil {
						log.Printf("views: reading desktop integrations failed: %v", readErr)
						toggle.set(!enabled)
						row.SetSubtitle("Couldn't check this setting. Reopen the app to try again.")
					} else {
						state := states[extension.UUID]
						toggle.set(state.Enabled)
						toggle.widget.SetSensitive(state.Installed)
						if err == nil && !dryrun.Enabled() && state.Enabled != enabled {
							err = fmt.Errorf("GNOME did not apply the requested change")
						}
					}
					if err != nil {
						log.Printf("views: changing %s failed: %v", extension.UUID, err)
						uh.toastAdder.ShowErrorToast("Couldn't change " + extension.Title + ". Try again.")
					} else if readErr != nil {
						uh.toastAdder.ShowErrorToast("Couldn't check whether the change worked.")
					} else if dryrun.Enabled() {
						uh.toastAdder.ShowToast("Preview only — the GNOME extension was not changed.")
					}
				})
			}()
		})
		toggle.widget.SetSensitive(false)
		row.AddSuffix(&toggle.widget.Widget)
		row.SetActivatableWidget(&toggle.widget.Widget)
		group.Add(&row.Widget)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			states, err := shellextensions.Load(ctx)
			sgtk.RunOnMainThread(func() {
				if err != nil {
					log.Printf("views: desktop integrations unavailable: %v", err)
					row.SetSubtitle("Only available on the GNOME desktop.")
					return
				}
				state := states[extension.UUID]
				if !state.Installed {
					row.SetSubtitle("Not installed on this computer.")
					return
				}
				toggle.set(state.Enabled)
				toggle.widget.SetSensitive(true)
				row.SetSubtitle(extension.Description)
			})
		}()
	}
}
