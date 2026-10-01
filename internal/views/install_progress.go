package views

import (
	"codeberg.org/puregotk/puregotk/v4/glib"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// installProgress owns one reusable GTK-thread timer for all active installs.
// Homebrew exposes no fraction: pulse honestly while the command is running,
// without consuming output or enqueueing a callback for every printed line.
type installProgress struct {
	active   map[*gtk.ProgressBar]struct{}
	pulse    glib.SourceFunc
	timer    uint32
	disposed bool
}

func newInstallProgress(text string) *gtk.ProgressBar {
	bar := gtk.NewProgressBar()
	bar.SetShowText(true)
	bar.SetText(text)
	bar.SetSizeRequest(180, -1)
	bar.SetVisible(false)
	return bar
}

func (p *installProgress) start(bar *gtk.ProgressBar) {
	if p.disposed {
		return
	}
	if p.active == nil {
		p.active = make(map[*gtk.ProgressBar]struct{})
	}
	p.active[bar] = struct{}{}
	bar.SetVisible(true)
	bar.Pulse()
	if p.timer != 0 {
		return
	}
	if p.pulse == nil {
		p.pulse = func(uintptr) bool {
			for bar := range p.active {
				bar.Pulse()
			}
			return true
		}
	}
	p.timer = glib.TimeoutAdd(100, &p.pulse, 0)
}

func (p *installProgress) stop(bar *gtk.ProgressBar) {
	if p.disposed {
		return
	}
	delete(p.active, bar)
	bar.SetVisible(false)
	bar.SetFraction(0)
	if len(p.active) == 0 && p.timer != 0 {
		glib.SourceRemove(p.timer)
		p.timer = 0
	}
}

func (p *installProgress) dispose() {
	p.disposed = true
	if p.timer != 0 {
		glib.SourceRemove(p.timer)
		p.timer = 0
	}
	p.active = nil
}
