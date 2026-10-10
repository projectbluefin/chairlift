package pageview

import (
	"fmt"

	"github.com/projectbluefin/chairlift/internal/printerapp"
)

// PrintersGroupTitle is the Features page's printer applications heading.
func PrintersGroupTitle() string {
	return "Printers"
}

// PrintersGroupDescription says what turning a family on does for a person:
// extra drivers for printers that do not work on their own, usable from this
// computer only. That limit is the one consequence worth knowing before
// flipping the switch, so it is named.
func PrintersGroupDescription() string {
	return "Extra drivers for printers that don't work on their own. Only this computer can use them."
}

// familyRowTitles overrides the derived "<family> printers" title where the
// family name alone would not tell a person which printers it is for.
var familyRowTitles = map[string]string{
	"hplip": "HP printers (HPLIP)",
}

// PrinterFamilyRow is the switch row's title for one driver family.
func PrinterFamilyRow(f printerapp.Family) string {
	if title, ok := familyRowTitles[f.ID]; ok {
		return title
	}
	return f.DisplayName + " printers"
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
func PrinterAppFailureToast(enabling bool, rowTitle string) string {
	if enabling {
		return "Couldn't turn on " + rowTitle + ". Try again."
	}
	return rowTitle + " is still running. Couldn't turn it off. Try again."
}
