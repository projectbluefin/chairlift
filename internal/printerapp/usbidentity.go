package printerapp

import (
	"fmt"
	"strings"
)

// USBIdentity names one USB printer by what the printer reports about itself:
// its vendor ID, product ID and serial number. A device path such as
// /dev/bus/usb/001/008 is where the printer is plugged in right now, and it
// changes on a replug or a reboot; the identity does not, so it is what a
// saved choice records and ResolveUSBIdentity turns back into a path.
type USBIdentity struct {
	VendorID  string
	ProductID string
	// Serial is empty for a printer that reports no serial number.
	Serial string
}

// IsZero reports whether the identity names no printer.
func (id USBIdentity) IsZero() bool {
	return id == USBIdentity{}
}

// String is the identity's stored form, "vendor:product:serial", for example
// "03f0:052a:ABC123", or "03f0:052a:" for a printer with no serial number.
// The zero identity is the empty string.
func (id USBIdentity) String() string {
	if id.IsZero() {
		return ""
	}
	return id.VendorID + ":" + id.ProductID + ":" + id.Serial
}

// ParseUSBIdentity reads the form String writes. The empty string is the
// zero identity. The serial is everything after the second colon, so a
// serial number that itself contains a colon survives the round trip.
func ParseUSBIdentity(s string) (USBIdentity, error) {
	if s == "" {
		return USBIdentity{}, nil
	}
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 || !isUSBID(parts[0]) || !isUSBID(parts[1]) {
		return USBIdentity{}, fmt.Errorf("printerapp: %q is not a USB printer identity (want vendor:product:serial)", s)
	}
	return USBIdentity{
		VendorID:  strings.ToLower(parts[0]),
		ProductID: strings.ToLower(parts[1]),
		Serial:    parts[2],
	}, nil
}

// isUSBID reports whether s is a four-digit hexadecimal USB vendor or
// product ID.
func isUSBID(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		isHex := r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
		if !isHex {
			return false
		}
	}
	return true
}

// Identity is the device's stable identity. It is the zero identity when
// sysfs did not report valid vendor and product IDs, so such a device can be
// neither chosen nor matched.
func (d USBDevice) Identity() USBIdentity {
	if !isUSBID(d.VendorID) || !isUSBID(d.ProductID) {
		return USBIdentity{}
	}
	return USBIdentity{VendorID: d.VendorID, ProductID: d.ProductID, Serial: d.Serial}
}

// ResolveUSBIdentity scans the connected USB devices and returns the device
// path of the one printer with this identity. ok is false when no connected
// printer matches, and also when more than one does — two identical printers
// with no serial number cannot be told apart, and guessing would pass the
// wrong one through.
func ResolveUSBIdentity(id USBIdentity) (path string, ok bool, err error) {
	devices, err := ScanUSBDevices()
	if err != nil {
		return "", false, err
	}
	path, ok = resolveUSBIdentityIn(devices, id)
	return path, ok, nil
}

func resolveUSBIdentityIn(devices []USBDevice, id USBIdentity) (string, bool) {
	if id.IsZero() {
		return "", false
	}
	var path string
	matches := 0
	for _, d := range devices {
		if d.IsPrinter && d.Identity() == id {
			path = d.DevicePath
			matches++
		}
	}
	if matches != 1 {
		return "", false
	}
	return path, true
}
