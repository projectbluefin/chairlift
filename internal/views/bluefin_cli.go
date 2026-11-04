package views

import (
	"fmt"
	"log"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/bluefincli"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/firstrun"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
)

type bluefinCLIRow struct {
	row     *adw.ExpanderRow
	toggle  *guardedSwitch
	spinner *gtk.Spinner
	state   bluefincli.State
	loaded  bool
	err     error
	gate    actionstate.Gate
	setup   *firstrun.BluefinCLISelection
}

func (uh *UserHome) buildBluefinCLIGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle("Terminal")
	group.SetDescription("Bluefin CLI supports Bash only. Fish and Zsh are not managed.")
	cli := &bluefinCLIRow{row: adw.NewExpanderRow(), spinner: newActivitySpinner()}
	cli.row.SetTitle("Bluefin CLI")
	cli.row.SetSubtitle("Checking Bash setup…")
	cli.toggle = newGuardedSwitch(false, func(on bool) {
		if cli.setup != nil {
			cli.setup.Choose(on)
			uh.showBluefinCLIState()
			return
		}
		uh.runBluefinCLI(on, nil)
	})
	SetAccessibleLabel(cli.toggle.widget, "Bluefin CLI")
	cli.toggle.widget.SetSensitive(false)
	cli.row.AddSuffix(&cli.spinner.Widget)
	cli.row.AddSuffix(&cli.toggle.widget.Widget)
	items := bluefincli.Items()
	addContentsRows(cli.row, items, len(items))
	group.Add(&cli.row.Widget)
	page.Add(group)
	uh.bluefinCLI = cli
	go func() {
		state, err := bluefincli.Status()
		sgtk.RunOnMainThread(func() {
			cli.state, cli.err, cli.loaded = state, err, true
			if cli.setup != nil {
				cli.setup.Observe(state.Disabled, state.Enabled)
			}
			uh.showBluefinCLIState()
		})
	}()
}

func (uh *UserHome) showBluefinCLIState() {
	cli := uh.bluefinCLI
	if cli == nil || !cli.loaded {
		return
	}
	if !bluefincli.Supported() {
		cli.toggle.set(false)
		cli.toggle.widget.SetSensitive(false)
		cli.row.SetSubtitle("Bash only. Your selected login shell is not supported.")
		return
	}
	cli.toggle.widget.SetSensitive(!cli.gate.Running())
	if cli.setup != nil {
		cli.toggle.set(cli.setup.Selected)
		if cli.setup.Applied {
			switch {
			case dryrun.Enabled():
				cli.row.SetSubtitle("Preview mode — this setup makes no changes.")
			case cli.state.Enabled:
				cli.row.SetSubtitle("Enabled for Bash. Open a new Bash terminal for changes to take effect.")
			default:
				cli.row.SetSubtitle("Bash integration is off; installed tools are kept.")
			}
		} else if cli.setup.Selected {
			cli.row.SetSubtitle("Selected for setup. Next installs the tools and configures Bash.")
		} else {
			cli.row.SetSubtitle("Not selected. Next keeps Bash integration off; installed tools are kept.")
		}
		return
	}
	cli.toggle.set(cli.state.Enabled)
	switch {
	case cli.err != nil:
		if cli.state.Configured || cli.state.Legacy {
			cli.row.SetSubtitle("Bash setup is incomplete. Turn off to remove managed settings, then on to repair.")
			cli.toggle.set(true)
		} else {
			cli.row.SetSubtitle("Bash setup is incomplete. Turn on to repair it.")
		}
	case cli.state.Enabled:
		cli.row.SetSubtitle("Enabled for Bash. Turning off keeps installed tools.")
	case cli.state.Configured:
		cli.toggle.set(true)
		cli.row.SetSubtitle("Bash integration is enabled, but some tools are missing. Turn off to remove it, then on to repair.")
	case cli.state.Legacy:
		cli.toggle.set(true)
		cli.row.SetSubtitle("Legacy Bash integration is enabled. Turn off to remove it, then on to migrate to the managed setup.")
	case cli.state.PackagesInstalled:
		cli.row.SetSubtitle("Tools installed; Bash integration is off.")
	default:
		cli.row.SetSubtitle("Install terminal tools, prompt, and Bash integration. Turning off keeps tools.")
	}
}

func (uh *UserHome) refreshBluefinCLI() {
	cli := uh.bluefinCLI
	if cli == nil || !cli.loaded || cli.setup != nil || !cli.gate.TryStart() {
		return
	}
	cli.toggle.widget.SetSensitive(false)
	go func() {
		state, err := bluefincli.Status()
		sgtk.RunOnMainThread(func() {
			cli.state, cli.err = state, err
			if cli.setup != nil {
				cli.setup.Observe(state.Disabled, state.Enabled)
			}
			cli.gate.Reset()
			uh.showBluefinCLIState()
		})
	}()
}

// BeginBluefinCLISetup changes only the pending choice, not installed state.
func (uh *UserHome) BeginBluefinCLISetup() {
	cli := uh.bluefinCLI
	if cli == nil || cli.setup != nil {
		return
	}
	choice := firstrun.NewBluefinCLISelection()
	cli.setup = &choice
	if cli.loaded {
		cli.setup.Observe(cli.state.Disabled, cli.state.Enabled)
	}
	uh.showBluefinCLIState()
}

func (uh *UserHome) EndBluefinCLISetup() {
	if uh.bluefinCLI == nil {
		return
	}
	uh.bluefinCLI.setup = nil
	uh.showBluefinCLIState()
	uh.refreshBluefinCLI()
}

// ApplyBluefinCLISetup is the Features step's explicit Next action. It never
// runs for ordinary activation or Back, and failures leave the step retryable.
func (uh *UserHome) ApplyBluefinCLISetup(done func(bool)) {
	cli := uh.bluefinCLI
	if cli == nil || cli.setup == nil || !bluefincli.Supported() {
		done(true)
		return
	}
	if !cli.loaded {
		uh.toastAdder.ShowToast("Bash setup is still being checked. Try Next again.")
		done(false)
		return
	}
	if cli.setup.Applied {
		done(true)
		return
	}
	uh.runBluefinCLI(cli.setup.Selected, done)
}

func (uh *UserHome) runBluefinCLI(on bool, done func(bool)) {
	cli := uh.bluefinCLI
	if cli == nil || !cli.gate.TryStart() {
		if done != nil {
			uh.toastAdder.ShowToast("Bash setup is busy. Try Next again when it finishes.")
			done(false)
		}
		return
	}
	cli.toggle.widget.SetSensitive(false)
	cli.spinner.Start()
	cli.row.SetSubtitle("Updating Bash setup…")
	setup := cli.setup
	go func() {
		apply := bluefincli.Disable
		if on {
			apply = bluefincli.Enable
		}
		var err error
		var choice firstrun.BluefinCLISelection
		if setup != nil {
			choice = *setup
			err = choice.Apply(bluefincli.Enable, bluefincli.Disable)
		} else {
			err = apply()
		}
		state, statusErr := bluefincli.Status()
		if err == nil && !dryrun.Enabled() && on && !state.Enabled {
			err = statusErr
			if err == nil {
				err = fmt.Errorf("Bash integration did not become ready")
			}
		}
		sgtk.RunOnMainThread(func() {
			cli.gate.Reset()
			cli.spinner.Stop()
			cli.state, cli.err = state, statusErr
			if setup != nil && cli.setup == setup && err == nil {
				*setup = choice
			} else if cli.setup != nil && cli.setup != setup {
				cli.setup.Observe(state.Disabled, state.Enabled)
			}
			uh.showBluefinCLIState()
			if err != nil {
				log.Printf("bluefin-cli: changing Bash setup: %v", err)
				uh.toastAdder.ShowErrorToast("Couldn't change Bluefin CLI: " + err.Error())
			} else if dryrun.Enabled() {
				uh.toastAdder.ShowToast("[DRY-RUN] Bluefin CLI Bash setup previewed — no changes made")
			} else {
				message := "Bluefin CLI disabled. Installed tools were kept. Open a new Bash terminal."
				if on {
					message = "Bluefin CLI enabled. Open a new Bash terminal."
				}
				uh.toastAdder.ShowToast(message)
				uh.homebrewInventoryChanged()
			}
			if done != nil && cli.setup == setup {
				done(err == nil)
			}
		})
	}()
}
