package views

import "codeberg.org/puregotk/puregotk/v4/gtk"

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
