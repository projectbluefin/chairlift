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
)

const appID = "io.projectbluefin.chairlift"

// optionSetup is the long name of the --setup option, and with it the key
// GLib uses for the option in the command-line dictionary it forwards to the
// primary instance.
const optionSetup = "setup"

var (
	gTypeApplication gobject.Type
	appRegistry      *gobj.InstanceRegistry
)

// Application wraps the Adwaita Application as a proper GObject subtype
type Application struct {
	adw.Application
	window         *window.Window
	setupRequested bool
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
			appClass.OverrideCommandLine(func(a *gio.Application, cl *gio.ApplicationCommandLine) int32 {
				ptr := reg.Get(a.GoPointer())
				if ptr == nil {
					log.Fatal("Application instance not found")
				}
				return (*Application)(ptr).onCommandLine(cl)
			})
		},
	})
}

// New creates a new ChairLift application
func New() *Application {
	// HandlesCommandLine, not None: a second `chairlift --setup` against a
	// running instance does not run this function at all, so a flag read
	// from that process's os.Args can never reach the process owning the
	// window. With this flag GLib forwards the parsed option dictionary to
	// the primary instance's command_line handler, which is what makes
	// --setup re-open the assistant at any time as README documents.
	obj := gobject.NewObject(gTypeApplication, "application_id", appID, "flags", gio.GApplicationHandlesCommandLineValue, uintptr(0))
	if obj == nil {
		log.Fatal("Failed to create application")
	}

	app := (*Application)(appRegistry.Get(obj.GoPointer()))

	// Check for --dry-run before GTK processes args. Dry-run stays a local
	// read: it is a property of this process's integrations, and a remote
	// invocation must not be able to flip the mode of a running window.
	// --setup is read from the option dictionary in onCommandLine instead,
	// so it works for a remote invocation too.
	for _, arg := range os.Args[1:] {
		if arg == "--dry-run" || arg == "-d" {
			log.Println("Running in dry-run mode")
			// One process-wide flag; every integration package reads
			// dryrun.Enabled() directly, so a new integration cannot
			// silently fall out of dry-run mode by missing a setter here.
			dryrun.Set(true)
			// Lets the screenshot walkthrough render the Bluefin-family
			// rows on a host that is not a Bluefin system. This is a no-op
			// in every ordinary build; see imageinfo_override.go.
			applyImageInfoOverride()
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

// onCommandLine runs on the primary instance for every invocation, local or
// remote, and owns the --setup decision.
//
// A remote invocation reaches here with the option dictionary GLib forwarded
// from the other process; the primary's own os.Args says nothing about it.
// When the window already exists the assistant is presented directly, because
// activation reuses the window and deliberately does not re-run the first-run
// check.
func (a *Application) onCommandLine(cl *gio.ApplicationCommandLine) int32 {
	setup := commandLineRequestsSetup(cl)
	running := a.window != nil

	// Consumed by onActivate's first-run check when this invocation is the
	// one that creates the window, so the explicit request skips the
	// disposition probe instead of racing it to Present.
	a.setupRequested = setup

	a.Activate()

	if setup && running && a.window != nil {
		a.window.PresentFirstRun()
	}
	return 0
}

// commandLineRequestsSetup reports whether the invocation carried --setup/-s.
//
// GLib keys the dictionary on the long option name, so the short form needs
// no separate lookup.
func commandLineRequestsSetup(cl *gio.ApplicationCommandLine) bool {
	if cl == nil {
		return false
	}
	opts := cl.GetOptionsDict()
	if opts == nil {
		return false
	}
	return opts.Contains(optionSetup)
}

// onActivate is called when the application is activated
func (a *Application) onActivate() {
	activateStart := time.Now()
	log.Println("ChairLift activated")

	// Guard: reuse existing window if already created. The first-run check
	// deliberately does not re-run here — re-activating the application
	// (clicking its icon) would otherwise rebuild the assistant's model and
	// send a user who is mid-wizard back to the welcome screen.
	if a.window != nil {
		a.window.Present()
		return
	}

	// Create and present the main window
	win := window.New(a.Application)
	a.window = win
	a.AddWindow(&win.Window)
	a.registerQuitAction()
	a.setupKeyboardShortcuts(win.NavigationItems())
	win.Present()
	win.CheckFirstRun(a.setupRequested)
	log.Printf("app: window presented in %s (since activate)", time.Since(activateStart))
}

// registerQuitAction registers the app.quit action that navigation's
// <Primary>q shortcut targets. GTK registers no quit action for an
// application by default, so without this the accelerator
// setupKeyboardShortcuts installs resolves to nothing and Ctrl+Q is
// advertised in the shortcuts dialog while doing nothing.
func (a *Application) registerQuitAction() {
	quitAction := gio.NewSimpleAction("quit", nil)
	quitActivateCb := func(action gio.SimpleAction, param uintptr) {
		a.Quit()
	}
	quitAction.ConnectActivate(&quitActivateCb)
	a.AddAction(quitAction)
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
	a.AddMainOption(
		optionSetup,
		's',
		glib.GOptionFlagNoneValue,
		glib.GOptionArgNoneValue,
		"Display the first-run onboarding assistant.",
		"",
	)
}
