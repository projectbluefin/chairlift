package views

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/maintenanceexec"
	"github.com/projectbluefin/chairlift/internal/updateproviders"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/cleanupview"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// The Maintenance page holds three things, in descending order of how often
// a person needs them and ascending order of how much they cost:
//
//  1. One routine cleanup action, composing internal/updateproviders' typed
//     cleanup inventory. It used to be three separate buttons plus a "Coming
//     soon" placeholder, which asked the user to know which package manager
//     owned their wasted disk space.
//  2. Whatever maintenance the administrator configured, labelled as
//     theirs. ChairLift knows nothing about these scripts, so they are never
//     folded into the cleanup above.
//  3. Recovery. Powerwash and Factory Reset are not maintenance, they are
//     what you reach for when something has already gone wrong, and they are
//     separated visually and by an opt-in default (see reset.go).

// buildMaintenancePage builds the Maintenance page content
func (uh *UserHome) buildMaintenancePage() {
	page := uh.maintenancePrefsPage
	if page == nil {
		return
	}

	if uh.config.IsGroupEnabled("maintenance_page", "maintenance_freespace_group") {
		uh.buildFreeSpaceGroup(page)
	}

	if uh.config.IsGroupEnabled("maintenance_page", "maintenance_cleanup_group") {
		uh.buildConfiguredTasksGroup(page)
	}

	// Recovery detail entry. The detail view houses rollback and reset.
	if uh.recoveryProvidersAvailable() {
		recoveryGroup := adw.NewPreferencesGroup()
		recoveryGroup.SetTitle("Recovery")
		recoveryRow := adw.NewActionRow()
		recoveryRow.SetTitle("Recovery")
		recoveryRow.SetSubtitle(pageview.RecoveryEntrySubtitle())
		recoveryRow.SetActivatable(true)
		icon := gtk.NewImageFromIconName("pan-end-symbolic")
		recoveryRow.AddSuffix(&icon.Widget)
		recoveryActivatedCb := func(row adw.ActionRow) {
			if uh.openRecoveryDetail != nil {
				uh.openRecoveryDetail()
			}
		}
		recoveryRow.ConnectActivated(&recoveryActivatedCb)
		recoveryGroup.Add(&recoveryRow.Widget)
		page.Add(recoveryGroup)
	}

	// Reset group (Powerwash / Factory Reset).
	if uh.config.IsGroupEnabled("maintenance_page", "reset_group") {
		uh.buildResetGroup(uh.recoveryPrefsPage)
	}
}

// buildFreeSpaceGroup builds the page's single routine cleanup action.
func (uh *UserHome) buildFreeSpaceGroup(page *adw.PreferencesPage) {
	group := adw.NewPreferencesGroup()
	group.SetTitle(cleanupview.GroupTitle)
	group.SetDescription(cleanupview.GroupDescription)

	row := adw.NewActionRow()
	row.SetTitle(cleanupview.RowTitle)
	row.SetSubtitle(cleanupview.RowSubtitle)

	icon := gtk.NewImageFromIconName("user-trash-symbolic")
	row.AddPrefix(&icon.Widget)

	button := gtk.NewButtonWithLabel(cleanupview.ButtonLabel)
	button.SetValign(gtk.AlignCenterValue)
	button.AddCssClass("suggested-action")

	// Connected once, at build time: this row is never rebuilt, so the
	// callback table keeps exactly one slot for it.
	clickedCb := func(gtk.Button) {
		uh.onFreeUpSpaceClicked(button, row)
	}
	button.ConnectClicked(&clickedCb)

	row.AddSuffix(&button.Widget)
	group.Add(&row.Widget)
	page.Add(group)
}

// onFreeUpSpaceClicked runs every cleanup provider off the main thread and
// reports what each one actually did.
//
// Free space is read before and after the run. That reading is the only
// source of a reclaimed-bytes figure: neither provider reports its own
// total in a form worth trusting, so when either read fails, or the
// difference is small enough to be ordinary system noise, the result says
// the cleanup finished and names no number at all.
func (uh *UserHome) onFreeUpSpaceClicked(button *gtk.Button, row *adw.ActionRow) {
	button.SetSensitive(false)
	button.SetLabel(cleanupview.BusyLabel)

	cleanup := updateproviders.NewCleanup(uh.config)
	previewOnly := dryrun.Enabled()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		paths := cleanupview.CachePaths()
		before, beforeOK := cleanupview.FreeBytes(paths)
		results := cleanup.RunSteps(ctx)
		after, afterOK := cleanupview.FreeBytes(paths)

		for _, result := range results {
			log.Printf("views: free up space step=%s outcome=%s detail=%q",
				result.ID, result.Outcome, result.Detail)
		}

		outcome := cleanupview.Summarize(previewOnly, results, cleanupview.Space{
			Before:   before,
			After:    after,
			Measured: beforeOK && afterOK && !previewOnly,
		})

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			button.SetLabel(cleanupview.ButtonLabel)

			if outcome.Headline != "" {
				row.SetSubtitle(outcome.Headline)
			}
			if outcome.IsError {
				uh.toastAdder.ShowErrorToast(outcome.Toast)
				return
			}
			uh.toastAdder.ShowToast(outcome.Toast)
		})
	}()
}

// buildConfiguredTasksGroup builds the administrator-configured actions.
// ChairLift has no idea what these do, so the group says where they came
// from and each row says only whether it will ask for a password — never
// the command it runs.
func (uh *UserHome) buildConfiguredTasksGroup(page *adw.PreferencesPage) {
	groupCfg := uh.config.GetGroupConfig("maintenance_page", "maintenance_cleanup_group")
	if groupCfg == nil || len(groupCfg.Actions) == 0 {
		// An enabled group with no actions has nothing to say; showing its
		// heading would promise tasks that do not exist.
		return
	}

	group := adw.NewPreferencesGroup()
	group.SetTitle(cleanupview.ScriptsGroupTitle)
	group.SetDescription(cleanupview.ScriptsGroupDescription)

	for _, action := range groupCfg.Actions {
		row := adw.NewActionRow()
		row.SetTitle(action.Title)
		if action.Sudo {
			row.SetSubtitle(cleanupview.ScriptsAdminSubtitle)
			sudoIcon := gtk.NewImageFromIconName("dialog-password-symbolic")
			row.AddPrefix(&sudoIcon.Widget)
		}

		button := gtk.NewButtonWithLabel(cleanupview.ScriptsButtonLabel)
		button.SetValign(gtk.AlignCenterValue)
		button.AddCssClass("suggested-action")

		script := action.Script
		sudo := action.Sudo
		title := action.Title
		btn := button
		clickedCb := func(_ gtk.Button) {
			uh.runMaintenanceAction(title, script, sudo, btn)
		}
		button.ConnectClicked(&clickedCb)

		row.AddSuffix(&button.Widget)
		group.Add(&row.Widget)
	}

	page.Add(group)
}

// onBrewBundleDumpClicked exports the user's package list. The row that
// triggers it lives on the Applications page; only the handler sits here.
func (uh *UserHome) onBrewBundleDumpClicked() {
	go func() {
		homeDir, _ := os.UserHomeDir()
		path := homeDir + "/Brewfile"
		if err := homebrew.BundleDump(path, true); err != nil {
			log.Printf("Package list export failed: %v", err)
			sgtk.RunOnMainThread(func() {
				uh.toastAdder.ShowErrorToast("Could not export your package list")
			})
			return
		}
		sgtk.RunOnMainThread(func() {
			uh.toastAdder.ShowToast(actionmsg.BundleDump(dryrun.Enabled()))
		})
	}()
}

// runMaintenanceAction runs a maintenance action script
func (uh *UserHome) runMaintenanceAction(title, script string, sudo bool, button *gtk.Button) {
	log.Printf("Running action: %s (script: %s, sudo: %v)", title, script, sudo)

	decision := actionmsg.MaintenanceScript(dryrun.Enabled(), title)

	button.SetSensitive(false)
	button.SetLabel(cleanupview.ScriptsBusyLabel)

	go func() {
		var err error

		// One command value serves both branches, so the journalled argv is
		// the argv a real run executes rather than a separately formatted
		// preview string that could drift from it.
		command := pageview.MaintenanceCommand(script, sudo)
		wouldRun := append([]string{command.Name}, command.Args...)
		suppressed := journal.SuppressedNone
		if !decision.Execute {
			suppressed = journal.SuppressedDryRun
		}
		journal.Record(
			path.Base(script),
			map[string]string{"title": title, "sudo": strconv.FormatBool(sudo)},
			wouldRun,
			suppressed,
		)

		if decision.Execute {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			err = maintenanceexec.Run(ctx, command.Name, command.Args...)
		} else {
			log.Printf("[DRY-RUN] Would execute: %s", strings.Join(wouldRun, " "))
		}

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			button.SetLabel(cleanupview.ScriptsButtonLabel)

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("%s failed: %v", title, err))
				return
			}

			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
