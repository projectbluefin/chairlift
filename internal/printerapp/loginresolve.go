package printerapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/livery"
)

// LoginUnitName is the systemd user unit that finds each family's chosen USB
// printer again at login. It does not start with unitPrefix, which names one
// family's quadlet.
const LoginUnitName = "chairlift-printers-usb.service"

// ResolveUSBFlag is the command-line flag that runs one login pass. It is the
// login unit's entire ExecStart.
const ResolveUSBFlag = "--resolve-printer-usb"

// loginUnitTemplate runs the pass once per login, before any family starts.
// A family's unit starts at every login with the device path last written
// into it, and a USB path changes on a replug or reboot, so without this pass
// the family would start without its printer or, worse, with whatever device
// now has the old bus and device numbers.
const loginUnitTemplate = `[Unit]
Description=%s USB printer lookup
Before=%s

[Service]
Type=oneshot
%sExecStart=%s %s

[Install]
WantedBy=default.target
`

// loginUnitDir is an injection seam for the systemd user unit directory.
var loginUnitDir = defaultLoginUnitDir

func defaultLoginUnitDir() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "systemd", "user"), nil
}

// loginExecutable is an injection seam for the binary the login unit runs.
// Livery's helper names the Homebrew bin link rather than the versioned
// Caskroom path an upgrade deletes.
var loginExecutable = livery.UnitExecutable

// syncLoginUnit installs the login unit while any family is on and removes it
// once none is. It is a seam so tests of Enable and Disable see only the
// family's own systemctl calls.
var syncLoginUnit = defaultSyncLoginUnit

// LoginUnitPath returns the absolute path of the login unit.
func LoginUnitPath() (string, error) {
	dir, err := loginUnitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LoginUnitName), nil
}

// renderLoginUnit returns the login unit for this process. Like Livery's
// rotation unit, it carries GSETTINGS_SCHEMA_DIR forward: a source build
// finds the printers schema only through it, and systemd starts the unit with
// none of the developer's shell environment.
func renderLoginUnit() (string, error) {
	exe, err := loginExecutable()
	if err != nil {
		return "", fmt.Errorf("printerapp: locating the application executable: %w", err)
	}
	var env string
	if dir := os.Getenv("GSETTINGS_SCHEMA_DIR"); dir != "" {
		quoted, err := livery.SystemdQuote("GSETTINGS_SCHEMA_DIR=" + dir)
		if err != nil {
			return "", fmt.Errorf("printerapp: carrying GSETTINGS_SCHEMA_DIR into the login unit: %w", err)
		}
		env = "Environment=" + quoted + "\n"
	}
	quotedExe, err := livery.SystemdQuote(exe)
	if err != nil {
		return "", fmt.Errorf("printerapp: writing the login unit's ExecStart: %w", err)
	}
	services := make([]string, 0, len(families))
	for _, f := range families {
		services = append(services, Select(f).ServiceName())
	}
	return fmt.Sprintf(loginUnitTemplate, branding.AppName, strings.Join(services, " "), env, quotedExe, ResolveUSBFlag), nil
}

// anyFamilyEnabled reports whether any family's default app has a unit. Only
// a default app carries a saved USB printer (withSavedUSBPrinter).
func anyFamilyEnabled() bool {
	for _, f := range families {
		if IsEnabled(Select(f)) {
			return true
		}
	}
	return false
}

func defaultSyncLoginUnit(ctx context.Context) error {
	if anyFamilyEnabled() {
		return installLoginUnit(ctx)
	}
	return removeLoginUnit(ctx)
}

// installLoginUnit writes and enables the login unit. An identical unit is
// left alone, so turning a second family on makes no extra systemctl calls.
func installLoginUnit(ctx context.Context) error {
	unit, err := renderLoginUnit()
	if err != nil {
		return err
	}
	path, err := LoginUnitPath()
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == unit {
		return nil
	}
	if err := writeAtomic(path, []byte(unit)); err != nil {
		return fmt.Errorf("printerapp: installing the login unit: %w", err)
	}
	if err := runSystemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	return runSystemctl(ctx, "enable", LoginUnitName)
}

// removeLoginUnit disables and deletes the login unit. A missing unit is not
// an error.
func removeLoginUnit(ctx context.Context) error {
	path, err := LoginUnitPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	if err := runSystemctl(ctx, "disable", LoginUnitName); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return runSystemctl(ctx, "daemon-reload")
}

// ResolveUSBPrintersAtLogin finds each family's saved USB printer at its
// current path and rewrites the AddDevice= line of every installed default
// app whose line no longer matches: a new path, or no line at all when the
// printer is not connected, cannot be told apart from an identical one, or is
// no longer chosen. Changed units are reloaded and restarted without waiting,
// so a start systemd has already queued uses the new path. An unchanged unit
// is not rewritten and nothing is reloaded or restarted.
//
// Only the AddDevice= line is touched. The rest of the unit — above all the
// verified image digest Enable wrote — stays as it is: this pass runs at
// login, without the signature check, and must not change what runs.
//
// When the saved choices cannot be read, no unit is touched: guessing would
// strip a printer from a family whose choice is merely unreadable right now.
// The pass runs as the user and needs no privilege.
func ResolveUSBPrintersAtLogin(ctx context.Context) error {
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would find each saved USB printer again and update its family's unit")
		return nil
	}
	choices, err := LoadUSBChoices(ctx)
	if errors.Is(err, ErrUSBSchemaMissing) {
		return nil
	}
	if err != nil {
		return err
	}
	devices, err := ScanUSBDevices()
	if err != nil {
		return fmt.Errorf("printerapp: scanning USB devices: %w", err)
	}

	var changed []App
	var errs []error
	for _, f := range families {
		app := Select(f)
		path, err := UnitPath(app)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		devicePath := ""
		if id := choices[f.ID]; !id.IsZero() {
			if p, ok := resolveUSBIdentityIn(devices, id); ok {
				devicePath = p
			} else {
				log.Printf("printerapp: USB printer %s for %s is not connected; starting without it", id, app.UnitName())
			}
		}
		next, ok := withDeviceLine(string(data), devicePath)
		if !ok {
			log.Printf("printerapp: %s has no [Container] section; leaving it alone", path)
			continue
		}
		if next == string(data) {
			continue
		}
		if err := writeAtomic(path, []byte(next)); err != nil {
			errs = append(errs, err)
			continue
		}
		changed = append(changed, app)
	}
	if len(changed) == 0 {
		return errors.Join(errs...)
	}
	if err := runSystemctl(ctx, "daemon-reload"); err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, app := range changed {
		if err := runSystemctl(ctx, "restart", "--no-block", app.ServiceName()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// withDeviceLine returns unit with its AddDevice= lines replaced by the one
// RenderUnit would write for devicePath, in the place RenderUnit writes it:
// before PublishPort=, or straight after [Container] in a unit without one.
// ok is false for a unit with neither, which is not one ChairLift wrote.
func withDeviceLine(unit, devicePath string) (string, bool) {
	lines := strings.SplitAfter(unit, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(line, "AddDevice=") {
			kept = append(kept, line)
		}
	}
	insertAt := -1
	for i, line := range kept {
		if strings.HasPrefix(line, "PublishPort=") {
			insertAt = i
			break
		}
	}
	if insertAt < 0 {
		for i, line := range kept {
			if strings.TrimSpace(line) == "[Container]" {
				insertAt = i + 1
				break
			}
		}
	}
	if insertAt < 0 {
		return unit, false
	}
	var b strings.Builder
	for i, line := range kept {
		if i == insertAt {
			b.WriteString(deviceLine(devicePath))
		}
		b.WriteString(line)
	}
	if insertAt == len(kept) {
		b.WriteString(deviceLine(devicePath))
	}
	return b.String(), true
}
