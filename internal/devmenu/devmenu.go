package devmenu

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// Schema is the Custom Command Menu extension's GSettings schema ID.
const Schema = "org.gnome.shell.extensions.custom-command-list"

// DconfPath is the base dconf path for the Custom Command Menu extension.
const DconfPath = "/org/gnome/shell/extensions/custom-command-list/"

// MaxCommands is the maximum number of command keys in the schema (command1..command99).
const MaxCommands = 99

// CommandOrderKey is the extension's `ai` key listing which command slots the
// panel menu renders, in order. The extension skips every slot not listed
// here, so a slot's own visible flag alone does not put it in the menu.
const CommandOrderKey = "command-order"

// TargetLabels are the Custom Command Menu labels managed by Developer Mode.
var TargetLabels = []string{"Terminal", "Containers"}

const (
	// AskBluefinLabel is the title of the distro-owned Ask Bluefin menu entry.
	AskBluefinLabel = "Ask Bluefin"

	// AskBluefinCommand is the command of the distro-owned Ask Bluefin menu entry.
	AskBluefinCommand = "xdg-open https://ask.projectbluefin.io"

	// AskBluefinDispatchCommand is the entry the distro is expected to ship in
	// place of the web link; both are ChairLift's owned identity.
	AskBluefinDispatchCommand = "chairlift --ask-bluefin"

	// AskBluefinWrapperCommand is the dispatcher entry as Bluefin's distro
	// layer ships it (projectbluefin/common#1396): the Homebrew wrapper by
	// absolute path, because a GNOME Shell extension's command runs without
	// Homebrew on $PATH. ChairLift recognizes it and never rewrites it.
	AskBluefinWrapperCommand = "/home/linuxbrew/.linuxbrew/bin/chairlift-wrapper --ask-bluefin"
)

// askBluefinCommands are the commands an Ask Bluefin entry may carry and
// still be the one ChairLift shows or hides. A label alone is not enough: a
// user who repointed the slot at their own script owns it.
var askBluefinCommands = []string{AskBluefinCommand, AskBluefinDispatchCommand, AskBluefinWrapperCommand}

// Entry represents a Custom Command Menu tuple: (label, command, icon, visible).
type Entry struct {
	Label   string
	Command string
	Icon    string
	Visible bool
}

// IsDeveloperLabel reports whether a menu entry label is managed by Developer Mode.
func IsDeveloperLabel(label string) bool {
	for _, target := range TargetLabels {
		if label == target {
			return true
		}
	}
	return false
}

// IsAskBluefin reports whether an Entry matches ChairLift's owned Ask Bluefin identity.
func IsAskBluefin(e Entry) bool {
	return e.Label == AskBluefinLabel && slices.Contains(askBluefinCommands, e.Command)
}

// runCommand is an injection seam for external command execution.
var runCommand = execCommand

// lookPath is an injection seam for finding executables in PATH.
var lookPath = exec.LookPath

func execCommand(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// SetTestRunners configures custom runner and path-lookup functions for testing.
func SetTestRunners(lp func(string) (string, error), rc func(context.Context, string, ...string) (string, error)) {
	if lp != nil {
		lookPath = lp
	}
	if rc != nil {
		runCommand = rc
	}
}

// ResetTestRunners resets runner and path-lookup functions back to default.
func ResetTestRunners() {
	lookPath = exec.LookPath
	runCommand = execCommand
}

// menu is the extension's resolved state from one `dconf dump`: each command
// key's tuple and the raw command-order value ("" when no layer sets it).
type menu struct {
	entries map[string]string
	order   string
}

// scan reads every command key under the extension's dconf path with a single
// `dconf dump`, returning key name (command1..command99) to resolved tuple value
// along with the resolved command-order.
// dconf resolves through the whole profile, so the dump carries distro defaults
// (e.g. Bluefin's 04-bluefin-custom-command-menu) as well as user-layer overrides;
// one subprocess therefore replaces a per-key read of all 99 keys.
func scan(ctx context.Context) (menu, error) {
	out, err := runCommand(ctx, "dconf", "dump", DconfPath)
	if err != nil {
		return menu{}, fmt.Errorf("devmenu: dumping %s: %w: %s", DconfPath, err, strings.TrimSpace(out))
	}
	return parseDump(out), nil
}

// parseDump parses `dconf dump` keyfile output, keeping only command keys and
// command-order that live directly under the dumped path.
func parseDump(out string) menu {
	m := menu{entries: make(map[string]string)}
	inRoot := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inRoot = line == "[/]"
			continue
		}
		if !inRoot {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch {
		case key == CommandOrderKey:
			m.order = value
		case isCommandKey(key):
			m.entries[key] = value
		}
	}
	return m
}

// isCommandKey reports whether key names one of command1..command99.
func isCommandKey(key string) bool {
	digits, found := strings.CutPrefix(key, "command")
	if !found || digits == "" || strings.HasPrefix(digits, "0") {
		return false
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return false
	}
	return n >= 1 && n <= MaxCommands
}

// load probes the extension and returns its command entries in one dconf dump.
func load(ctx context.Context) (menu, error) {
	if _, err := lookPath("dconf"); err != nil {
		return menu{}, nil
	}
	return scan(ctx)
}

// schemaDefaultOrder is command-order's schema default, [1..99]: every slot.
// It applies when neither the user nor a distro layer sets the key.
func schemaDefaultOrder() []int {
	order := make([]int, MaxCommands)
	for i := range order {
		order[i] = i + 1
	}
	return order
}

// ParseCommandOrder parses command-order's GVariant text (`[1, 2, 3]`, or
// `@ai []` when empty). An empty raw value means no layer sets the key, so the
// schema default — every slot — is in effect.
func ParseCommandOrder(raw string) ([]int, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return schemaDefaultOrder(), nil
	}
	s = strings.TrimSpace(strings.TrimPrefix(s, "@ai"))
	inner, ok := strings.CutPrefix(s, "[")
	if !ok {
		return nil, fmt.Errorf("devmenu: command-order %q is not an array", raw)
	}
	inner, ok = strings.CutSuffix(inner, "]")
	if !ok {
		return nil, fmt.Errorf("devmenu: command-order %q is not an array", raw)
	}
	order := []int{}
	if strings.TrimSpace(inner) == "" {
		return order, nil
	}
	for _, field := range strings.Split(inner, ",") {
		field = strings.TrimSpace(field)
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("devmenu: command-order %q: %w", raw, err)
		}
		order = append(order, n)
	}
	return order, nil
}

// FormatCommandOrder formats an order as GVariant text for `dconf write`. An
// empty order carries its type annotation, which dconf cannot otherwise infer.
func FormatCommandOrder(order []int) string {
	if len(order) == 0 {
		return "@ai []"
	}
	parts := make([]string, len(order))
	for i, n := range order {
		parts[i] = strconv.Itoa(n)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// slotNumber returns n for a key command<n>.
func slotNumber(key string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(key, "command"))
	return n
}

// available reports whether the Custom Command Menu extension is present and can be managed.
// It decides availability from dconf alone without relying on gsettings (which cannot locate
// schemas compiled only inside the extension's private directory on Bluefin).
// The extension is considered present when dconf is available and any command key (command1..command99)
// under /org/gnome/shell/extensions/custom-command-list/ has a distro default or user-set value.
// A missing dconf tool or absence of custom-command-list keys returns false, nil (supported no-op
// for Plasma/non-GNOME or environments without the extension).
func available(ctx context.Context) (bool, error) {
	m, err := load(ctx)
	if err != nil {
		return false, err
	}
	return len(m.entries) > 0, nil
}

// ParseEntry parses a GVariant (sssb) tuple representation into an Entry.
// Example: ('Terminal', 'ptyxis --new-window', 'utilities-terminal-symbolic', true)
func ParseEntry(raw string) (Entry, error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return Entry{}, fmt.Errorf("devmenu: entry tuple must start with '(' and end with ')'")
	}
	s = strings.TrimSpace(s[1 : len(s)-1])

	label, s, err := parseGVariantString(s)
	if err != nil {
		return Entry{}, fmt.Errorf("devmenu: parsing label: %w", err)
	}
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, ",") {
		return Entry{}, errors.New("devmenu: expected comma after label")
	}
	s = strings.TrimSpace(s[1:])

	cmd, s, err := parseGVariantString(s)
	if err != nil {
		return Entry{}, fmt.Errorf("devmenu: parsing command: %w", err)
	}
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, ",") {
		return Entry{}, errors.New("devmenu: expected comma after command")
	}
	s = strings.TrimSpace(s[1:])

	icon, s, err := parseGVariantString(s)
	if err != nil {
		return Entry{}, fmt.Errorf("devmenu: parsing icon: %w", err)
	}
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, ",") {
		return Entry{}, errors.New("devmenu: expected comma after icon")
	}
	s = strings.TrimSpace(s[1:])

	boolStr := strings.TrimSpace(s)
	var visible bool
	switch strings.ToLower(boolStr) {
	case "true":
		visible = true
	case "false":
		visible = false
	default:
		return Entry{}, fmt.Errorf("devmenu: invalid boolean value %q", boolStr)
	}

	return Entry{
		Label:   label,
		Command: cmd,
		Icon:    icon,
		Visible: visible,
	}, nil
}

// parseGVariantString parses a single- or double-quoted string from s,
// unescaping backslash escapes, and returns the parsed string and the rest of s.
func parseGVariantString(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return "", "", errors.New("expected quoted string")
	}
	quote := s[0]
	if quote != '\'' && quote != '"' {
		return "", "", fmt.Errorf("expected string beginning with quote, got %c", quote)
	}

	var sb strings.Builder
	escaped := false
	i := 1
	for ; i < len(s); i++ {
		c := s[i]
		if escaped {
			switch c {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case 'a':
				sb.WriteByte('\a')
			case 'b':
				sb.WriteByte('\b')
			case 'f':
				sb.WriteByte('\f')
			case 'v':
				sb.WriteByte('\v')
			default:
				sb.WriteByte(c)
			}
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if c == quote {
			return sb.String(), s[i+1:], nil
		}
		sb.WriteByte(c)
	}
	return "", "", errors.New("unterminated string literal")
}

// FormatEntry formats an Entry into a GVariant (sssb) tuple string.
func FormatEntry(e Entry) string {
	return fmt.Sprintf("('%s', '%s', '%s', %t)",
		escapeGVariant(e.Label),
		escapeGVariant(e.Command),
		escapeGVariant(e.Icon),
		e.Visible,
	)
}

func escapeGVariant(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch b {
		case '\\':
			sb.WriteString(`\\`)
		case '\'':
			sb.WriteString(`\'`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		case '\r':
			sb.WriteString(`\r`)
		case '\a':
			sb.WriteString(`\a`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		case '\v':
			sb.WriteString(`\v`)
		default:
			sb.WriteByte(b)
		}
	}
	return sb.String()
}

// Apply updates the visibility of developer entries (Terminal, Containers)
// in the Custom Command Menu extension according to developerMode.
// Missing extension or non-GNOME environment is a supported no-op.
func Apply(ctx context.Context, developerMode bool) error {
	m, err := load(ctx)
	if err != nil {
		return err
	}
	entries := m.entries
	if len(entries) == 0 {
		return nil
	}

	for i := 1; i <= MaxCommands; i++ {
		key := fmt.Sprintf("command%d", i)
		cur, ok := entries[key]
		if !ok {
			continue
		}
		dconfKeyPath := DconfPath + key

		entry, err := ParseEntry(cur)
		if err != nil {
			continue
		}

		if !IsDeveloperLabel(entry.Label) {
			continue
		}

		targetVisible := developerMode
		desired := Entry{
			Label:   entry.Label,
			Command: entry.Command,
			Icon:    entry.Icon,
			Visible: targetVisible,
		}
		desiredFormatted := FormatEntry(desired)

		defaultRaw, err := runCommand(ctx, "dconf", "read", "-d", dconfKeyPath)
		if err != nil {
			return fmt.Errorf("devmenu: reading default %s: %w: %s", key, err, strings.TrimSpace(defaultRaw))
		}
		def := strings.TrimSpace(defaultRaw)

		defMatches := false
		if def != "" {
			if defEntry, defErr := ParseEntry(def); defErr == nil {
				if defEntry == desired {
					defMatches = true
				}
			}
		}

		// Compare parsed entries, never their textual forms: `dconf dump` quoting
		// and spacing differ from FormatEntry's, so a string comparison would write
		// a semantically identical value into the user layer and pin it.
		if entry == desired {
			continue
		}

		if defMatches {
			if dryrun.Enabled() {
				log.Printf("[DRY-RUN] would reset Custom Command Menu %s to default", key)
			} else {
				out, err := runCommand(ctx, "dconf", "reset", dconfKeyPath)
				if err != nil {
					return fmt.Errorf("devmenu: resetting %s: %w: %s", key, err, strings.TrimSpace(out))
				}
			}
			continue
		}

		if dryrun.Enabled() {
			log.Printf("[DRY-RUN] would set Custom Command Menu %s visible=%t", key, targetVisible)
		} else {
			out, err := runCommand(ctx, "dconf", "write", dconfKeyPath, desiredFormatted)
			if err != nil {
				return fmt.Errorf("devmenu: writing %s: %w: %s", key, err, strings.TrimSpace(out))
			}
		}
	}

	return nil
}

// findAskBluefin returns the command key and entry of the owned Ask Bluefin
// slot, or ok=false when the menu has none.
func findAskBluefin(entries map[string]string) (key string, entry Entry, ok bool) {
	for i := 1; i <= MaxCommands; i++ {
		key := fmt.Sprintf("command%d", i)
		cur, found := entries[key]
		if !found {
			continue
		}
		entry, err := ParseEntry(cur)
		if err != nil {
			continue
		}
		if IsAskBluefin(entry) {
			return key, entry, true
		}
	}
	return "", Entry{}, false
}

// AskBluefinState reports whether the owned Ask Bluefin entry is available in
// the Custom Command Menu extension and whether the panel menu actually shows
// it. The extension renders only slots listed in command-order, so a slot
// whose own visible flag is true but which command-order omits is not visible
// (Dakota's distro layer moved Ask Bluefin to command12 while inheriting an
// order of [1..11]).
func AskBluefinState(ctx context.Context) (available bool, visible bool, err error) {
	m, err := load(ctx)
	if err != nil {
		return false, false, err
	}
	key, entry, ok := findAskBluefin(m.entries)
	if !ok {
		return false, false, nil
	}
	order, err := ParseCommandOrder(m.order)
	if err != nil {
		return true, false, err
	}
	return true, entry.Visible && slices.Contains(order, slotNumber(key)), nil
}

// SetAskBluefinVisible updates the visibility of the owned Ask Bluefin entry in
// the Custom Command Menu extension.
// It modifies only an entry whose title and command match ChairLift's owned identity.
// Showing the entry also lists its slot in command-order when the order omits
// it, appending the slot and preserving the rest of the order; hiding it
// changes only the entry's visible flag. For both keys, a value that matches
// the distro default (read via `dconf read -d`) resets the user layer so
// distro defaults are revealed rather than copied into user state.
func SetAskBluefinVisible(ctx context.Context, visible bool) error {
	m, err := load(ctx)
	if err != nil {
		return err
	}
	key, entry, ok := findAskBluefin(m.entries)
	if !ok {
		return nil
	}

	desired := entry
	desired.Visible = visible
	if entry != desired {
		defaultRaw, err := runCommand(ctx, "dconf", "read", "-d", DconfPath+key)
		if err != nil {
			return fmt.Errorf("devmenu: reading default %s: %w: %s", key, err, strings.TrimSpace(defaultRaw))
		}
		defEntry, defErr := ParseEntry(strings.TrimSpace(defaultRaw))
		if err := settleKey(ctx, key, FormatEntry(desired), defErr == nil && defEntry == desired, fmt.Sprintf("visible=%t", visible)); err != nil {
			return err
		}
	}
	if !visible {
		return nil
	}

	order, err := ParseCommandOrder(m.order)
	if err != nil {
		return err
	}
	slot := slotNumber(key)
	if slices.Contains(order, slot) {
		return nil
	}
	desiredOrder := append(slices.Clone(order), slot)
	defaultRaw, err := runCommand(ctx, "dconf", "read", "-d", DconfPath+CommandOrderKey)
	if err != nil {
		return fmt.Errorf("devmenu: reading default %s: %w: %s", CommandOrderKey, err, strings.TrimSpace(defaultRaw))
	}
	defOrder, defErr := ParseCommandOrder(defaultRaw)
	formatted := FormatCommandOrder(desiredOrder)
	return settleKey(ctx, CommandOrderKey, formatted, defErr == nil && slices.Equal(defOrder, desiredOrder), "to "+formatted)
}

// settleKey moves one key to its desired value in the user layer: a reset when
// the desired value is the distro default, otherwise a write. Dry-run logs
// change and mutates nothing.
func settleKey(ctx context.Context, key, desired string, matchesDefault bool, change string) error {
	path := DconfPath + key
	if matchesDefault {
		if dryrun.Enabled() {
			log.Printf("[DRY-RUN] would reset Custom Command Menu %s to default", key)
			return nil
		}
		if out, err := runCommand(ctx, "dconf", "reset", path); err != nil {
			return fmt.Errorf("devmenu: resetting %s: %w: %s", key, err, strings.TrimSpace(out))
		}
		return nil
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set Custom Command Menu %s %s", key, change)
		return nil
	}
	if out, err := runCommand(ctx, "dconf", "write", path, desired); err != nil {
		return fmt.Errorf("devmenu: writing %s: %w: %s", key, err, strings.TrimSpace(out))
	}
	return nil
}
