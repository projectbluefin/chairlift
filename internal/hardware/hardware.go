package hardware

import (
	"os"
	"path/filepath"
	"strings"
)

// Vendor represents a detected hardware manufacturer.
type Vendor string

const (
	VendorNone      Vendor = "none"
	VendorASUS      Vendor = "asus"
	VendorFramework Vendor = "framework"
)

// SysfsPath returns the path to a DMI sysfs file, checking /sys/class/dmi/id
// and falling back to /sys/devices/virtual/dmi/id.
func SysfsPath(sysfsRoot, file string) string {
	if sysfsRoot == "" {
		sysfsRoot = "/"
	}
	candidates := []string{
		filepath.Join(sysfsRoot, "sys", "class", "dmi", "id", file),
		filepath.Join(sysfsRoot, "sys", "devices", "virtual", "dmi", "id", file),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	// Default to primary location if neither exists
	return candidates[0]
}

func readDMI(sysfsRoot, file string) string {
	path := SysfsPath(sysfsRoot, file)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// Detect inspects DMI sysfs attributes to identify supported OEM hardware.
// An empty sysfsRoot evaluates against the host root ("/").
func Detect(sysfsRoot string) Vendor {
	chassisVendor := readDMI(sysfsRoot, "chassis_vendor")
	sysVendor := readDMI(sysfsRoot, "sys_vendor")

	// Standard Bluefin pattern:
	// If chassis_vendor == "Framework" -> VendorFramework
	if chassisVendor == "Framework" {
		return VendorFramework
	}

	// If sys_vendor contains "ASUSTeK" or "ASUS" -> VendorASUS
	if strings.Contains(sysVendor, "ASUSTeK") || strings.Contains(sysVendor, "ASUS") {
		return VendorASUS
	}

	return VendorNone
}
