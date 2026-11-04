package hardware

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFramework(t *testing.T) {
	sysfs := t.TempDir()
	dmiDir := filepath.Join(sysfs, "sys", "class", "dmi", "id")
	if err := os.MkdirAll(dmiDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dmiDir, "chassis_vendor"), []byte("Framework\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmiDir, "sys_vendor"), []byte("Framework\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte("Laptop 13 (AMD Ryzen 7040Series)\n"), 0644); err != nil {
		t.Fatal(err)
	}

	vendor := Detect(sysfs)
	if vendor != VendorFramework {
		t.Errorf("expected %q, got %q", VendorFramework, vendor)
	}
}

func TestDetectASUS(t *testing.T) {
	tests := []struct {
		name          string
		sysVendor     string
		chassisVendor string
	}{
		{
			name:          "ASUSTeK COMPUTER INC.",
			sysVendor:     "ASUSTeK COMPUTER INC.",
			chassisVendor: "ASUSTeK COMPUTER INC.",
		},
		{
			name:          "ASUS",
			sysVendor:     "ASUS",
			chassisVendor: "ASUS",
		},
		{
			name:          "ASUSTeK in sys_vendor with other chassis",
			sysVendor:     "ASUSTeK COMPUTER INC.",
			chassisVendor: "Default string",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sysfs := t.TempDir()
			dmiDir := filepath.Join(sysfs, "sys", "class", "dmi", "id")
			if err := os.MkdirAll(dmiDir, 0755); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filepath.Join(dmiDir, "sys_vendor"), []byte(tc.sysVendor+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dmiDir, "chassis_vendor"), []byte(tc.chassisVendor+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte("ROG Zephyrus G14\n"), 0644); err != nil {
				t.Fatal(err)
			}

			vendor := Detect(sysfs)
			if vendor != VendorASUS {
				t.Errorf("expected %q, got %q", VendorASUS, vendor)
			}
		})
	}
}

func TestDetectGeneric(t *testing.T) {
	vendors := []struct {
		sysVendor     string
		chassisVendor string
	}{
		{sysVendor: "Lenovo", chassisVendor: "Lenovo"},
		{sysVendor: "Dell Inc.", chassisVendor: "Dell Inc."},
		{sysVendor: "HP", chassisVendor: "HP"},
		{sysVendor: "", chassisVendor: ""},
	}

	for _, v := range vendors {
		t.Run(v.sysVendor, func(t *testing.T) {
			sysfs := t.TempDir()
			dmiDir := filepath.Join(sysfs, "sys", "class", "dmi", "id")
			if err := os.MkdirAll(dmiDir, 0755); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(filepath.Join(dmiDir, "sys_vendor"), []byte(v.sysVendor+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dmiDir, "chassis_vendor"), []byte(v.chassisVendor+"\n"), 0644); err != nil {
				t.Fatal(err)
			}

			vendor := Detect(sysfs)
			if vendor != VendorNone {
				t.Errorf("expected %q, got %q", VendorNone, vendor)
			}
		})
	}
}

func TestDetectFallbackVirtualDMI(t *testing.T) {
	sysfs := t.TempDir()
	dmiDir := filepath.Join(sysfs, "sys", "devices", "virtual", "dmi", "id")
	if err := os.MkdirAll(dmiDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dmiDir, "chassis_vendor"), []byte("Framework\n"), 0644); err != nil {
		t.Fatal(err)
	}

	vendor := Detect(sysfs)
	if vendor != VendorFramework {
		t.Errorf("expected %q, got %q", VendorFramework, vendor)
	}
}
