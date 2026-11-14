package printerapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// TestMain keeps every test in this package off the real dconf database:
// withSavedUSBPrinter runs inside Enable and Refresh, which many tests drive.
// Unless a test installs its own fake, the printers schema reads as missing.
func TestMain(m *testing.M) {
	runGSettings = func(context.Context, ...string) (string, error) {
		return "No such schema “" + USBSchemaID + "”", errors.New("exit status 1")
	}
	os.Exit(m.Run())
}

// fakeGSettings stores the usb-printers value in memory and records every
// call, standing in for `gsettings get` and `gsettings set`.
type fakeGSettings struct {
	value string
	calls []string
}

func useFakeGSettings(t *testing.T, initial string) *fakeGSettings {
	t.Helper()
	fake := &fakeGSettings{value: initial}
	previous := runGSettings
	runGSettings = func(_ context.Context, args ...string) (string, error) {
		fake.calls = append(fake.calls, strings.Join(args, " "))
		if len(args) < 3 || args[1] != USBSchemaID || args[2] != KeyUSBPrinters {
			return "No such key", errors.New("exit status 1")
		}
		switch args[0] {
		case "get":
			return fake.value + "\n", nil
		case "set":
			fake.value = args[3]
			return "", nil
		}
		return "", errors.New("unexpected gsettings call")
	}
	t.Cleanup(func() { runGSettings = previous })
	return fake
}

var (
	hpLaserJet = USBIdentity{VendorID: "03f0", ProductID: "052a", Serial: "ABC123"}
	epsonNoSN  = USBIdentity{VendorID: "04b8", ProductID: "0005"}
)

func TestUSBChoicesRoundTripThroughTheStoredValue(t *testing.T) {
	odd := USBIdentity{VendorID: "04b8", ProductID: "0005", Serial: `it's a \ "serial"`}
	choices := map[string]USBIdentity{"hplip": hpLaserJet, "ps": epsonNoSN, "gutenprint": odd}
	got := parseUSBChoices(formatUSBChoices(choices))
	if len(got) != len(choices) {
		t.Fatalf("round trip = %v, want %v", got, choices)
	}
	for family, id := range choices {
		if got[family] != id {
			t.Errorf("%s round-tripped to %+v, want %+v", family, got[family], id)
		}
	}
	if v := formatUSBChoices(nil); v != "@a{ss} {}" {
		t.Errorf("empty choices = %q, want a typed empty dictionary", v)
	}
	if got := parseUSBChoices("@a{ss} {}\n"); len(got) != 0 {
		t.Errorf("empty dictionary parsed to %v", got)
	}
}

// The forms GLib prints: single quotes, double quotes around a string that
// contains a single quote, and \u escapes for control characters.
func TestParseUSBChoicesReadsWhatGSettingsPrints(t *testing.T) {
	got := parseUSBChoices(`{'hplip': '03f0:052a:ABC123', 'ps': "04b8:0005:it's", 'gutenprint': '04b8:0006:a\u0009b', 'bogus': '/dev/bus/usb/001/008'}`)
	want := map[string]USBIdentity{
		"hplip":      hpLaserJet,
		"ps":         {VendorID: "04b8", ProductID: "0005", Serial: "it's"},
		"gutenprint": {VendorID: "04b8", ProductID: "0006", Serial: "a\tb"},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %v, want %v (malformed entries dropped)", got, want)
	}
	for family, id := range want {
		if got[family] != id {
			t.Errorf("%s = %+v, want %+v", family, got[family], id)
		}
	}
}

func TestAssignUSBPrinterKeepsOneOwnerPerPrinter(t *testing.T) {
	choices := map[string]USBIdentity{"hplip": hpLaserJet}

	if _, err := AssignUSBPrinter(choices, "ghostscript", hpLaserJet); !errors.Is(err, ErrUSBPrinterTaken) {
		t.Errorf("a second family took hplip's printer: err = %v", err)
	}

	next, err := AssignUSBPrinter(choices, "hplip", hpLaserJet)
	if err != nil || next["hplip"] != hpLaserJet {
		t.Errorf("re-choosing the same printer for its own family = %v, %v", next, err)
	}

	next, err = AssignUSBPrinter(choices, "ghostscript", epsonNoSN)
	if err != nil || next["ghostscript"] != epsonNoSN || next["hplip"] != hpLaserJet {
		t.Errorf("choosing a free printer = %v, %v; want both families kept", next, err)
	}

	next, err = AssignUSBPrinter(choices, "hplip", USBIdentity{})
	if err != nil || len(next) != 0 {
		t.Errorf("choosing no USB printer = %v, %v; want the entry removed", next, err)
	}
	if choices["hplip"] != hpLaserJet {
		t.Error("AssignUSBPrinter modified the map it was given")
	}
}

func TestSaveUSBChoiceWritesAndRefuses(t *testing.T) {
	fake := useFakeGSettings(t, "@a{ss} {}")
	ctx := context.Background()

	if err := SaveUSBChoice(ctx, "hplip", hpLaserJet); err != nil {
		t.Fatalf("SaveUSBChoice: %v", err)
	}
	if fake.value != "{'hplip': '03f0:052a:ABC123'}" {
		t.Errorf("stored value = %q", fake.value)
	}

	if err := SaveUSBChoice(ctx, "ghostscript", hpLaserJet); !errors.Is(err, ErrUSBPrinterTaken) {
		t.Errorf("saving a taken printer: err = %v, want ErrUSBPrinterTaken", err)
	}
	if fake.value != "{'hplip': '03f0:052a:ABC123'}" {
		t.Errorf("a refused save changed the stored value to %q", fake.value)
	}

	loaded, err := LoadUSBChoices(ctx)
	if err != nil || loaded["hplip"] != hpLaserJet {
		t.Errorf("LoadUSBChoices = %v, %v", loaded, err)
	}

	if err := SaveUSBChoice(ctx, "hplip", USBIdentity{}); err != nil {
		t.Fatal(err)
	}
	if fake.value != "@a{ss} {}" {
		t.Errorf("after choosing no USB printer the stored value = %q", fake.value)
	}
}

func TestSaveUSBChoiceUnderDryRunWritesNothing(t *testing.T) {
	fake := useFakeGSettings(t, "@a{ss} {}")
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := SaveUSBChoice(context.Background(), "hplip", hpLaserJet); err != nil {
		t.Fatal(err)
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "set ") {
			t.Errorf("dry-run wrote %q", call)
		}
	}
}

func TestUSBChoicesReportAMissingSchema(t *testing.T) {
	if _, err := LoadUSBChoices(context.Background()); !errors.Is(err, ErrUSBSchemaMissing) {
		t.Errorf("LoadUSBChoices without the schema: err = %v", err)
	}
	if err := SaveUSBChoice(context.Background(), "hplip", hpLaserJet); !errors.Is(err, ErrUSBSchemaMissing) {
		t.Errorf("SaveUSBChoice without the schema: err = %v", err)
	}
}

func TestUSBChoicesForListsNoneThenPrintersThenSaved(t *testing.T) {
	devices := []USBDevice{
		{DevicePath: "/dev/bus/usb/001/002", VendorID: "1d6b", ProductID: "0002"},
		{DevicePath: "/dev/bus/usb/001/008", VendorID: "03f0", ProductID: "052a", Serial: "ABC123", IsPrinter: true},
		{DevicePath: "/dev/bus/usb/001/009", VendorID: "04b8", ProductID: "0005", IsPrinter: true},
		// Indistinguishable from the one above: listed once.
		{DevicePath: "/dev/bus/usb/001/010", VendorID: "04b8", ProductID: "0005", IsPrinter: true},
		// No valid IDs: cannot be saved, so not offered.
		{DevicePath: "/dev/bus/usb/001/011", IsPrinter: true},
	}

	got := USBChoicesFor(devices, USBIdentity{})
	if len(got) != 3 || !got[0].Identity.IsZero() || got[1].Identity != hpLaserJet || got[2].Identity != epsonNoSN {
		t.Fatalf("choices = %+v; want none, the HP, the Epson", got)
	}
	if got[1].Device == nil || got[1].Device.DevicePath != "/dev/bus/usb/001/008" {
		t.Errorf("the HP choice lost its device: %+v", got[1])
	}

	unplugged := USBIdentity{VendorID: "0924", ProductID: "3ce8", Serial: "Z"}
	got = USBChoicesFor(devices, unplugged)
	if last := got[len(got)-1]; last.Identity != unplugged || last.Device != nil {
		t.Errorf("an unplugged saved printer is not listed last without a device: %+v", got)
	}
	if i := IndexOfUSBChoice(got, unplugged); i != len(got)-1 {
		t.Errorf("IndexOfUSBChoice(unplugged) = %d, want %d", i, len(got)-1)
	}
	if i := IndexOfUSBChoice(got, USBIdentity{VendorID: "ffff", ProductID: "ffff"}); i != 0 {
		t.Errorf("IndexOfUSBChoice(unknown) = %d, want 0 (no USB printer)", i)
	}
}

func TestUSBStatusOf(t *testing.T) {
	prevOpen := canOpenDevice
	t.Cleanup(func() { canOpenDevice = prevOpen })
	writable := true
	var checked []string
	canOpenDevice = func(path string) bool {
		checked = append(checked, path)
		return writable
	}

	devices := []USBDevice{{DevicePath: "/dev/bus/usb/001/008", VendorID: "03f0", ProductID: "052a", Serial: "ABC123", IsPrinter: true}}
	if s := USBStatusOf(devices, USBIdentity{}); s != USBNone {
		t.Errorf("no choice = %v, want USBNone", s)
	}
	if s := USBStatusOf(devices, hpLaserJet); s != USBConnected {
		t.Errorf("plugged-in writable choice = %v, want USBConnected", s)
	}
	if len(checked) != 1 || checked[0] != "/dev/bus/usb/001/008" {
		t.Errorf("access checked on %v, want the chosen printer's node only", checked)
	}
	if s := USBStatusOf(devices, epsonNoSN); s != USBNotConnected {
		t.Errorf("unplugged choice = %v, want USBNotConnected", s)
	}

	writable = false
	if s := USBStatusOf(devices, hpLaserJet); s != USBNoAccess {
		t.Errorf("plugged-in choice this account cannot open = %v, want USBNoAccess", s)
	}
	if s := USBStatusOf(devices, epsonNoSN); s != USBNotConnected {
		t.Errorf("unplugged choice without access = %v, want USBNotConnected", s)
	}
}

// A printer this account cannot open is still passed through, so it works
// as soon as the OS grants access, with no change to the unit.
func TestEnablePassesThroughAPrinterThisAccountCannotOpen(t *testing.T) {
	prevStat, prevOpen := statDeviceNode, canOpenDevice
	statDeviceNode = func(string) bool { return true }
	canOpenDevice = func(string) bool { return false }
	t.Cleanup(func() { statDeviceNode, canOpenDevice = prevStat, prevOpen })
	useFakeUSBSysfs(t, fakeUSBDevice{name: "2-1", bus: "2", dev: "14", vendor: "03f0", product: "052a", serial: "ABC123", printer: true})
	useFakeGSettings(t, "{'hplip': '03f0:052a:ABC123'}")

	app := withSavedUSBPrinter(context.Background(), Select(familyByID(t, "hplip")))
	if !strings.Contains(RenderUnit(app), "AddDevice=-/dev/bus/usb/002/014\n") {
		t.Errorf("unit for an unopenable printer lacks its device:\n%s", RenderUnit(app))
	}
}

// Turning a family on passes its saved printer through at the printer's
// current path; choosing no printer, or a printer that is unplugged, writes
// no AddDevice= line and the family still starts.
func TestEnablePassesTheSavedPrinterThrough(t *testing.T) {
	prevStat := statDeviceNode
	statDeviceNode = func(string) bool { return true }
	t.Cleanup(func() { statDeviceNode = prevStat })
	useFakeUSBSysfs(t, fakeUSBDevice{name: "2-1", bus: "2", dev: "14", vendor: "03f0", product: "052a", serial: "ABC123", printer: true})

	hplip := Select(familyByID(t, "hplip"))
	cases := []struct {
		name, stored, want string
	}{
		{"saved and connected", "{'hplip': '03f0:052a:ABC123'}", "AddDevice=-/dev/bus/usb/002/014\n"},
		{"no USB printer", "@a{ss} {}", ""},
		{"saved but unplugged", "{'hplip': '04b8:0005:'}", ""},
		{"another family's printer", "{'ps': '03f0:052a:ABC123'}", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, calls := stubUnitDir(t)
			useFakeGSettings(t, tc.stored)
			if err := Enable(context.Background(), hplip); err != nil {
				t.Fatalf("Enable: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, hplip.UnitName()))
			if err != nil {
				t.Fatal(err)
			}
			unit := string(data)
			if tc.want == "" && strings.Contains(unit, "AddDevice=") {
				t.Errorf("unit passes a device through:\n%s", unit)
			}
			if tc.want != "" && !strings.Contains(unit, tc.want) {
				t.Errorf("unit lacks %q:\n%s", tc.want, unit)
			}
			if got := strings.Join(*calls, "|"); got != "daemon-reload|start "+hplip.ServiceName() {
				t.Errorf("systemctl calls = %v; the family must still start", *calls)
			}
		})
	}
}

// A choice belongs to the family's default app; a second app of the same
// family never claims the device too.
func TestSavedPrinterAppliesOnlyToTheFamilyDefaultApp(t *testing.T) {
	useFakeUSBSysfs(t, fakeUSBDevice{name: "2-1", bus: "2", dev: "14", vendor: "03f0", product: "052a", serial: "ABC123", printer: true})
	useFakeGSettings(t, "{'hplip': '03f0:052a:ABC123'}")
	family := familyByID(t, "hplip")

	if got := withSavedUSBPrinter(context.Background(), Select(family)); got.DevicePath != "/dev/bus/usb/002/014" {
		t.Errorf("default app DevicePath = %q", got.DevicePath)
	}
	if got := withSavedUSBPrinter(context.Background(), App{Family: family, Name: "office"}); got.DevicePath != "" {
		t.Errorf("second app of the family claimed the device: %q", got.DevicePath)
	}
	explicit := App{Family: family, Name: family.ID, DevicePath: "/dev/bus/usb/009/009"}
	if got := withSavedUSBPrinter(context.Background(), explicit); got.DevicePath != explicit.DevicePath {
		t.Errorf("an explicit DevicePath was replaced: %q", got.DevicePath)
	}
}

func familyByID(t *testing.T, id string) Family {
	t.Helper()
	for _, f := range Families() {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("no family %q", id)
	return Family{}
}
