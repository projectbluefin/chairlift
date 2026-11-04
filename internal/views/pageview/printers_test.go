package pageview

import (
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/printerapp"
)

// The description explains what turning a family on does without jargon.
func TestPrintersGroupDescriptionDoesNotContainJargon(t *testing.T) {
	d := PrintersGroupDescription()
	for _, jargon := range []string{"container", "Podman", "driverless", "system"} {
		if strings.Contains(d, jargon) {
			t.Errorf("description %q names %q", d, jargon)
		}
	}
}

// Every family gets a distinct title that names the family, so a person can
// match the row to the driver a printer's documentation names.
func TestPrinterFamilyRowNamesEveryFamilyDistinctly(t *testing.T) {
	seen := map[string]string{}
	for _, f := range printerapp.Families() {
		title := PrinterFamilyRow(f)
		if !strings.Contains(title, f.DisplayName) {
			t.Errorf("%s row title %q does not name the family", f.ID, title)
		}
		if prev, dup := seen[title]; dup {
			t.Errorf("families %s and %s share title %q", prev, f.ID, title)
		}
		seen[title] = f.ID
	}
	if got := PrinterFamilyRow(printerapp.Family{ID: "hplip", DisplayName: "HPLIP"}); !strings.HasPrefix(got, "HP Printers") {
		t.Errorf("HPLIP row = %q; a person looks for HP, not HPLIP", got)
	}
}

// Every state reads differently, and only Ready says running: a starting or
// failed family can never be mistaken for one that works.
func TestPrinterAppSubtitleDistinguishesEveryState(t *testing.T) {
	seen := map[string]printerapp.State{}
	for s := printerapp.StateUnavailable; s <= printerapp.StateFailedCrash; s++ {
		text := PrinterAppSubtitle(s, 18010)
		if prev, dup := seen[text]; dup {
			t.Errorf("states %d and %d share subtitle %q", prev, s, text)
		}
		seen[text] = s
		if s != printerapp.StateReady && strings.HasPrefix(text, "Running") {
			t.Errorf("state %d claims running: %q", s, text)
		}
	}
}

// Every diagnostic failure state says what went wrong and what to do in a
// person's words, without claiming running and without naming a command,
// group, or registry.
func TestPrinterAppDiagnosticSubtitlesAreActionable(t *testing.T) {
	cases := []struct {
		state printerapp.State
		wants []string
	}{
		{
			state: printerapp.StateUnavailable,
			wants: []string{"Not available"},
		},
		{
			state: printerapp.StateFailedDeviceAccess,
			wants: []string{"Couldn't reach the printer", "USB"},
		},
		{
			state: printerapp.StateFailedImage,
			wants: []string{"Couldn't download", "internet connection"},
		},
		{
			state: printerapp.StateFailedPlugin,
			wants: []string{"security check", "wasn't installed"},
		},
		{
			state: printerapp.StateFailedCrash,
			wants: []string{"stopped unexpectedly", "Turn it off and on again"},
		},
	}

	for _, tc := range cases {
		text := PrinterAppSubtitle(tc.state, 18010)
		for _, want := range tc.wants {
			if !strings.Contains(text, want) {
				t.Errorf("state %d subtitle %q does not contain %q", tc.state, text, want)
			}
		}
		if strings.HasPrefix(text, "Running") {
			t.Errorf("failed state %d claims running: %q", tc.state, text)
		}
		for _, jargon := range []string{"journalctl", "Podman", "registry", "udev", "'lp'", "container"} {
			if strings.Contains(text, jargon) {
				t.Errorf("state %d subtitle %q names %q", tc.state, text, jargon)
			}
		}
	}
}

// The ready row is where a person goes next: the web page on the app's port.
func TestPrinterAppReadySubtitleNamesTheWebPage(t *testing.T) {
	for _, f := range printerapp.Families() {
		app := printerapp.Select(f)
		text := PrinterAppSubtitle(printerapp.StateReady, app.Port())
		want := "http://localhost:" + strconv.Itoa(app.Port()) + "/"
		if !strings.Contains(text, want) {
			t.Errorf("%s ready subtitle %q does not name %q", f.ID, text, want)
		}
	}
}

// A failed stop keeps the unit because the service may still run, so the
// toast has to keep saying it is running, and name which row it is about.
func TestPrinterAppFailureToastNamesTheRowAndDoesNotClaimAStop(t *testing.T) {
	off := strings.ToLower(PrinterAppFailureToast(false, "HP Printers (HPLIP)"))
	if !strings.Contains(off, "hp printers") || !strings.Contains(off, "still running") || !strings.Contains(off, "couldn't turn it off") {
		t.Errorf("failed-disable toast = %q", off)
	}
	on := PrinterAppFailureToast(true, "Gutenprint Printers")
	if !strings.Contains(on, "Gutenprint Printers") || !strings.HasPrefix(on, "Couldn't turn on") {
		t.Errorf("failed-enable toast = %q", on)
	}
}

// A family installed before signatures were checked keeps its state first
// and then says, without jargon, how to switch to a checked driver (#582).
func TestPrinterAppUnverifiedSubtitleKeepsStateAndSaysWhatToDo(t *testing.T) {
	base := PrinterAppSubtitle(printerapp.StateReady, 18010)
	got := PrinterAppUnverifiedSubtitle(base)
	if !strings.HasPrefix(got, base) {
		t.Errorf("subtitle %q does not lead with the state %q", got, base)
	}
	if !strings.Contains(got, "turn it off and on again") {
		t.Errorf("subtitle %q does not say what to do", got)
	}
	for _, jargon := range []string{"cosign", "digest", "registry", "AutoUpdate", "quadlet", "Podman"} {
		if strings.Contains(got, jargon) {
			t.Errorf("subtitle %q names %q", got, jargon)
		}
	}
}

// The Manage button hands the browser exactly the page the row's subtitle
// names: a person reads one and presses the other, so the two must not drift
// to different addresses (#671).
func TestPrinterAdminButtonAgreesWithTheReadySubtitle(t *testing.T) {
	for _, f := range printerapp.Families() {
		app := printerapp.Select(f)
		subtitle := PrinterAppSubtitle(printerapp.StateReady, app.Port())
		if url := app.WebAdminURL(); !strings.Contains(subtitle, url) {
			t.Errorf("%s ready subtitle %q does not name the button's %q", f.ID, subtitle, url)
		}
	}
}

// The button says what it does while it works, and why it is not available
// yet while the application is starting: an insensitive control with the
// ready wording would read as broken.
func TestPrinterAdminButtonTooltipFollowsTheState(t *testing.T) {
	ready := PrinterAdminButtonTooltip(printerapp.StateReady)
	if !strings.Contains(ready, "web page") {
		t.Errorf("ready tooltip = %q", ready)
	}
	for _, s := range []printerapp.State{printerapp.StateStarting, printerapp.StateFailed, printerapp.StateOff, printerapp.StateUnavailable} {
		text := PrinterAdminButtonTooltip(s)
		if text == ready || !strings.Contains(text, "running") {
			t.Errorf("state %d tooltip = %q; it must say the page needs a running application", s, text)
		}
	}
	if label := PrinterAdminButtonLabel(); label == "" {
		t.Error("AdminButtonLabel is empty; the button would be announced with no name")
	}
}
