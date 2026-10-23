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

// checkRoute shares one ::toggled callback across every checkbox of a list,
// so a list of a hundred entries allocates one puregotk trampoline instead
// of a hundred (see internal/views/signalroute). It must live in a
// long-lived struct field and never be copied: its toggled field's address
// is the callback identity puregotk caches.
//
// A checkbox whose state changes programmatically — Select All, or the
// refresh that marks an installed entry — emits ::toggled too, so the
// action behind it must be idempotent: it reads the widget's own state
// rather than counting changes.
type checkRoute struct {
	actions signalroute.Table[gtk.CheckButton]
	toggled func(gtk.CheckButton)
}

// connect binds action to check and connects the shared callback.
func (r *checkRoute) connect(check *gtk.CheckButton, action func(gtk.CheckButton)) {
	if r.toggled == nil {
		r.toggled = func(emitter gtk.CheckButton) { r.actions.Dispatch(emitter.GoPointer(), emitter) }
	}
	r.actions.Bind(check.GoPointer(), action)
	check.ConnectToggled(&r.toggled)
}

// clear drops every action when the list is rebuilt.
func (r *checkRoute) clear() {
	r.actions.Clear()
}

func newIconButton(icon, label string) *gtk.Button {
	button := gtk.NewButtonFromIconName(icon)
	// Rows do not flatten their suffix buttons; a header bar does, so
	// flat is right in both places.
	button.AddCssClass("flat")
	button.SetTooltipText(label)
	button.UpdateProperty(gtk.AccessiblePropertyLabelValue, label, -1)
	return button
}

// SetAccessibleLabel sets a widget's accessible name.
func SetAccessibleLabel(widget interface {
	UpdateProperty(gtk.AccessibleProperty, ...interface{})
}, label string) {
	widget.UpdateProperty(gtk.AccessiblePropertyLabelValue, label, -1)
}

// setNamedButtonLabel gives a button an accessible name of its own.
//
// GtkButton names itself from its label child through a LABELLED_BY
// relation, which outranks the LABEL property, so six buttons that all
// read "Select All" would be indistinguishable to a screen reader. The
// relation is dropped and the name set directly; the visible label stays
// the leading words of the name, which is what the "label in name" rule
// asks for.
func setNamedButtonLabel(button *gtk.Button, label, name string) {
	button.SetLabel(label)
	button.ResetRelation(gtk.AccessibleRelationLabelledByValue)
	SetAccessibleLabel(button, name)
}

// describeFor names what a repeated row button acts on. Every pending item
// shows the same "Update", every untrusted package the same "Trust"; the
// description lets a screen reader say which one, and unlike the button's
// name it survives the label changing to a progress word.
func describeFor(button *gtk.Button, subject string) {
	button.UpdateProperty(gtk.AccessiblePropertyDescriptionValue, subject, -1)
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
