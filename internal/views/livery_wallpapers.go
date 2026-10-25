package views

import (
	"errors"
	"log"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
	sgtk "github.com/frostyard/snowkit/gtk"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/actionstate"
	"github.com/projectbluefin/chairlift/internal/views/bundleview"
	"github.com/projectbluefin/chairlift/internal/views/trustmsg"
)

// buildWallpapersGroup discovers the external collection without constructing
// an unavailable-tool card. Other Livery controls have independent lifetimes.
func (uh *UserHome) buildWallpapersGroup(page *adw.PreferencesPage) {
	var paths []string
	if cfg := uh.config.GetGroupConfig("applications_page", "brew_bundles_group"); cfg != nil {
		paths = append(paths, cfg.BundlesPaths...)
	}
	go func() {
		bundles, err := homebrew.AvailableBundles(paths)
		if err != nil {
			log.Printf("wallpapers: discovery: %v", err)
		}
		for _, bundle := range bundles {
			if !bundleview.WallpaperCollection(bundle.Name) {
				continue
			}
			items, err := homebrew.BundleContents(bundle.Path)
			if err != nil {
				log.Printf("wallpapers: contents: %v", err)
				continue
			}
			sgtk.RunOnMainThread(func() {
				if uh.wallpapersGroup == nil {
					uh.wallpapersGroup = adw.NewPreferencesGroup()
					uh.wallpapersGroup.SetTitle("Wallpapers")
					uh.wallpapersGroup.SetDescription("Curated wallpaper collections for your desktop.")
					page.Add(uh.wallpapersGroup)
				}
				group := uh.wallpapersGroup
				row := adw.NewExpanderRow()
				row.SetUseMarkup(false)
				row.SetTitle("Wallpaper Collections")
				row.SetSubtitle(bundle.Description)
				addContentsRows(row, items, bundle.ItemCount)
				button := gtk.NewButtonWithLabel("Install")
				button.SetValign(gtk.AlignCenterValue)
				button.SetSensitive(false)
				row.AddSuffix(&button.Widget)
				group.Add(&row.Widget)

				var generation actionstate.RefreshGate
				busy := false
				refresh := func() {
					if busy {
						return
					}
					ticket := generation.Begin()
					go func() {
						status, err := homebrew.BundleCheck(bundle.Path)
						if err != nil {
							log.Printf("wallpapers: check: %v", err)
							status = homebrew.BundleIndeterminate
						}
						sgtk.RunOnMainThread(func() {
							if !generation.IsCurrent(ticket) {
								return
							}
							subtitle, actionable := bundleview.WallpaperStatus(bundle.Description, status, err)
							row.SetSubtitle(subtitle)
							button.SetSensitive(actionable)
							if status == homebrew.BundleUpdateAvailable {
								button.SetLabel("Update")
							} else {
								button.SetLabel("Install")
							}
						})
					}()
				}
				previous := uh.wallpaperRefresh
				uh.wallpaperRefresh = func() {
					if previous != nil {
						previous()
					}
					refresh()
				}
				clicked := func(_ gtk.Button) {
					if !uh.bundleInstallGate.TryStart() {
						return
					}
					busy = true
					generation.Begin() // Invalidate an observation that predates this action.
					button.SetSensitive(false)
					go func() {
						err := homebrew.BundleInstall(bundle.Path)
						preview := dryrun.Enabled()
						decision := actionstate.BundleInstall(err == nil, preview)
						sgtk.RunOnMainThread(func() {
							uh.bundleInstallGate.Reset()
							busy = false
							button.SetSensitive(true)
							if err != nil {
								log.Printf("wallpapers: install: %v", err)
								var trustErr *homebrew.UntrustedTapError
								if errors.As(err, &trustErr) {
									uh.toastAdder.ShowErrorToast(trustmsg.BundleMessage("wallpaper collections"))
								} else {
									uh.toastAdder.ShowErrorToast("Couldn't install wallpaper collections. Try again.")
								}
								return
							}
							uh.toastAdder.ShowToast(actionmsg.BundleInstall(preview))
							if decision.Refresh {
								uh.homebrewInventoryChanged()
							}
						})
					}()
				}
				button.ConnectClicked(&clicked)
				refresh()
			})
		}
	}()
}
