package views

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/shellenv"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/trustmsg"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// bundleItemRow tracks one collection row in the App collections group.
type bundleItemRow struct {
	bundle      homebrew.Bundle
	memberPaths []string
	total       int
	row         *adw.ExpanderRow
	choice      *gtk.CheckButton
}

// collectionsAvailable reports whether the App collections group is available
// (brew_bundles_group enabled and its Homebrew capability floor met).
func (uh *UserHome) collectionsAvailable() bool {
	return uh.groupEnabled("applications_page", "brew_bundles_group")
}

// refreshBundleStatuses observes, off the GTK thread, whether each collection
// is already installed. If installed, the checkbox is checked, insensitive,
// and the subtitle indicates installed status. If not installed, it restores
// the baseline title and subtitle.
func (uh *UserHome) refreshBundleStatuses() {
	if uh == nil || len(uh.bundleItems) == 0 {
		return
	}

	type itemCheck struct {
		item  *bundleItemRow
		paths []string
	}
	checks := make([]itemCheck, 0, len(uh.bundleItems))
	for _, item := range uh.bundleItems {
		paths := item.memberPaths
		if len(paths) == 0 {
			paths = []string{item.bundle.Path}
		}
		checks = append(checks, itemCheck{item: item, paths: paths})
	}

	generation := uh.bundleStatusRefresh.Begin()

	go func() {
		for _, c := range checks {
			if !uh.bundleStatusRefresh.IsCurrent(generation) {
				return
			}
			statuses := make([]homebrew.BundleStatus, 0, len(c.paths))
			for _, path := range c.paths {
				status, err := homebrew.BundleCheck(path)
				if err != nil {
					log.Printf("Could not check whether app collection %q is installed: %v", path, err)
				}
				statuses = append(statuses, status)
			}
			installed, known := bundleview.AggregateInstalled(statuses)
			if !known {
				continue
			}
			sgtk.RunOnMainThread(func() {
				if !uh.bundleStatusRefresh.IsCurrent(generation) {
					return
				}
				desc := bundleview.DescribeInstalled(c.item.bundle.Name, c.item.bundle.Description, c.item.total, installed)
				c.item.row.SetSubtitle(desc.Subtitle)
				if installed {
					c.item.choice.SetActive(true)
					c.item.choice.SetSensitive(false)
				} else {
					c.item.choice.SetSensitive(true)
				}
			})
		}
	}()
}

// runSelectedBundlesInstall installs all checked, uninstalled bundles in one action.
func (uh *UserHome) runSelectedBundlesInstall() {
	if !uh.bundleInstallGate.TryStart() {
		return
	}

	var toInstall []*bundleItemRow
	for _, item := range uh.bundleItems {
		if item.choice.GetActive() && item.choice.GetSensitive() {
			toInstall = append(toInstall, item)
		}
	}

	if len(toInstall) == 0 {
		uh.bundleInstallGate.Reset()
		if uh.toastAdder != nil {
			uh.toastAdder.ShowToast("No collections selected to install.")
		}
		return
	}

	uh.setBundleInstallProgress(true, 0, len(toInstall), "")

	go func() {
		var failedNames []string
		var hadTrustErr bool
		var lastTrustErrName string

		for idx, item := range toInstall {
			b := item.bundle
			collection := bundleview.Describe(b.Name, b.Description, item.total)
			sgtk.RunOnMainThread(func() {
				uh.setBundleInstallProgress(true, idx, len(toInstall), collection.Title)
			})
			var itemFailed bool

			// Install primary and any configured member Brewfiles
			paths := item.memberPaths
			if len(paths) == 0 {
				paths = []string{b.Path}
			}
			for _, p := range paths {
				if err := homebrew.BundleInstall(p); err != nil {
					log.Printf("Error installing app collection %q (%s): %v", b.Name, p, err)
					var trustErr *homebrew.UntrustedTapError
					if errors.As(err, &trustErr) {
						hadTrustErr = true
						lastTrustErrName = collection.Title
					}
					itemFailed = true
					break
				}
			}
			if itemFailed {
				failedNames = append(failedNames, collection.Title)
				continue
			}

			// If Bluefin CLI was selected, enable shell bling and prompt
			if b.Name == "cli" {
				if err := shellenv.EnableBling(); err != nil {
					log.Printf("Error enabling shell bling: %v", err)
				}
			}
		}

		sgtk.RunOnMainThread(func() {
			defer uh.bundleInstallGate.Reset()
			uh.setBundleInstallProgress(false, len(toInstall), len(toInstall), "")
			if dryrun.Enabled() {
				if uh.toastAdder != nil {
					uh.toastAdder.ShowToast("[DRY-RUN] Preview: selected collections would be installed — no changes made")
				}
				return
			}

			uh.homebrewInventoryChanged()

			if uh.toastAdder != nil {
				if len(failedNames) == 0 {
					uh.toastAdder.ShowToast("Selected collections installed successfully.")
				} else if hadTrustErr {
					uh.toastAdder.ShowErrorToast(trustmsg.BundleMessage(lastTrustErrName))
				} else {
					uh.toastAdder.ShowErrorToast(fmt.Sprintf("Couldn't install all collections (%s). Try again.", strings.Join(failedNames, ", ")))
				}
			}
		})
	}()
}

func (uh *UserHome) setBundleInstallProgress(busy bool, current, total int, currentName string) {
	if uh.bundleInstallButton != nil {
		uh.bundleInstallButton.SetSensitive(!busy)
	}
	if uh.bundleInstallBar != nil {
		if busy {
			text := fmt.Sprintf("%d of %d collections", current+1, total)
			if currentName != "" {
				text = fmt.Sprintf("%s (%d of %d)", currentName, current+1, total)
			}
			uh.bundleInstallBar.SetText(text)
			fraction := float64(current) / float64(total)
			if fraction <= 0 {
				uh.appInstallProgress.start(uh.bundleInstallBar)
			} else {
				uh.appInstallProgress.setDeterminate(uh.bundleInstallBar, fraction)
			}
		} else {
			uh.appInstallProgress.stop(uh.bundleInstallBar)
		}
	}
}
