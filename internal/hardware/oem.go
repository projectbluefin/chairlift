package hardware

// Profile describes the hardware enablement integration and OEM packages for a vendor.
type Profile struct {
	Vendor      Vendor
	DisplayName string
	Packages    []string // e.g. ["ublue-os/tap/asusctl-linux", "ublue-os/tap/rog-control-center-linux"]
}

// OEMProfile returns the enablement profile and true if vendor is supported,
// or an empty Profile and false if none.
func OEMProfile(v Vendor) (Profile, bool) {
	switch v {
	case VendorASUS:
		return Profile{
			Vendor:      VendorASUS,
			DisplayName: "ASUS Hardware Support",
			Packages: []string{
				"ublue-os/tap/asusctl-linux",
				"ublue-os/tap/rog-control-center-linux",
			},
		}, true
	case VendorFramework:
		return Profile{
			Vendor:      VendorFramework,
			DisplayName: "Framework Hardware Support",
			Packages: []string{
				"ublue-os/tap/framework-tool",
				"ublue-os/tap/framework-wallpapers",
			},
		}, true
	default:
		return Profile{}, false
	}
}
