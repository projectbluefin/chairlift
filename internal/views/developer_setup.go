package views

import (
	"fmt"

	"codeberg.org/puregotk/puregotk/v4/adw"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/capability"
	"github.com/projectbluefin/chairlift/internal/developerfeeds"
	"github.com/projectbluefin/chairlift/internal/devsetup"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

func addSetupContents(row *adw.ExpanderRow, title, subtitle string) *adw.ActionRow {
	entry := adw.NewActionRow()
	entry.SetUseMarkup(false)
	entry.SetTitle(title)
	entry.SetSubtitle(subtitle)
	row.AddRow(&entry.Widget)
	return entry
}

// Both direct and implicit promotions render the confirmed access transition
// immediately. The shared gate and checklist remain busy for subsequent tools.
func (uh *UserHome) developerSetupReport(enabled bool) func(devsetup.Stage, devsetup.Status) {
	return func(stage devsetup.Stage, status devsetup.Status) {
		uh.developerSetupActivity.report(stage, status)
		if stage == devsetup.Feeds && status == devsetup.Complete {
			if path, err := developerfeeds.OPMLPath(); err == nil {
				sgtk.RunOnMainThread(func() {
					if !uh.developerSetupActivity.progress.disposed {
						uh.developerSetupActivity.rows[stage].SetSubtitle("Complete — Saved to " + path + ". Import this OPML file in your feed reader.")
					}
				})
			}
		}
		if stage != devsetup.Access || status != devsetup.Complete {
			return
		}
		sgtk.RunOnMainThread(func() {
			uh.developerEnabled = enabled
			if uh.developerToggle != nil {
				uh.developerToggle.set(enabled)
			}
			if uh.developerRow != nil {
				uh.developerRow.SetSubtitle(pageview.DeveloperResultSubtitle(enabled))
			}
		})
	}
}

func (uh *UserHome) developerBundlePaths() []string {
	if group := uh.config.GetGroupConfig("applications_page", "brew_bundles_group"); group != nil {
		return append([]string(nil), group.BundlesPaths...)
	}
	return nil
}

func developerAppsBundle(paths []string) (homebrew.Bundle, error) {
	bundles, err := homebrew.AvailableBundles(paths)
	if err != nil {
		return homebrew.Bundle{}, err
	}
	for _, bundle := range bundles {
		if bundle.Name == bundleview.DeveloperAppsCollection {
			return bundle, nil
		}
	}
	return homebrew.Bundle{}, fmt.Errorf("developer apps collection %s is not available", bundleview.DeveloperAppsCollection)
}

func (uh *UserHome) addDeveloperContents(row *adw.ExpanderRow) {
	addSetupContents(row, "Developer access", "Administrator-approved developer groups. A new login may be required.")
	if uh.capabilities[capability.Homebrew] {
		addContentsRows(row, []homebrew.BundleItem{{Name: devsetup.DevcontainerPackage, Kind: homebrew.BundleItemBrew}}, 1)
	}
	group := uh.config.GetGroupConfig("features_page", "dx_group")
	if group == nil {
		return
	}
	if group.InstallPulp {
		addContentsRows(row, []homebrew.BundleItem{{Name: developerfeeds.PulpID, Kind: homebrew.BundleItemFlatpak}}, 1)
	}
	if group.StageFeeds {
		description := "OPML feed catalog for manual import after setup; no subscriptions are imported automatically."
		if path, err := developerfeeds.OPMLPath(); err == nil {
			description = path + "\n" + description
		}
		addSetupContents(row, developerfeeds.OPMLFileName, description)
	}
	if !group.InstallDevApps || !uh.collectionsAvailable() {
		return
	}
	collection := adw.NewExpanderRow()
	collection.SetUseMarkup(false)
	collection.SetTitle(bundleview.DeveloperAppsCollection)
	collection.SetSubtitle("Checking collection contents…")
	row.AddRow(&collection.Widget)
	paths := uh.developerBundlePaths()
	go func() {
		bundle, err := developerAppsBundle(paths)
		var items []homebrew.BundleItem
		if err == nil {
			items, err = homebrew.BundleContents(bundle.Path)
		}
		sgtk.RunOnMainThread(func() {
			if err != nil {
				collection.SetSubtitle("Couldn't read this collection. No package list is assumed.")
				return
			}
			presentation := bundleview.Describe(bundle.Name, bundle.Description, bundle.ItemCount)
			collection.SetTitle(presentation.Title)
			collection.SetSubtitle(presentation.Subtitle)
			addContentsRows(collection, items, bundle.ItemCount)
		})
	}()
}

// developerPostTasks snapshots config on the GTK thread. These run in the
// original worker, so its gate and activity stay busy until all opt-ins settle.
func (uh *UserHome) developerPostTasks(enabled, succeeded bool) []devsetup.Task {
	options := devsetup.PostOptions{Homebrew: uh.capabilities[capability.Homebrew], Collections: uh.collectionsAvailable()}
	if group := uh.config.GetGroupConfig("features_page", "dx_group"); group != nil {
		options.InstallPulp, options.StageFeeds, options.InstallApps = group.InstallPulp, group.StageFeeds, group.InstallDevApps
	}
	var tasks []devsetup.Task
	for _, stage := range devsetup.PostStages(dryrun.Enabled(), enabled && succeeded, options) {
		task := devsetup.Task{Stage: stage, Optional: true}
		switch stage {
		case devsetup.Devcontainer:
			task.Run = func() error { return homebrew.Install(devsetup.DevcontainerPackage, false) }
		case devsetup.Pulp:
			task.Run = developerfeeds.Provision
		case devsetup.Feeds:
			task.Run = developerfeeds.StageOPML
		case devsetup.Apps:
			paths := uh.developerBundlePaths()
			task.Run = func() error {
				bundle, err := developerAppsBundle(paths)
				if err != nil {
					return err
				}
				return homebrew.BundleInstall(bundle.Path)
			}
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func setupStages(access bool, tasks []devsetup.Task) []devsetup.Stage {
	stages := make([]devsetup.Stage, 0, len(tasks)+1)
	if access {
		stages = append(stages, devsetup.Access)
	}
	for _, task := range tasks {
		stages = append(stages, task.Stage)
	}
	return stages
}
