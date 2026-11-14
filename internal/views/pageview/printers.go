package pageview

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

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

// PrinterUSBRowTitle is the title of the row under each family that picks
// the USB printer the family's application may use.
func PrinterUSBRowTitle() string {
	return "USB Printer"
}

// PrinterUSBRowSubtitle says what the picker is for.
func PrinterUSBRowSubtitle() string {
	return "The printer plugged into this computer that these drivers may use."
}

// PrinterUSBUnavailableSubtitle replaces the picker's subtitle when the
// saved choices cannot be read, so the picker is offered but not usable.
func PrinterUSBUnavailableSubtitle() string {
	return "Couldn't read the saved printer choice."
}

// PrinterUSBChoiceLabel is one entry of the picker: "No USB printer", a
// connected printer by the maker and model it reports, or a saved printer
// that is not plugged in right now.
func PrinterUSBChoiceLabel(c printerapp.USBChoice) string {
	if c.Identity.IsZero() {
		return "No USB printer"
	}
	if c.Device == nil {
		return "Saved printer (not connected)"
	}
	if name := strings.TrimSpace(c.Device.Manufacturer + " " + c.Device.Product); name != "" {
		return name
	}
	return "USB printer " + c.Identity.VendorID + ":" + c.Identity.ProductID
}

// PrinterUSBChoiceLabels labels every entry, in order.
func PrinterUSBChoiceLabels(choices []printerapp.USBChoice) []string {
	labels := make([]string, len(choices))
	for i, c := range choices {
		labels[i] = PrinterUSBChoiceLabel(c)
	}
	return labels
}

// PrinterAppUSBNotConnectedSubtitle adds, to a family's subtitle, that its
// chosen printer is not plugged in. The family still runs without it.
func PrinterAppUSBNotConnectedSubtitle(subtitle string) string {
	return subtitle + " USB printer not connected."
}

// PrinterAppUSBNoAccessSubtitle adds, to a family's subtitle, that its chosen
// printer is plugged in but this account cannot use it yet. The cause is the
// OS's device permissions; the row names no group or rule to change.
func PrinterAppUSBNoAccessSubtitle(subtitle string) string {
	return subtitle + " This account can't use the USB printer yet."
}

// PrinterUSBChosenToast confirms a choice. restarted says a running family
// was restarted to pick it up; a family that is off uses it when turned on.
func PrinterUSBChosenToast(dryRun bool, rowTitle, label string, restarted bool) string {
	if dryRun {
		return "Preview: " + rowTitle + " would use " + label + ". Nothing was changed."
	}
	if restarted {
		return rowTitle + " restarted to use " + label + "."
	}
	return rowTitle + " will use " + label + "."
}

// PrinterUSBFailureToast says a choice was not saved, and when the printer
// already belongs to another family, how to free it.
func PrinterUSBFailureToast(rowTitle string, err error) string {
	if errors.Is(err, printerapp.ErrUSBPrinterTaken) {
		return "That printer is already used by another driver. Choose No USB printer there first."
	}
	return "Couldn't save the USB printer for " + rowTitle + ". Try again."
}

// PrinterUSBRestartFailedToast says a choice was saved but the running
// family could not be restarted to use it.
func PrinterUSBRestartFailedToast(rowTitle string) string {
	return "Saved, but couldn't restart " + rowTitle + ". Turn it off and on again to use the printer."
}
