package views

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/printerapp"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
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
	admin  *gtk.Button
	gate   actionstate.Gate
	// state is the last state the row rendered; a failed action and a
	// dry-run preview both restore it.
	state printerapp.State
	// unverified marks a unit written before signatures were checked; the
	// subtitle asks the person to turn it off and on again (ADR-0023).
	unverified bool

	// usb picks the USB printer passed through to this family. It shares
	// the row's gate: a choice restarts a running family, so it must not
	// overlap a toggle. Its two handlers are connected once, at build time.
	usb       *adw.ComboRow
	usbNotify func(gobject.Object, uintptr)
	usbHover  func(gtk.Widget, gtk.StateFlags)
	// usbChoices is what the picker lists, in order; usbSaved is the saved
	// choice, and usbStatus how it stands among the last scan.
	usbChoices []printerapp.USBChoice
	usbSaved   printerapp.USBIdentity
	usbStatus  printerapp.USBStatus
	// usbReady is set once the saved choice has been read; until then, and
	// when it cannot be read, the picker is insensitive.
	usbReady bool
	// usbSuppress marks the picker's own model and selection updates, which
	// emit the same notifications as a person's choice.
	usbSuppress bool
	usbScanning bool
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
		pr.unverified = facts.UnitPresent && printerapp.InstalledBeforeVerification(app)
		pr.row = adw.NewActionRow()
		pr.row.SetTitle(pr.title)
		// guardedSwitch, not a bare gtk.Switch: showing the machine's real
		// state here and reverting after a failure are both indistinguishable
		// from a click unless they are marked. Unmarked, a failed stop would
		// revert the switch to on and immediately start the service again.
		pr.toggle = newGuardedSwitch(state.On(), func(on bool) {
			uh.onPrinterAppToggled(pr, on)
		})
		// The row's own web page, as a button: the subtitle names the loopback
		// address, and nobody should have to retype it (#671). Built beside
		// the switch at page-build time, never in a refresh path, and it opens
		// only the app's own published port (printerapp.WebAdminURL).
		pr.admin = gtk.NewButtonWithLabel(pageview.PrinterAdminButtonLabel())
		pr.admin.SetValign(gtk.AlignCenterValue)
		describeFor(pr.admin, pr.title)
		adminClicked := func(_ gtk.Button) { uh.openURL(pr.app.WebAdminURL()) }
		pr.admin.ConnectClicked(&adminClicked)
		uh.showPrinterAppState(pr, state)
		pr.row.AddSuffix(&pr.admin.Widget)
		pr.row.AddSuffix(&pr.toggle.widget.Widget)
		pr.row.SetActivatableWidget(&pr.toggle.widget.Widget)
		group.Add(&pr.row.Widget)
		uh.buildPrinterUSBRow(group, pr)
		uh.printerRows = append(uh.printerRows, pr)

		if facts.UnitPresent {
			go uh.probePrinterApp(pr, facts)
		}
	}

	page.Add(group)
	uh.printersGroup = group
	go uh.loadPrinterUSBChoices(uh.printerRows)

	// A single structured readiness marker. The screenshot walkthrough and
	// the AT-SPI suite assert on this line to confirm the captured session
	// really built the rows rather than silently hiding them.
	log.Printf("views: printers group built families=%d", len(families))
}

// showPrinterAppState renders one state on a row. Main thread only.
func (uh *UserHome) showPrinterAppState(pr *printerRow, state printerapp.State) {
	pr.state = state
	subtitle := pageview.PrinterAppSubtitle(state, pr.app.Port())
	if pr.unverified && state.On() {
		subtitle = pageview.PrinterAppUnverifiedSubtitle(subtitle)
	}
	if pr.usbStatus == printerapp.USBNotConnected {
		subtitle = pageview.PrinterAppUSBNotConnectedSubtitle(subtitle)
	}
	pr.row.SetSubtitle(subtitle)

	// The web page exists once the family is installed, and answers once its
	// service is observed running: the button appears with the family and
	// stays insensitive through a start or a failure rather than opening a
	// page that would refuse the connection.
	pr.admin.SetVisible(state.On())
	pr.admin.SetSensitive(state == printerapp.StateReady)
	pr.admin.SetTooltipText(pageview.PrinterAdminButtonTooltip(state))
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
		unverified := facts.UnitPresent && printerapp.InstalledBeforeVerification(pr.app)
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
			pr.unverified = unverified

			if err != nil {
				// The error names commands and unit files; it is logged,
				// and the toast says what actually happened.
				log.Printf("views: printer application %s toggle to %v failed: %v", pr.app.Family.ID, enabled, err)
				pr.toggle.set(!enabled)
				uh.showPrinterAppState(pr, pr.state)
				uh.toastAdder.ShowErrorToast(pageview.PrinterAppFailureToast(enabled, pr.title, err))
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

// buildPrinterUSBRow adds the picker for the USB printer passed through to
// one family, directly under the family's row. It lists "No USB printer"
// until the saved choice and the connected printers have been read off the
// main thread, and it re-reads the connected printers whenever the pointer
// or keyboard focus reaches it, so a printer plugged in after the page was
// built is listed by the time the person opens the picker. There is no
// timer and no hotplug watch.
func (uh *UserHome) buildPrinterUSBRow(group *adw.PreferencesGroup, pr *printerRow) {
	pr.usb = adw.NewComboRow()
	pr.usb.SetTitle(pageview.PrinterUSBRowTitle())
	pr.usb.SetSubtitle(pageview.PrinterUSBRowSubtitle())
	// Four pickers share one title; the description says which family each
	// belongs to.
	pr.usb.UpdateProperty(gtk.AccessiblePropertyDescriptionValue, pr.title, -1)
	pr.usb.SetSensitive(false)
	uh.showPrinterUSBChoices(pr, nil)

	pr.usbNotify = func(_ gobject.Object, _ uintptr) { uh.onPrinterUSBSelected(pr) }
	pr.usb.ConnectNotify(&pr.usbNotify)
	pr.usbHover = func(w gtk.Widget, previous gtk.StateFlags) {
		const reached = gtk.StateFlagPrelightValue | gtk.StateFlagFocusedValue
		if w.GetStateFlags()&reached != 0 && previous&reached == 0 {
			uh.rescanPrinterUSB(pr)
		}
	}
	pr.usb.ConnectStateFlagsChanged(&pr.usbHover)
	group.Add(&pr.usb.Widget)
}

// loadPrinterUSBChoices reads every family's saved printer and the connected
// printers once, off the main thread, and fills in the pickers. A failed read
// leaves the pickers insensitive rather than offering a choice that could
// overwrite one it could not see.
func (uh *UserHome) loadPrinterUSBChoices(rows []*printerRow) {
	ctx, cancel := context.WithTimeout(context.Background(), printerProbeTimeout)
	defer cancel()
	saved, err := printerapp.LoadUSBChoices(ctx)
	devices, scanErr := printerapp.ScanUSBDevices()
	if scanErr != nil {
		log.Printf("views: scanning USB printers failed: %v", scanErr)
	}

	sgtk.RunOnMainThread(func() {
		if err != nil {
			if !errors.Is(err, printerapp.ErrUSBSchemaMissing) {
				log.Printf("views: reading USB printer choices failed: %v", err)
			}
			for _, pr := range rows {
				pr.usb.SetSubtitle(pageview.PrinterUSBUnavailableSubtitle())
			}
			return
		}
		for _, pr := range rows {
			// Select the saved choice before the picker accepts input: any
			// notification after that reads as a person's choice.
			pr.usbSaved = saved[pr.app.Family.ID]
			uh.showPrinterUSBChoices(pr, devices)
			pr.usbReady = true
			pr.usb.SetSensitive(true)
			// A toggle in flight owns the family row's subtitle; it renders
			// the USB status itself when it finishes.
			if pr.gate.TryStart() {
				uh.showPrinterAppState(pr, pr.state)
				pr.gate.Reset()
			}
		}
	})
}

// rescanPrinterUSB re-reads the connected printers for one picker, off the
// main thread. A failed scan keeps the list the picker already shows.
func (uh *UserHome) rescanPrinterUSB(pr *printerRow) {
	if !pr.usbReady || pr.usbScanning {
		return
	}
	pr.usbScanning = true
	go func() {
		devices, err := printerapp.ScanUSBDevices()
		sgtk.RunOnMainThread(func() {
			pr.usbScanning = false
			if err != nil {
				log.Printf("views: scanning USB printers failed: %v", err)
				return
			}
			// A choice being saved owns the picker until it finishes.
			if !pr.gate.TryStart() {
				return
			}
			uh.showPrinterUSBChoices(pr, devices)
			uh.showPrinterAppState(pr, pr.state)
			pr.gate.Reset()
		})
	}()
}

// showPrinterUSBChoices lists the saved choice and the scanned printers and
// selects the saved one. It replaces the model only when the list changed,
// so a rescan that finds nothing new leaves an open picker alone. Main
// thread only.
func (uh *UserHome) showPrinterUSBChoices(pr *printerRow, devices []printerapp.USBDevice) {
	choices := printerapp.USBChoicesFor(devices, pr.usbSaved)
	pr.usbStatus = printerapp.USBStatusOf(devices, pr.usbSaved)
	pr.usbSuppress = true
	defer func() { pr.usbSuppress = false }()
	if !sameUSBChoices(pr.usbChoices, choices) {
		pr.usb.SetModel(gtk.NewStringList(pageview.PrinterUSBChoiceLabels(choices)))
	}
	pr.usbChoices = choices
	pr.usb.SetSelected(uint32(printerapp.IndexOfUSBChoice(choices, pr.usbSaved)))
}

func sameUSBChoices(a, b []printerapp.USBChoice) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Identity != b[i].Identity || pageview.PrinterUSBChoiceLabel(a[i]) != pageview.PrinterUSBChoiceLabel(b[i]) {
			return false
		}
	}
	return true
}

// onPrinterUSBSelected saves a person's choice, off the main thread, and
// restarts the family if it is on so its unit passes the new printer through
// (or none). internal/printerapp refuses a printer another family already
// has, and both the save and the restart honour dry-run. A refusal, a failure
// and a preview all put the picker back on the saved choice.
func (uh *UserHome) onPrinterUSBSelected(pr *printerRow) {
	if pr.usbSuppress || !pr.usbReady {
		return
	}
	pos := pr.usb.GetSelected()
	if pos == gtk.INVALID_LIST_POSITION || int(pos) >= len(pr.usbChoices) {
		return
	}
	choice := pr.usbChoices[pos]
	if choice.Identity == pr.usbSaved {
		return
	}
	if !pr.gate.TryStart() {
		// The family is turning on or off; its result decides the unit.
		pr.usbSuppress = true
		pr.usb.SetSelected(uint32(printerapp.IndexOfUSBChoice(pr.usbChoices, pr.usbSaved)))
		pr.usbSuppress = false
		return
	}
	label := pageview.PrinterUSBChoiceLabel(choice)
	pr.usb.SetSensitive(false)
	pr.toggle.widget.SetSensitive(false)
	dryRun := dryrun.Enabled()

	go func() {
		defer pr.gate.Reset()

		ctx, cancel := context.WithTimeout(context.Background(), printerActionTimeout)
		defer cancel()

		saveErr := printerapp.SaveUSBChoice(ctx, pr.app.Family.ID, choice.Identity)
		var restartErr error
		restarted := false
		state, haveState := printerapp.State(0), false
		if saveErr == nil && printerapp.Observe(pr.app, true).UnitPresent {
			restartErr = printerapp.Refresh(ctx, pr.app)
			restarted = restartErr == nil
			if restarted && !dryRun {
				if _, err := printerapp.WaitSettled(ctx, pr.app, printerSettleWait); err != nil {
					log.Printf("views: printer application %s readiness unknown: %v", pr.app.Family.ID, err)
				}
				state, haveState = printerapp.ProbeDiagnostics(ctx, pr.app, true), true
			}
		}
		devices, scanErr := printerapp.ScanUSBDevices()
		if scanErr != nil {
			log.Printf("views: scanning USB printers failed: %v", scanErr)
		}

		sgtk.RunOnMainThread(func() {
			// Put the picker on the saved choice before it accepts input
			// again, so re-enabling it is never read as another choice.
			if saveErr == nil && !dryRun {
				pr.usbSaved = choice.Identity
			}
			uh.showPrinterUSBChoices(pr, devices)
			pr.usb.SetSensitive(true)
			pr.toggle.widget.SetSensitive(true)
			if haveState {
				uh.showPrinterAppState(pr, state)
			} else {
				uh.showPrinterAppState(pr, pr.state)
			}

			switch {
			case saveErr != nil:
				log.Printf("views: saving USB printer for %s failed: %v", pr.app.Family.ID, saveErr)
				uh.toastAdder.ShowErrorToast(pageview.PrinterUSBFailureToast(pr.title, saveErr))
			case restartErr != nil:
				log.Printf("views: restarting %s for its USB printer failed: %v", pr.app.Family.ID, restartErr)
				uh.toastAdder.ShowErrorToast(pageview.PrinterUSBRestartFailedToast(pr.title))
			default:
				uh.toastAdder.ShowToast(pageview.PrinterUSBChosenToast(dryRun, pr.title, label, restarted))
			}
		})
	}()
}
