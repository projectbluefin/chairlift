package printerapp

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeUSBDevice writes one USB device directory into a fake sysfs root, the
// way TestScanUSBDevices does. An empty serial writes no serial file, as
// sysfs does for a device that reports none.
type fakeUSBDevice struct {
	name, bus, dev, vendor, product, serial string
	printer                                 bool
}

func useFakeUSBSysfs(t *testing.T, devices ...fakeUSBDevice) string {
	t.Helper()
	root := t.TempDir()
	previous := usbSysfsRoot
	usbSysfsRoot = root
	t.Cleanup(func() { usbSysfsRoot = previous })
	writeFakeUSBDevices(t, root, devices...)
	return root
}

func writeFakeUSBDevices(t *testing.T, root string, devices ...fakeUSBDevice) {
	t.Helper()
	for _, d := range devices {
		dir := filepath.Join(root, d.name)
		iface := filepath.Join(dir, d.name+":1.0")
		if err := os.MkdirAll(iface, 0o755); err != nil {
			t.Fatal(err)
		}
		class := "09"
		if d.printer {
			class = "07"
		}
		files := map[string]string{
			"busnum":       d.bus + "\n",
			"devnum":       d.dev + "\n",
			"idVendor":     d.vendor + "\n",
			"idProduct":    d.product + "\n",
			"manufacturer": "Maker\n",
			"product":      "Model\n",
			"bDeviceClass": "00\n",
			filepath.Join(d.name+":1.0", "bInterfaceClass"): class + "\n",
		}
		if d.serial != "" {
			files["serial"] = d.serial + "\n"
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestScanUSBDevicesReadsStableIdentity(t *testing.T) {
	useFakeUSBSysfs(t,
		fakeUSBDevice{name: "1-2", bus: "1", dev: "8", vendor: "03F0", product: "052a", serial: "ABC123", printer: true},
		fakeUSBDevice{name: "1-3", bus: "1", dev: "9", vendor: "04b8", product: "0005", printer: true},
	)
	devices, err := ScanUSBDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(devices))
	}
	if got := devices[0]; got.VendorID != "03f0" || got.ProductID != "052a" || got.Serial != "ABC123" {
		t.Errorf("printer with serial = %+v; want 03f0/052a/ABC123 (vendor lowercased)", got)
	}
	if got := devices[1]; got.VendorID != "04b8" || got.ProductID != "0005" || got.Serial != "" {
		t.Errorf("printer without serial = %+v; want 04b8/0005 and an empty serial", got)
	}
}

func TestUSBIdentityRoundTrips(t *testing.T) {
	for _, id := range []USBIdentity{
		{VendorID: "03f0", ProductID: "052a", Serial: "ABC123"},
		{VendorID: "04b8", ProductID: "0005"},
		{VendorID: "04b8", ProductID: "0005", Serial: "with:colon"},
		{},
	} {
		got, err := ParseUSBIdentity(id.String())
		if err != nil || got != id {
			t.Errorf("ParseUSBIdentity(%q) = %+v, %v; want %+v", id.String(), got, err, id)
		}
	}
	if s := (USBIdentity{VendorID: "03f0", ProductID: "052a", Serial: "ABC123"}).String(); s != "03f0:052a:ABC123" {
		t.Errorf("String() = %q, want 03f0:052a:ABC123", s)
	}
}

func TestParseUSBIdentityRejectsMalformedValues(t *testing.T) {
	for _, s := range []string{"03f0", "03f0:052a", "3f0:052a:x", "03f0:05zz:x", ":052a:x", "/dev/bus/usb/001/008"} {
		if _, err := ParseUSBIdentity(s); err == nil {
			t.Errorf("ParseUSBIdentity(%q) accepted a malformed identity", s)
		}
	}
}

func TestResolveUSBIdentityFindsPrinterAfterReplug(t *testing.T) {
	id := USBIdentity{VendorID: "03f0", ProductID: "052a", Serial: "ABC123"}
	root := useFakeUSBSysfs(t, fakeUSBDevice{name: "1-2", bus: "1", dev: "8", vendor: "03f0", product: "052a", serial: "ABC123", printer: true})

	path, ok, err := ResolveUSBIdentity(id)
	if err != nil || !ok || path != "/dev/bus/usb/001/008" {
		t.Fatalf("before replug: %q, %v, %v; want /dev/bus/usb/001/008", path, ok, err)
	}

	// Replugged into another port: same identity, new bus and device numbers.
	if err := os.RemoveAll(filepath.Join(root, "1-2")); err != nil {
		t.Fatal(err)
	}
	writeFakeUSBDevices(t, root, fakeUSBDevice{name: "2-1", bus: "2", dev: "14", vendor: "03f0", product: "052a", serial: "ABC123", printer: true})

	path, ok, err = ResolveUSBIdentity(id)
	if err != nil || !ok || path != "/dev/bus/usb/002/014" {
		t.Fatalf("after replug: %q, %v, %v; want /dev/bus/usb/002/014", path, ok, err)
	}
}

func TestResolveUSBIdentityWithoutSerial(t *testing.T) {
	id := USBIdentity{VendorID: "04b8", ProductID: "0005"}
	useFakeUSBSysfs(t, fakeUSBDevice{name: "1-3", bus: "1", dev: "9", vendor: "04b8", product: "0005", printer: true})
	path, ok, err := ResolveUSBIdentity(id)
	if err != nil || !ok || path != "/dev/bus/usb/001/009" {
		t.Fatalf("got %q, %v, %v; want /dev/bus/usb/001/009", path, ok, err)
	}
}

func TestResolveUSBIdentityRefusesNoMatchAndAmbiguousMatch(t *testing.T) {
	useFakeUSBSysfs(t,
		fakeUSBDevice{name: "1-3", bus: "1", dev: "9", vendor: "04b8", product: "0005", printer: true},
		fakeUSBDevice{name: "1-4", bus: "1", dev: "10", vendor: "04b8", product: "0005", printer: true},
		// Same IDs as a printer, but not a printer: never a match.
		fakeUSBDevice{name: "1-5", bus: "1", dev: "11", vendor: "03f0", product: "052a", serial: "HUB"},
	)
	cases := map[string]USBIdentity{
		"two identical printers without serials":  {VendorID: "04b8", ProductID: "0005"},
		"no connected printer":                    {VendorID: "03f0", ProductID: "052a", Serial: "ABC123"},
		"a matching device that is not a printer": {VendorID: "03f0", ProductID: "052a", Serial: "HUB"},
		"the zero identity":                       {},
	}
	for name, id := range cases {
		path, ok, err := ResolveUSBIdentity(id)
		if err != nil || ok || path != "" {
			t.Errorf("%s: got %q, %v, %v; want no match", name, path, ok, err)
		}
	}
}
