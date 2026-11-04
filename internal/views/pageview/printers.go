package pageview

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/printerapp"
)

// PrintersGroupTitle is the Features page's printer applications heading.
func PrintersGroupTitle() string {
	return "Printers"
}

// PrintersGroupDescription says what turning a family on does for a person:
// extra drivers for printers that do not work on their own.
func PrintersGroupDescription() string {
	return "Extra drivers for near-extinct printers that don't work on their own. Let's hope this works."
}

// familyRowTitles overrides the derived "<family> Printers" title where the
// family name alone would not tell a person which printers it is for.
var familyRowTitles = map[string]string{
	"hplip": "HP Printers (HPLIP)",
}

// PrinterFamilyRow is the switch row's title for one driver family.
func PrinterFamilyRow(f printerapp.Family) string {
	if title, ok := familyRowTitles[f.ID]; ok {
		return title
	}
	return f.DisplayName + " Printers"
}

// PrinterAppSubtitle is the switch row's subtitle for a resolved state. port
// is where the ready application serves both IPP and its web page, on
// localhost only.
func PrinterAppSubtitle(s printerapp.State, port int) string {
	switch s {
	case printerapp.StateUnavailable:
		return "Not available on this computer."
	case printerapp.StateStarting:
		return "Starting… The first start can take a few minutes."
	case printerapp.StateReady:
		return fmt.Sprintf("Running. Add and manage its printers at http://localhost:%d/", port)
	case printerapp.StateFailedDeviceAccess:
		return "Couldn't reach the printer. Your account may not be allowed to use USB printers."
	case printerapp.StateFailedImage:
		return "Couldn't download the printer driver. Check your internet connection."
	case printerapp.StateFailedPlugin:
		return "HP's driver add-on failed a security check, so it wasn't installed."
	case printerapp.StateFailedCrash:
		return "The printer driver stopped unexpectedly. Turn it off and on again."
	case printerapp.StateFailed:
		return "The printer driver isn't running. Turn it off and on again."
	default:
		return "Turn on to use these printers from this computer."
	}
}

// PrinterAdminButtonLabel is the row action that hands the family's own web
// page to the desktop's browser, so nobody has to retype the loopback address
// the subtitle names (#671). Short because the row it sits in already names
// the family, which the screen reader reads as the button's description.
func PrinterAdminButtonLabel() string {
	return "Manage"
}

// PrinterAdminButtonTooltip says what pressing the button does, and for a
// family that is installed but not answering yet, why it is not available
// yet: the page only exists while the application is running.
func PrinterAdminButtonTooltip(s printerapp.State) string {
	if s == printerapp.StateReady {
		return "Open this printer's web page"
	}
	return "This printer's web page is available once it is running."
}

// PrinterAppUnverifiedSubtitle adds, to an installed family's subtitle, that
// it was installed before driver signatures were checked and how to switch
// to a checked driver. The row keeps its state first so "Running" or a
// failure is still the first thing read.
func PrinterAppUnverifiedSubtitle(subtitle string) string {
	return subtitle + " Installed before " + branding.AppName + " checked driver signatures: turn it off and on again to use a checked driver."
}

// PrinterAppWorkingSubtitle is shown while the switch is acting.
func PrinterAppWorkingSubtitle(enabling bool) string {
	if enabling {
		return "Turning on…"
	}
	return "Turning off…"
}

// PrinterAppFailureToast is the toast for a switch that could not do what
// was asked, naming the row so three families' toasts are distinguishable.
// The error itself is logged: it names commands and unit files. A failed
// disable keeps the unit, because the service could not be proven stopped,
// so that toast says the driver is still running.
func PrinterAppFailureToast(enabling bool, rowTitle string, failure ...error) string {
	if enabling && len(failure) > 0 && errors.Is(failure[0], printerapp.ErrSignatureUnverified) {
		if errors.Is(failure[0], exec.ErrNotFound) {
			return "Couldn't turn on " + rowTitle + ". Install cosign and make it available on PATH."
		}
		return "Couldn't turn on " + rowTitle + ". The printer driver's signature could not be verified."
	}
	if enabling {
		return "Couldn't turn on " + rowTitle + ". Try again."
	}
	return rowTitle + " is still running. Couldn't turn it off. Try again."
}
