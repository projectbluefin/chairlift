package devmenu

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
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

// TargetLabels are the Custom Command Menu labels managed by Developer Mode.
var TargetLabels = []string{"Terminal", "Containers"}

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

// scan reads every command key under the extension's dconf path with a single
// `dconf dump`, returning key name (command1..command99) to resolved tuple value.
// dconf resolves through the whole profile, so the dump carries distro defaults
// (e.g. Bluefin's 04-bluefin-custom-command-menu) as well as user-layer overrides;
// one subprocess therefore replaces a per-key read of all 99 keys.
func scan(ctx context.Context) (map[string]string, error) {
	out, err := runCommand(ctx, "dconf", "dump", DconfPath)
	if err != nil {
		return nil, fmt.Errorf("devmenu: dumping %s: %w: %s", DconfPath, err, strings.TrimSpace(out))
	}
	return parseDump(out), nil
}

// parseDump parses `dconf dump` keyfile output, keeping only command keys that
// live directly under the dumped path.
func parseDump(out string) map[string]string {
	entries := make(map[string]string)
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
		if value == "" || !isCommandKey(key) {
			continue
		}
		entries[key] = value
	}
	return entries
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
func load(ctx context.Context) (map[string]string, error) {
	if _, err := lookPath("dconf"); err != nil {
		return nil, nil
	}
	return scan(ctx)
}

// available reports whether the Custom Command Menu extension is present and can be managed.
// It decides availability from dconf alone without relying on gsettings (which cannot locate
// schemas compiled only inside the extension's private directory on Bluefin).
// The extension is considered present when dconf is available and any command key (command1..command99)
// under /org/gnome/shell/extensions/custom-command-list/ has a distro default or user-set value.
// A missing dconf tool or absence of custom-command-list keys returns false, nil (supported no-op
// for Plasma/non-GNOME or environments without the extension).
func available(ctx context.Context) (bool, error) {
	entries, err := load(ctx)
	if err != nil {
		return false, err
	}
	return len(entries) > 0, nil
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
	entries, err := load(ctx)
	if err != nil {
		return err
	}
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
