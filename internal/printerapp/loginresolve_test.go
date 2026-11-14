package printerapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// installedHPLIP writes the hplip family's unit the way Enable would, with a
// verified digest and the given device path, and returns the app as written.
func installedHPLIP(t *testing.T, dir, devicePath string) App {
	t.Helper()
	app := Select(familyByID(t, "hplip"))
	app.Family.Image = "ghcr.io/projectbluefin/hplip-printer-app@sha256:" + strings.Repeat("b", 64)
	app.DevicePath = devicePath
	if err := os.WriteFile(filepath.Join(dir, app.UnitName()), []byte(RenderUnit(app)), 0o644); err != nil {
		t.Fatal(err)
	}
	return app
}

func readUnit(t *testing.T, dir string, app App) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, app.UnitName()))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func everyPathIsADevice(t *testing.T) {
	t.Helper()
	prev := statDeviceNode
	statDeviceNode = func(string) bool { return true }
	t.Cleanup(func() { statDeviceNode = prev })
}

func TestLoginPassRewritesTheDeviceLineOnlyWhenItChanged(t *testing.T) {
	const oldPath = "/dev/bus/usb/001/008"
	chosen := fakeUSBDevice{name: "2-1", bus: "2", dev: "14", vendor: "03f0", product: "052a", serial: "ABC123", printer: true}
	cases := []struct {
		name    string
		devices []fakeUSBDevice
		stored  string
		// want is the device path the rewritten unit passes through; "" for none.
		want      string
		unchanged bool
	}{
		{"printer has a new number", []fakeUSBDevice{chosen}, "{'hplip': '03f0:052a:ABC123'}", "/dev/bus/usb/002/014", false},
		{
			"old number now belongs to another device",
			[]fakeUSBDevice{{name: "1-1", bus: "1", dev: "8", vendor: "046d", product: "c52b", serial: "MOUSE", printer: false}},
			"{'hplip': '03f0:052a:ABC123'}", "", false,
		},
		{
			"old number now belongs to another printer",
			[]fakeUSBDevice{{name: "1-1", bus: "1", dev: "8", vendor: "04b8", product: "0005", serial: "EPSON", printer: true}},
			"{'hplip': '03f0:052a:ABC123'}", "", false,
		},
		{"printer not connected", nil, "{'hplip': '03f0:052a:ABC123'}", "", false},
		{"printer no longer chosen", []fakeUSBDevice{chosen}, "@a{ss} {}", "", false},
		{"printer still at its number", []fakeUSBDevice{{name: "1-1", bus: "1", dev: "8", vendor: "03f0", product: "052a", serial: "ABC123", printer: true}}, "{'hplip': '03f0:052a:ABC123'}", oldPath, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			everyPathIsADevice(t)
			dir, calls := stubUnitDir(t)
			useFakeUSBSysfs(t, tc.devices...)
			useFakeGSettings(t, tc.stored)
			app := installedHPLIP(t, dir, oldPath)
			unitPath := filepath.Join(dir, app.UnitName())
			past := time.Now().Add(-time.Hour).Truncate(time.Second)
			if err := os.Chtimes(unitPath, past, past); err != nil {
				t.Fatal(err)
			}

			if err := ResolveUSBPrintersAtLogin(context.Background()); err != nil {
				t.Fatalf("ResolveUSBPrintersAtLogin: %v", err)
			}

			wantApp := app
			wantApp.DevicePath = tc.want
			if got := readUnit(t, dir, app); got != RenderUnit(wantApp) {
				t.Errorf("unit after login =\n%s\nwant\n%s", got, RenderUnit(wantApp))
			}
			info, err := os.Stat(unitPath)
			if err != nil {
				t.Fatal(err)
			}
			if tc.unchanged {
				if !info.ModTime().Equal(past) {
					t.Error("an unchanged unit was rewritten")
				}
				if len(*calls) != 0 {
					t.Errorf("systemctl calls = %v; nothing changed, so nothing may be reloaded or restarted", *calls)
				}
				return
			}
			want := "daemon-reload|restart --no-block " + app.ServiceName()
			if got := strings.Join(*calls, "|"); got != want {
				t.Errorf("systemctl calls = %q, want %q", got, want)
			}
		})
	}
}

// The pass keeps everything but the device line: it runs without the
// signature check, so it must never change the image the family runs.
func TestLoginPassKeepsTheVerifiedImage(t *testing.T) {
	everyPathIsADevice(t)
	dir, _ := stubUnitDir(t)
	useFakeUSBSysfs(t, fakeUSBDevice{name: "2-1", bus: "2", dev: "14", vendor: "03f0", product: "052a", serial: "ABC123", printer: true})
	useFakeGSettings(t, "{'hplip': '03f0:052a:ABC123'}")
	app := installedHPLIP(t, dir, "")

	if err := ResolveUSBPrintersAtLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	unit := readUnit(t, dir, app)
	if !strings.Contains(unit, "Image="+app.Family.Image+"\n") || strings.Contains(unit, ":stable") {
		t.Errorf("login pass changed the verified image:\n%s", unit)
	}
	if !strings.Contains(unit, "AddDevice=-/dev/bus/usb/002/014\n") {
		t.Errorf("login pass did not add the printer:\n%s", unit)
	}
}

// When the saved choices cannot be read, guessing would strip a printer from
// its family, so nothing is touched.
func TestLoginPassLeavesUnitsAloneWhenChoicesAreUnreadable(t *testing.T) {
	for _, tc := range []struct {
		name    string
		out     string
		wantErr bool
	}{
		{"schema missing", "No such schema “" + USBSchemaID + "”", false},
		{"dconf unreachable", "Failed to connect", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			everyPathIsADevice(t)
			dir, calls := stubUnitDir(t)
			useFakeUSBSysfs(t)
			prev := runGSettings
			runGSettings = func(context.Context, ...string) (string, error) {
				return tc.out, errors.New("exit status 1")
			}
			t.Cleanup(func() { runGSettings = prev })
			app := installedHPLIP(t, dir, "/dev/bus/usb/001/008")
			before := readUnit(t, dir, app)

			err := ResolveUSBPrintersAtLogin(context.Background())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %t", err, tc.wantErr)
			}
			if readUnit(t, dir, app) != before || len(*calls) != 0 {
				t.Errorf("unit or user manager touched: calls %v", *calls)
			}
		})
	}
}

func TestLoginPassSkipsFamiliesThatAreOff(t *testing.T) {
	dir, calls := stubUnitDir(t)
	useFakeUSBSysfs(t)
	useFakeGSettings(t, "{'hplip': '03f0:052a:ABC123'}")
	if err := ResolveUSBPrintersAtLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 || len(*calls) != 0 {
		t.Errorf("login pass wrote %v or called %v with no family on", entries, *calls)
	}
}

func TestWithDeviceLineRefusesAUnitItDidNotWrite(t *testing.T) {
	unit := "[Unit]\nDescription=someone else's\n"
	if got, ok := withDeviceLine(unit, "/dev/bus/usb/001/008"); ok || got != unit {
		t.Errorf("withDeviceLine = %q, %t; want the unit unchanged and false", got, ok)
	}
}

func useLoginUnit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("GSETTINGS_SCHEMA_DIR", "")
	prevDir, prevExe, prevSync := loginUnitDir, loginExecutable, syncLoginUnit
	loginUnitDir = func() (string, error) { return dir, nil }
	loginExecutable = func() (string, error) { return "/usr/bin/chairlift", nil }
	syncLoginUnit = defaultSyncLoginUnit
	t.Cleanup(func() { loginUnitDir, loginExecutable, syncLoginUnit = prevDir, prevExe, prevSync })
	return filepath.Join(dir, LoginUnitName)
}

func TestLoginUnitRunsBeforeEveryFamily(t *testing.T) {
	useLoginUnit(t)
	unit, err := renderLoginUnit()
	if err != nil {
		t.Fatal(err)
	}
	var before string
	for _, line := range strings.Split(unit, "\n") {
		if v, ok := strings.CutPrefix(line, "Before="); ok {
			before = v
		}
	}
	ordered := map[string]bool{}
	for _, service := range strings.Fields(before) {
		ordered[service] = true
	}
	for _, f := range Families() {
		if service := Select(f).ServiceName(); !ordered[service] {
			t.Errorf("login unit does not run before %s:\n%s", service, unit)
		}
	}
	for _, want := range []string{"Type=oneshot\n", `ExecStart="/usr/bin/chairlift" ` + ResolveUSBFlag + "\n", "WantedBy=default.target\n"} {
		if !strings.Contains(unit, want) {
			t.Errorf("login unit lacks %q:\n%s", want, unit)
		}
	}
}

func TestLoginUnitFollowsTheFamilySwitches(t *testing.T) {
	_, calls := stubUnitDir(t)
	loginPath := useLoginUnit(t)
	ghostscript := Select(familyByID(t, "ghostscript"))
	hplip := Select(familyByID(t, "hplip"))
	step := func(name string, run func(context.Context, App) error, app App, wantCalls string, wantInstalled bool) {
		t.Helper()
		*calls = (*calls)[:0]
		if err := run(context.Background(), app); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := strings.Join(*calls, "|"); got != wantCalls {
			t.Errorf("%s: systemctl calls = %q, want %q", name, got, wantCalls)
		}
		if _, err := os.Stat(loginPath); (err == nil) != wantInstalled {
			t.Errorf("%s: login unit installed = %t, want %t", name, err == nil, wantInstalled)
		}
	}

	step("first family on", Enable, ghostscript,
		"daemon-reload|start "+ghostscript.ServiceName()+"|daemon-reload|enable "+LoginUnitName, true)
	step("second family on", Enable, hplip,
		"daemon-reload|start "+hplip.ServiceName(), true)
	step("one family off", Disable, ghostscript,
		"stop "+ghostscript.ServiceName()+"|daemon-reload", true)
	step("last family off", Disable, hplip,
		"stop "+hplip.ServiceName()+"|daemon-reload|disable "+LoginUnitName+"|daemon-reload", false)
}
