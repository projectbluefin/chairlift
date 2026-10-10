package views

import (
	"context"
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/printerapp"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
)

// printerRow is one driver family's switch row on the Features page. Rows
// are built once, at page-build time, and each holds its own gate: two
// families may act at the same time, but one family admits one action at a
// time, and a readiness probe that lands mid-action yields to it.
//
// Nothing here is privileged. Each application is a rootless quadlet in the
// invoking account driven with `systemctl --user` (internal/printerapp), so
// there is no pkexec route on this group and there must not become one.
type printerRow struct {
	app    printerapp.App
	title  string
	row    *adw.ActionRow
	toggle *guardedSwitch
	gate   actionstate.Gate
	// state is the last state the row rendered; a failed action and a
	// dry-run preview both restore it.
	state printerapp.State
}

// printerActionTimeout bounds one enable or disable end to end. A first
// start pulls the driver image inside the container runtime, which is
// minutes on a slow link; internal/printerapp bounds each systemctl call
// separately.
const printerActionTimeout = 10 * time.Minute

// printerProbeTimeout bounds one readiness query.
const printerProbeTimeout = 15 * time.Second

// printerSettleWait bounds how long a live enable keeps asking after
// `systemctl start` returned before showing whatever state it reached.
const printerSettleWait = time.Minute

// buildPrintersGroup builds one switch row per printer application family.
// The group is built only when the capability floor found Podman, so the
// facts start capable. Everything else the row shows comes from
// printerapp.Resolve: the unit file's presence decides whether a family is
// on, and only a systemctl answer — never the file alone — decides whether
// "on" means running (#331). That answer is a query and runs off the main
// thread; until it lands an installed unit reads as starting.
func (uh *UserHome) buildPrintersGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.PrintersGroupTitle())
	group.SetDescription(pageview.PrintersGroupDescription())

	families := printerapp.Families()
	uh.printerRows = make([]*printerRow, 0, len(families))
	for _, family := range families {
		app := printerapp.Select(family)
		facts := printerapp.Observe(app, true)
		state := printerapp.Resolve(facts)
		pr := &printerRow{app: app, title: pageview.PrinterFamilyRow(family)}
		pr.row = adw.NewActionRow()
		pr.row.SetTitle(pr.title)
		// guardedSwitch, not a bare gtk.Switch: showing the machine's real
		// state here and reverting after a failure are both indistinguishable
		// from a click unless they are marked. Unmarked, a failed stop would
		// revert the switch to on and immediately start the service again.
		pr.toggle = newGuardedSwitch(state.On(), func(on bool) {
			uh.onPrinterAppToggled(pr, on)
		})
		uh.showPrinterAppState(pr, state)
		pr.row.AddSuffix(&pr.toggle.widget.Widget)
		pr.row.SetActivatableWidget(&pr.toggle.widget.Widget)
		group.Add(&pr.row.Widget)
		uh.printerRows = append(uh.printerRows, pr)

		if facts.UnitPresent {
			go uh.probePrinterApp(pr, facts)
		}
	}

	page.Add(group)
	uh.printersGroup = group

	// A single structured readiness marker. The screenshot walkthrough and
	// the AT-SPI suite assert on this line to confirm the captured session
	// really built the rows rather than silently hiding them.
	log.Printf("views: printers group built families=%d", len(families))
}

// showPrinterAppState renders one state on a row. Main thread only.
func (uh *UserHome) showPrinterAppState(pr *printerRow, state printerapp.State) {
	pr.state = state
	pr.row.SetSubtitle(pageview.PrinterAppSubtitle(state, pr.app.Port()))
}

// probePrinterApp asks systemd and diagnostics whether an installed unit's
// service is running and re-renders the row, off the main thread.
func (uh *UserHome) probePrinterApp(pr *printerRow, facts printerapp.Facts) {
	ctx, cancel := context.WithTimeout(context.Background(), printerProbeTimeout)
	defer cancel()
	state := printerapp.ProbeDiagnostics(ctx, pr.app, facts.Capable)

	sgtk.RunOnMainThread(func() {
		// A toggle that started, or already finished, meanwhile owns the
		// row; this probe's facts are stale then.
		if !pr.toggle.widget.GetActive() || !pr.gate.TryStart() {
			return
		}
		uh.showPrinterAppState(pr, state)
		pr.gate.Reset()
	})
}

// onPrinterAppToggled turns one family on or off, off the main thread. Both
// directions go through internal/printerapp, whose every mutation is behind
// dryrun.Enabled(). A failure and a preview both put the switch back where it
// was and restore the row's last state; a failed disable in particular keeps
// the unit, because the service could not be proven stopped, and the switch
// goes back on to say so.
func (uh *UserHome) onPrinterAppToggled(pr *printerRow, enabled bool) {
	if !pr.gate.TryStart() {
		return
	}
	pr.toggle.widget.SetSensitive(false)
	pr.row.SetSubtitle(pageview.PrinterAppWorkingSubtitle(enabled))
	dryRun := dryrun.Enabled()

	go func() {
		defer pr.gate.Reset()

		ctx, cancel := context.WithTimeout(context.Background(), printerActionTimeout)
		defer cancel()

		var err error
		if enabled {
			err = printerapp.Enable(ctx, pr.app)
		} else {
			err = printerapp.Disable(ctx, pr.app)
		}
		facts := printerapp.Observe(pr.app, true)
		if err == nil && !dryRun && facts.UnitPresent {
			active, probeErr := printerapp.WaitSettled(ctx, pr.app, printerSettleWait)
			if probeErr != nil {
				log.Printf("views: printer application %s readiness unknown: %v", pr.app.Family.ID, probeErr)
			}
			facts.Checked, facts.Active = true, active
		}

		var confirmedState printerapp.State
		decision := actionmsg.PrinterApp(dryRun, enabled, pr.title)
		if err == nil && decision.Confirm {
			confirmedState = printerapp.ProbeDiagnostics(ctx, pr.app, true)
		}

		sgtk.RunOnMainThread(func() {
			pr.toggle.widget.SetSensitive(true)

			if err != nil {
				// The error names commands and unit files; it is logged,
				// and the toast says what actually happened.
				log.Printf("views: printer application %s toggle to %v failed: %v", pr.app.Family.ID, enabled, err)
				pr.toggle.set(!enabled)
				uh.showPrinterAppState(pr, pr.state)
				uh.toastAdder.ShowErrorToast(pageview.PrinterAppFailureToast(enabled, pr.title))
				return
			}

			pr.toggle.set(decision.Confirm == enabled)
			if decision.Confirm {
				uh.showPrinterAppState(pr, confirmedState)
			} else {
				uh.showPrinterAppState(pr, pr.state)
			}
			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
