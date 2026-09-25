package views

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strings"

	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/gpu"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/launcher"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gdk"
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
	if uh.groupEnabled("help_page", "troubleshooting_group") {
		uh.buildTroubleshootGroup(page)
	}

	// Help Resources group
	if uh.groupEnabled("help_page", "help_resources_group") {
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
	// Diagnostics group (issue #249): Clean action row to gather scrubbed
	// system diagnostics onto the clipboard for support requests.
	uh.buildDiagnosticsGroup(page)

	// Feature availability (issue #209): groups configuration enables but
	// this host cannot back, from the capability set resolved at startup.
	// Collapsed behind one expander, and absent when nothing is missing.
	if rows := pageview.UnavailableFeatures(uh.capabilities, uh.config.IsGroupEnabled); len(rows) > 0 {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Feature availability")
		expander := adw.NewExpanderRow()
		expander.SetTitle("Why is something missing?")
		expander.SetSubtitle("Features this computer cannot run right now")
		for _, r := range rows {
			row := adw.NewActionRow()
			row.SetTitle(r.Title)
			row.SetSubtitle(r.Subtitle)
			expander.AddRow(&row.Widget)
		}
		group.Add(&expander.Widget)
		page.Add(group)
	}
}

func (uh *UserHome) buildDiagnosticsGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Diagnostics")
	group.SetDescription("System information to include when asking for help")

	row := adw.NewActionRow()
	diagRow := pageview.SystemDiagnosticsRow()
	row.SetTitle(diagRow.Title)
	row.SetSubtitle(diagRow.Subtitle)
	row.SetActivatable(true)

	copyBtn := gtk.NewButtonWithLabel("Copy")
	copyBtn.SetValign(gtk.AlignCenterValue)
	copyBtn.AddCssClass("suggested-action")
	copyGate := &actionstate.Gate{}
	onCopy := func() {
		if !copyGate.TryStart() {
			return
		}
		copyBtn.SetSensitive(false)
		go func() {
			// Gather diagnostic data off the main thread.
			var diag pageview.DiagnosticsData
			if info, err := imageinfo.Detect(); err == nil {
				diag.OSName = info.Name
				diag.OSVersion = info.EffectiveTag()
				diag.ImageRef = info.CleanRef()
			}
			if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
				diag.Kernel = strings.TrimSpace(string(data))
			}
			diag.DesktopEnv = deskenv.Detect().String()
			diag.GPU = gpu.Detect().Describe()

			var username, homedir string
			if u, err := user.Current(); err == nil && u != nil {
				username = u.Username
				homedir = u.HomeDir
			} else {
				username = os.Getenv("USER")
				homedir = os.Getenv("HOME")
			}

			text := pageview.FormatScrubbedDiagnostics(diag, username, homedir)

			sgtk.RunOnMainThread(func() {
				defer copyGate.Reset()
				defer copyBtn.SetSensitive(true)
				display := gdk.DisplayGetDefault()
				if display != nil {
					clipboard := display.GetClipboard()
					if clipboard != nil {
						clipboard.SetText(text)
					}
				}
				if uh.toastAdder != nil {
					uh.toastAdder.ShowToast(pageview.DiagnosticsClipboardToast())
				}
			})
		}()
	}

	btnCb := func(_ gtk.Button) { onCopy() }
	copyBtn.ConnectClicked(&btnCb)
	rowCb := func(_ adw.ActionRow) { onCopy() }
	row.ConnectActivated(&rowCb)

	row.AddSuffix(&copyBtn.Widget)
	group.Add(&row.Widget)
	page.Add(group)
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
