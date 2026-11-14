package printerapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// USBSchemaID is the GSettings schema holding each family's chosen USB
// printer. A choice is a user preference with no file to observe it from: the
// unit carries only today's device path, and turning a family off removes the
// unit, while the choice must outlive both.
const USBSchemaID = "io.projectbluefin.chairlift.printers"

// KeyUSBPrinters maps a family ID to the USBIdentity string of the printer
// passed through to it. A family with no entry has no USB printer.
const KeyUSBPrinters = "usb-printers"

// USBKeys is the schema's complete key inventory.
var USBKeys = []string{KeyUSBPrinters}

// ErrUSBSchemaMissing reports that the printers settings schema is not
// installed, so no choice can be read or saved.
var ErrUSBSchemaMissing = errors.New("printerapp: the printers settings schema is not installed")

// ErrUSBPrinterTaken reports a printer already chosen for another family.
// One printer has one owner: two applications driving the same device would
// fight over it.
var ErrUSBPrinterTaken = errors.New("printerapp: that USB printer is already used by another printer family")

// runGSettings runs the unprivileged `gsettings` tool. It is a seam so tests
// never touch the real dconf database.
var runGSettings = func(ctx context.Context, args ...string) (string, error) {
	return runCommand(ctx, "gsettings", args...)
}

// usbChoicesMu serialises SaveUSBChoice's read-modify-write of the whole map,
// so two rows saved at once cannot drop each other's choice.
var usbChoicesMu sync.Mutex

// LoadUSBChoices reads every family's chosen printer. An entry that does not
// parse is dropped with a log line rather than failing every family.
func LoadUSBChoices(ctx context.Context) (map[string]USBIdentity, error) {
	out, err := runGSettings(ctx, "get", USBSchemaID, KeyUSBPrinters)
	if err != nil {
		if isMissingUSBSchema(out) {
			return nil, ErrUSBSchemaMissing
		}
		return nil, fmt.Errorf("printerapp: reading USB printer choices: %w: %s", err, strings.TrimSpace(out))
	}
	return parseUSBChoices(out), nil
}

// SaveUSBChoice records id as familyID's USB printer; the zero identity
// removes the choice. It refuses, with ErrUSBPrinterTaken, a printer another
// family already has. Under dry-run it checks the same and writes nothing.
func SaveUSBChoice(ctx context.Context, familyID string, id USBIdentity) error {
	usbChoicesMu.Lock()
	defer usbChoicesMu.Unlock()

	choices, err := LoadUSBChoices(ctx)
	if err != nil {
		return err
	}
	next, err := AssignUSBPrinter(choices, familyID, id)
	if err != nil {
		return err
	}
	value := formatUSBChoices(next)
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set %s %s=%s", USBSchemaID, KeyUSBPrinters, value)
		return nil
	}
	if out, err := runGSettings(ctx, "set", USBSchemaID, KeyUSBPrinters, value); err != nil {
		if isMissingUSBSchema(out) {
			return ErrUSBSchemaMissing
		}
		return fmt.Errorf("printerapp: saving USB printer choice: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// AssignUSBPrinter returns a copy of choices with familyID's printer set to
// id, or removed for the zero identity. It compares identities, not device
// paths, so a printer chosen for one family stays taken while it is unplugged.
func AssignUSBPrinter(choices map[string]USBIdentity, familyID string, id USBIdentity) (map[string]USBIdentity, error) {
	next := make(map[string]USBIdentity, len(choices)+1)
	for family, chosen := range choices {
		if family == familyID {
			continue
		}
		if !id.IsZero() && chosen == id {
			return nil, fmt.Errorf("%w (%s)", ErrUSBPrinterTaken, family)
		}
		next[family] = chosen
	}
	if !id.IsZero() {
		next[familyID] = id
	}
	return next, nil
}

// USBStatus is how a family's saved printer stands right now.
type USBStatus int

const (
	// USBNone means no USB printer is chosen.
	USBNone USBStatus = iota
	// USBConnected means the chosen printer is plugged in and identifiable.
	USBConnected
	// USBNotConnected means a printer is chosen but is not plugged in, or
	// cannot be told apart from an identical one. The family still starts,
	// without it.
	USBNotConnected
)

// USBStatusOf reports how id stands among the scanned devices.
func USBStatusOf(devices []USBDevice, id USBIdentity) USBStatus {
	if id.IsZero() {
		return USBNone
	}
	if _, ok := resolveUSBIdentityIn(devices, id); ok {
		return USBConnected
	}
	return USBNotConnected
}

// USBChoice is one entry of a family's USB printer list. The zero Identity is
// "no USB printer". Device is nil for a saved printer that is not connected.
type USBChoice struct {
	Identity USBIdentity
	Device   *USBDevice
}

// USBChoicesFor lists what a family's picker offers, in order: no USB printer,
// every connected printer, and — when the saved printer is not among them —
// the saved printer itself, so the picker can still show what is chosen.
// Printers that share an identity are listed once: they cannot be told
// apart, so offering both would be offering the same choice twice.
func USBChoicesFor(devices []USBDevice, saved USBIdentity) []USBChoice {
	choices := []USBChoice{{}}
	seen := map[USBIdentity]bool{}
	for i := range devices {
		d := devices[i]
		id := d.Identity()
		if !d.IsPrinter || id.IsZero() || seen[id] {
			continue
		}
		seen[id] = true
		choices = append(choices, USBChoice{Identity: id, Device: &d})
	}
	if !saved.IsZero() && !seen[saved] {
		choices = append(choices, USBChoice{Identity: saved})
	}
	return choices
}

// IndexOfUSBChoice returns the position of id in choices, or 0 — no USB
// printer — when it is not listed.
func IndexOfUSBChoice(choices []USBChoice, id USBIdentity) int {
	for i, c := range choices {
		if c.Identity == id {
			return i
		}
	}
	return 0
}

// withSavedUSBPrinter fills in the device path of the printer saved for app's
// family, so every route that writes a unit — turning a family on, choosing a
// printer, and the driver update — passes the printer through at its current
// path. A choice belongs to the family's default app only: two apps of one
// family must not both claim the device. An unreadable or unplugged choice is
// logged and the app starts without a printer, as before passthrough existed.
func withSavedUSBPrinter(ctx context.Context, app App) App {
	if app.DevicePath != "" || app.Name != app.Family.ID {
		return app
	}
	choices, err := LoadUSBChoices(ctx)
	if err != nil {
		if !errors.Is(err, ErrUSBSchemaMissing) {
			log.Printf("printerapp: %s starts without a USB printer: %v", app.UnitName(), err)
		}
		return app
	}
	id := choices[app.Family.ID]
	if id.IsZero() {
		return app
	}
	path, ok, err := ResolveUSBIdentity(id)
	if err != nil || !ok {
		log.Printf("printerapp: USB printer %s for %s is not connected; starting without it (scan error: %v)", id, app.UnitName(), err)
		return app
	}
	app.DevicePath = path
	return app
}

func isMissingUSBSchema(out string) bool {
	return strings.Contains(out, "No such schema") || strings.Contains(out, "not installed")
}

// formatUSBChoices writes choices as a GVariant a{ss} literal, sorted by
// family so the stored value is stable.
func formatUSBChoices(choices map[string]USBIdentity) string {
	if len(choices) == 0 {
		return "@a{ss} {}"
	}
	families := make([]string, 0, len(choices))
	for family := range choices {
		families = append(families, family)
	}
	sort.Strings(families)
	parts := make([]string, 0, len(families))
	for _, family := range families {
		parts = append(parts, gvariantQuote(family)+": "+gvariantQuote(choices[family].String()))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// gvariantQuote quotes s as a GVariant text-format string.
func gvariantQuote(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch {
		case r == '\'' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// parseUSBChoices reads the a{ss} value `gsettings get` prints, such as
// "{'hplip': '03f0:052a:ABC123'}" or "@a{ss} {}". The dictionary's strings
// alternate key, value, so reading its quoted strings in order is enough.
func parseUSBChoices(out string) map[string]USBIdentity {
	strs := gvariantStrings(out)
	choices := make(map[string]USBIdentity, len(strs)/2)
	for i := 0; i+1 < len(strs); i += 2 {
		id, err := ParseUSBIdentity(strs[i+1])
		if err != nil {
			log.Printf("printerapp: ignoring saved USB printer for %s: %v", strs[i], err)
			continue
		}
		if !id.IsZero() {
			choices[strs[i]] = id
		}
	}
	return choices
}

// gvariantStrings returns every quoted string in a GVariant text value, in
// order. GLib prints a string in single quotes, or in double quotes when it
// contains a single quote, and escapes with a backslash.
func gvariantStrings(text string) []string {
	var out []string
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		quote := runes[i]
		if quote != '\'' && quote != '"' {
			continue
		}
		var b strings.Builder
		for i++; i < len(runes) && runes[i] != quote; i++ {
			if runes[i] != '\\' || i+1 >= len(runes) {
				b.WriteRune(runes[i])
				continue
			}
			i++
			switch runes[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'u', 'U':
				width := 4
				if runes[i] == 'U' {
					width = 8
				}
				if i+width < len(runes) {
					if code, err := strconv.ParseUint(string(runes[i+1:i+1+width]), 16, 32); err == nil {
						b.WriteRune(rune(code))
						i += width
						continue
					}
				}
				b.WriteRune(runes[i])
			default:
				b.WriteRune(runes[i])
			}
		}
		out = append(out, b.String())
	}
	return out
}
