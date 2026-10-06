package views

import (
	"context"
	"fmt"
	"log"
	"time"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	"github.com/projectbluefin/chairlift/internal/agentmode"
	"github.com/projectbluefin/chairlift/internal/aistack"
	"github.com/projectbluefin/chairlift/internal/devmenu"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/troubleshoot"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

// buildTroubleshootGroup builds the Troubleshooting group
// (agents_page troubleshooting_group): the Goose row, whose one button
// installs Goose and its read-only tools when they are missing and launches a
// session once Agent Mode serves a model, and the switch for the distro's Ask
// Bluefin panel-menu entry, which is one path into the same session.
//
// The row is an action row rather than a switch because there is nothing
// to turn off: the session's profile is ChairLift's own and is rewritten on
// every launch (internal/troubleshoot), and the packages are ordinary
// Homebrew installs. Signals are connected once, here.
func (uh *UserHome) buildTroubleshootGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.TroubleshootGroupTitle())
	group.SetDescription(pageview.TroubleshootGroupDescription())

	gooseRow := adw.NewActionRow()
	gooseRow.SetTitle(pageview.GooseRowTitle())
	// The subtitle embeds the model reference and setup-step names, neither
	// of which is Pango markup.
	gooseRow.SetUseMarkup(false)
	gooseRow.SetSubtitle("Checking Goose readiness…")
	uh.gooseRow = gooseRow
	uh.gooseSpinner = newActivitySpinner()
	gooseRow.AddSuffix(&uh.gooseSpinner.Widget)
	gooseBtn := gtk.NewButtonWithLabel(pageview.GooseLaunchButtonLabel())
	gooseBtn.SetValign(gtk.AlignCenterValue)
	gooseClicked := func(_ gtk.Button) { uh.onGooseClicked() }
	gooseBtn.ConnectClicked(&gooseClicked)
	gooseBtn.SetSensitive(false)
	gooseRow.AddSuffix(&gooseBtn.Widget)
	uh.gooseLaunchBtn = gooseBtn
	group.Add(&gooseRow.Widget)

	askBluefinRow := adw.NewActionRow()
	askBluefinRow.SetTitle(pageview.AskBluefinMenuRowTitle())
	askBluefinRow.SetSubtitle(pageview.AskBluefinMenuRowSubtitle())
	uh.askBluefinMenuRow = askBluefinRow
	uh.askBluefinToggle = newGuardedSwitch(false, func(on bool) {
		uh.onAskBluefinMenuToggled(on, uh.askBluefinToggle)
	})
	uh.askBluefinToggle.widget.SetSensitive(false)
	askBluefinRow.AddSuffix(&uh.askBluefinToggle.widget.Widget)
	askBluefinRow.SetActivatableWidget(&uh.askBluefinToggle.widget.Widget)
	group.Add(&askBluefinRow.Widget)
	page.Add(group)

	// The menu entry is read the first time the Agents page is shown, not
	// at window build: opening another page must not touch dconf.
	uh.askBluefinMapped = func(gtk.Widget) {
		if uh.askBluefinProbed {
			return
		}
		uh.askBluefinProbed = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			avail, vis, err := devmenu.AskBluefinState(ctx)
			sgtk.RunOnMainThread(func() {
				if uh.askBluefinMenuRow == nil || uh.askBluefinToggle == nil {
					return
				}
				if err != nil || !avail {
					uh.askBluefinToggle.widget.SetSensitive(false)
					return
				}
				uh.askBluefinToggle.set(vis)
				uh.askBluefinToggle.widget.SetSensitive(true)
			})
		}()
	}
	page.ConnectMap(&uh.askBluefinMapped)

	uh.refreshGooseState()
}

// refreshGooseState reads Goose's readiness off the main thread and applies
// it to the row. The Agents page calls it whenever Agent Mode's state or
// model changes. It runs on the main thread; a host whose group is disabled
// never built the row, and the call is a no-op.
func (uh *UserHome) refreshGooseState() {
	if uh.gooseRow == nil {
		return
	}
	generation := uh.gooseRefresh.Begin()
	go func() {
		ctx, cancel := aistack.DefaultContext()
		defer cancel()
		state, facts, _ := agentmode.ObserveLive(ctx)
		log.Printf("views: goose state=%v supported=%v server=%v desktop=%v llmman=%v daemon=%v model=%q",
			state, facts.Tools.Supported, facts.Tools.ServerPath != "", facts.Tools.DesktopPath != "",
			facts.Tools.LLMManPath != "", facts.DaemonHealthy, facts.ActiveModel)
		sgtk.RunOnMainThread(func() {
			if uh.gooseRefresh.IsCurrent(generation) {
				uh.applyGooseState(state, facts)
			}
		})
	}()
}

// applyGooseState puts the row and its button into the shape the state calls
// for. A setup or launch in flight owns the row until it finishes, and then
// applies the state it observed itself.
func (uh *UserHome) applyGooseState(state agentmode.State, facts agentmode.ReadinessFacts) {
	if uh.gooseRow == nil || uh.gooseLaunchBtn == nil || uh.gooseBusy {
		return
	}
	uh.gooseState, uh.gooseFacts = state, facts
	view := pageview.GooseRow(state, facts.ActiveModel)
	uh.gooseRow.SetSubtitle(view.Subtitle)
	uh.gooseLaunchBtn.SetLabel(view.Action.Label())
	uh.gooseLaunchBtn.SetSensitive(view.Action != pageview.GooseNoAction)
}

// finishGooseAction releases the row after a setup or launch and shows the
// state that action observed last. Older reads still in flight are dropped.
func (uh *UserHome) finishGooseAction(state agentmode.State, facts agentmode.ReadinessFacts) {
	uh.gooseGate.Reset()
	uh.gooseBusy = false
	setActivitySpinner(uh.gooseSpinner, false)
	uh.gooseRefresh.Begin()
	uh.applyGooseState(state, facts)
}

// onGooseClicked does whatever the row is currently offering.
func (uh *UserHome) onGooseClicked() {
	switch pageview.GooseRow(uh.gooseState, uh.gooseFacts.ActiveModel).Action {
	case pageview.GooseLaunch:
		uh.launchGoose()
	case pageview.GooseSetUp:
		uh.setUpGoose()
	}
}

// setUpGoose installs the missing packages: the ublue-os tap,
// linux-mcp-server, and the Goose desktop cask with the cpio its preflight
// needs. Every step is a user-scope Homebrew install.
func (uh *UserHome) setUpGoose() {
	if uh.gooseLaunchBtn == nil || !uh.gooseGate.TryStart() {
		return
	}
	uh.gooseBusy = true
	row := uh.gooseRow
	tools := uh.gooseFacts.Tools
	uh.gooseLaunchBtn.SetSensitive(false)
	uh.gooseLaunchBtn.SetLabel("Setting up…")
	setActivitySpinner(uh.gooseSpinner, true)

	go func() {
		_, err := troubleshoot.Setup(tools, func(step string) {
			sgtk.RunOnMainThread(func() { row.SetSubtitle(step + "…") })
		})
		ctx, cancel := aistack.DefaultContext()
		defer cancel()
		state, facts, _ := agentmode.ObserveLive(ctx)
		sgtk.RunOnMainThread(func() {
			uh.finishGooseAction(state, facts)
			if err != nil {
				log.Printf("views: goose setup failed: %v", err)
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Setup failed: %v", err))
				return
			}
			if dryrun.Enabled() {
				// A preview changed nothing, and the row already describes
				// the host as it still is.
				uh.toastAdder.ShowToast("[DRY-RUN] Preview: Goose would be set up — no changes made")
				return
			}
			uh.toastAdder.ShowToast(pageview.GooseSetupToast(state, facts.ActiveModel))
		})
	}()
}

// launchGoose re-reads readiness, then launches through agentmode.Launch,
// which writes ChairLift's Goose profile before a fresh session and hands a
// launch to the session already open. Both run off the main thread.
func (uh *UserHome) launchGoose() {
	if uh.gooseLaunchBtn == nil || !uh.gooseGate.TryStart() {
		return
	}
	uh.gooseBusy = true
	uh.gooseLaunchBtn.SetSensitive(false)
	setActivitySpinner(uh.gooseSpinner, true)
	uh.gooseRow.SetSubtitle("Launching Goose Desktop…")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		state, facts, _ := agentmode.ObserveLive(ctx)
		var launchErr error
		if state.Ready() {
			launchErr = agentmode.Launch(ctx, facts, func(asyncErr error) {
				sgtk.RunOnMainThread(func() {
					log.Printf("views: goose desktop exited with error: %v", asyncErr)
					uh.toastAdder.ShowErrorToast("Goose Desktop encountered an error.")
				})
			})
		}
		dryRun := dryrun.Enabled()
		sgtk.RunOnMainThread(func() {
			uh.finishGooseAction(state, facts)
			switch {
			case !state.Ready():
				uh.toastAdder.ShowErrorToast("Goose cannot be launched: " + state.MissingPrerequisite())
			case launchErr != nil:
				log.Printf("views: launch goose desktop failed: %v", launchErr)
				uh.toastAdder.ShowErrorToast("Failed to launch Goose Desktop.")
			case dryRun:
				uh.toastAdder.ShowToast("[DRY-RUN] Would launch Goose Desktop")
			}
		})
	}()
}

// onAskBluefinMenuToggled shows or hides the distro's Ask Bluefin menu entry.
// It changes only the entry's visibility; ChairLift never rewrites its
// command (internal/devmenu).
func (uh *UserHome) onAskBluefinMenuToggled(enabled bool, toggle *guardedSwitch) {
	if !uh.askBluefinGate.TryStart() {
		return
	}
	toggle.widget.SetSensitive(false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := devmenu.SetAskBluefinVisible(ctx, enabled)
		_, visible, readErr := devmenu.AskBluefinState(ctx)
		sgtk.RunOnMainThread(func() {
			uh.askBluefinGate.Reset()
			toggle.widget.SetSensitive(true)
			if readErr == nil {
				toggle.set(visible)
			}
			if err != nil {
				log.Printf("views: setting ask bluefin menu visibility failed: %v", err)
				uh.toastAdder.ShowErrorToast("Could not update the Ask Bluefin menu entry.")
			}
		})
	}()
}
