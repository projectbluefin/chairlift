package views

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/launcher"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// buildHelpPage builds the Help page content
func (uh *UserHome) buildHelpPage() {
	page := uh.helpPrefsPage
	if page == nil {
		return
	}

	// Enhanced Troubleshooting leads Help (issue #249): the one task-oriented
	// destination for help and support, with the AI assistant first. It keeps
	// the Features-page behavior — same Homebrew/config gate and shared
	// runner — but now answers "where do I get help?" directly. It is built
	// before resources so the assistant is the first thing a user sees.
	if uh.config.IsGroupEnabled("help_page", "troubleshooting_group") {
		uh.buildTroubleshootGroup(page)
	}

	// Help Resources group
	if uh.config.IsGroupEnabled("help_page", "help_resources_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Help &amp; Resources")
		group.SetDescription("Get help and learn more about " + branding.AppName)

		groupCfg := uh.config.GetGroupConfig("help_page", "help_resources_group")
		if groupCfg != nil {
			resources := pageview.HelpResources(groupCfg.Website, groupCfg.Issues, groupCfg.Chat)
			for _, resource := range resources {
				row := adw.NewActionRow()
				row.SetTitle(resource.Title)
				row.SetSubtitle(resource.URL)
				row.SetActivatable(true)

				icon := gtk.NewImageFromIconName("adw-external-link-symbolic")
				row.AddSuffix(&icon.Widget)

				url := resource.URL
				activatedCb := func(row adw.ActionRow) {
					uh.openURL(url)
				}
				row.ConnectActivated(&activatedCb)

				group.Add(&row.Widget)
			}
		}

		page.Add(group)
	}
}

// openURL opens a URL in the default browser using xdg-open
func (uh *UserHome) openURL(url string) {
	log.Printf("Opening URL: %s", url)

	cmd := exec.Command("xdg-open", url)
	cmd.Env = os.Environ()

	if err := launcher.Start(cmd, func(err error) {
		log.Printf("Failed to open URL %s: %v", url, err)
		// xdg-open exits nonzero when the session has no URL handler or the
		// URL is malformed; surface that async failure instead of silently
		// dropping it. Must run on the GTK main thread.
		sgtk.RunOnMainThread(func() {
			uh.toastAdder.ShowErrorToast(fmt.Sprintf("Failed to open URL: %s", url))
		})
	}); err != nil {
		log.Printf("Failed to open URL %s: %v", url, err)
		uh.toastAdder.ShowErrorToast(fmt.Sprintf("Failed to open URL: %s", url))
		return
	}
}
