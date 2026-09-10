// Package app provides the main ChairLift application
package app

import (
	"log"
	"os"
	"time"
	"unsafe"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/imageinfo"
	"github.com/projectbluefin/chairlift/internal/navigation"
	"github.com/projectbluefin/chairlift/internal/window"

	"github.com/frostyard/snowkit/gobj"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gio"
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gobject"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

const appID = "io.projectbluefin.chairlift"

var (
	gTypeApplication gobject.Type
	appRegistry      *gobj.InstanceRegistry
)

// Application wraps the Adwaita Application as a proper GObject subtype
type Application struct {
	adw.Application
	window *window.Window
	dryRun bool
}

func init() {
	gTypeApplication, appRegistry = gobj.RegisterType(gobj.TypeDef{
		ParentGLibType: adw.ApplicationGLibType,
		ClassName:      "ChairLiftApplication",
		ClassInit: func(tc *gobject.TypeClass, reg *gobj.InstanceRegistry) {
			objClass := (*gobject.ObjectClass)(unsafe.Pointer(tc))
			objClass.OverrideConstructed(func(o *gobject.Object) {
				parentObjClass := (*gobject.ObjectClass)(unsafe.Pointer(tc.PeekParent()))
				parentObjClass.GetConstructed()(o)

				var parent adw.Application
				o.Cast(&parent)

				app := &Application{Application: parent}
				reg.Pin(o, unsafe.Pointer(app))
			})

			appClass := (*gio.ApplicationClass)(unsafe.Pointer(tc))
			appClass.OverrideActivate(func(a *gio.Application) {
				ptr := reg.Get(a.GoPointer())
				if ptr == nil {
					log.Fatal("Application instance not found")
				}
				(*Application)(ptr).onActivate()
			})
		},
	})
}

// New creates a new ChairLift application
func New() *Application {
	obj := gobject.NewObject(gTypeApplication, "application_id", appID, "flags", gio.GApplicationFlagsNoneValue)
	if obj == nil {
		log.Fatal("Failed to create application")
	}

	app := (*Application)(appRegistry.Get(obj.GoPointer()))

	// Check for --dry-run flag before GTK processes args
	for _, arg := range os.Args[1:] {
		if arg == "--dry-run" || arg == "-d" {
			log.Println("Running in dry-run mode")
			app.dryRun = true
			// One process-wide flag; every integration package reads
			// dryrun.Enabled() directly, so a new integration cannot
			// silently fall out of dry-run mode by missing a setter here.
			dryrun.Set(true)
			// Lets the screenshot walkthrough render the Bluefin-family
			// rows on a host that is not a Bluefin system. This is a no-op
			// in every ordinary build; see imageinfo_override.go.
			applyImageInfoOverride()
			break
		}
	}

	// Apply the channel-table override, if the image or administrator
	// shipped one. The privileged helper loads the same file from the same
	// fixed paths, so both sides resolve identical switch targets. A broken
	// override is logged and the built-in table stays active: the failure
	// costs the release-channel row, not the whole application.
	if path, err := imageinfo.LoadSystemTable(); err != nil {
		log.Printf("channel table override ignored: %v", err)
	} else if path != "" {
		log.Printf("channel table loaded from %s", path)
	}

	// Register command line options
	app.registerOptions()

	return app
}

// onActivate is called when the application is activated
func (a *Application) onActivate() {
	activateStart := time.Now()
	log.Println("ChairLift activated")

	// Guard: reuse existing window if already created
	if a.window != nil {
		a.window.Present()
		return
	}

	// Create and present the main window
	win := window.New(a.Application)
	a.window = win
	a.AddWindow(&win.Window)
	a.setupKeyboardShortcuts(win.NavigationItems())
	win.Present()
	log.Printf("app: window presented in %s (since activate)", time.Since(activateStart))
}

// setupKeyboardShortcuts sets up application-wide keyboard shortcuts
func (a *Application) setupKeyboardShortcuts(items []navigation.Item) {
	for _, binding := range navigation.Bindings(items) {
		a.SetAccelsForAction(binding.Action, binding.Accelerators)
	}
}

// registerOptions registers command line options
func (a *Application) registerOptions() {
	a.AddMainOption(
		"dry-run",
		'd',
		glib.GOptionFlagNoneValue,
		glib.GOptionArgNoneValue,
		"Don't make any changes to the system.",
		"",
	)
}

// GetGtkApplication returns the underlying GTK Application
func (a *Application) GetGtkApplication() *gtk.Application {
	return &a.Application.Application
}
