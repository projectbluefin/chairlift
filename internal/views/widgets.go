package views

import (
	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/projectbluefin/chairlift/internal/views/signalroute"
)

// buttonRoute shares one ::clicked callback across every button of a list
// that is rebuilt on refresh, so reloading the list allocates no new puregotk
// trampoline (see internal/views/signalroute). It must live in a long-lived
// struct field and never be copied: its clicked field's address is the
// callback identity puregotk caches.
type buttonRoute struct {
	actions signalroute.Table[gtk.Button]
	clicked func(gtk.Button)
}

// connect binds action to button and connects the shared callback.
func (r *buttonRoute) connect(button *gtk.Button, action func(gtk.Button)) {
	if r.clicked == nil {
		r.clicked = func(emitter gtk.Button) { r.actions.Dispatch(emitter.GoPointer(), emitter) }
	}
	r.actions.Bind(button.GoPointer(), action)
	button.ConnectClicked(&r.clicked)
}

// forget drops one button's action when its row is removed on its own.
func (r *buttonRoute) forget(button *gtk.Button) {
	r.actions.Unbind(button.GoPointer())
}

// clear drops every action when the list is rebuilt.
func (r *buttonRoute) clear() {
	r.actions.Clear()
}

// dialogRoute shares one ::response callback across every confirmation
// dialog a page presents; each dialog's action runs once and is forgotten.
type dialogRoute struct {
	actions  signalroute.Table[string]
	response func(adw.AlertDialog, string)
}

// connect binds action to dialog's single response.
func (r *dialogRoute) connect(dialog *adw.AlertDialog, action func(response string)) {
	if r.response == nil {
		r.response = func(emitter adw.AlertDialog, response string) {
			r.actions.DispatchOnce(emitter.GoPointer(), response)
		}
	}
	r.actions.Bind(dialog.GoPointer(), action)
	dialog.ConnectResponse(&r.response)
}

func newIconButton(icon, label string) *gtk.Button {
	button := gtk.NewButtonFromIconName(icon)
	button.SetTooltipText(label)
	button.UpdateProperty(gtk.AccessiblePropertyLabelValue, label, -1)
	return button
}

func SetAccessibleLabel(widget interface {
	UpdateProperty(gtk.AccessibleProperty, ...interface{})
}, label string) {
	widget.UpdateProperty(gtk.AccessiblePropertyLabelValue, label, -1)
}

func newActivitySpinner() *gtk.Spinner {
	spinner := gtk.NewSpinner()
	spinner.SetValign(gtk.AlignCenterValue)
	spinner.SetVisible(false)
	SetAccessibleLabel(spinner, "Working…")
	return spinner
}

func setActivitySpinner(spinner *gtk.Spinner, busy bool) {
	if spinner != nil {
		spinner.SetVisible(busy)
		spinner.SetSpinning(busy)
	}
}
