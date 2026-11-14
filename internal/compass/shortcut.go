package compass

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/projectbluefin/chairlift/internal/deskenv"
)

const (
	// Binding is the default GNOME key combination used to activate Compass.
	Binding        = "<Super>space"
	mediaSchema    = "org.gnome.settings-daemon.plugins.media-keys"
	shortcutPath   = "/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/chairlift-compass/"
	shortcutSchema = mediaSchema + ".custom-keybinding:" + shortcutPath
	inputSchema    = "org.gnome.desktop.wm.keybindings"
	inputKey       = "switch-input-source"
	displacedKey   = shortcutPath + "displaced-input-binding"
)

var (
	runCmd = func(ctx context.Context, name string, args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
		}
		return strings.TrimSpace(string(out)), nil
	}
	lookCmd = exec.LookPath
)

// ShortcutAvailable reports whether GNOME and its configuration tools (gsettings, dconf)
// are available to manage the Super+Space shortcut on this system.
func ShortcutAvailable() bool {
	if deskenv.Detect() != deskenv.GNOME {
		return false
	}
	for _, tool := range []string{"gsettings", "dconf"} {
		if _, err := lookCmd(tool); err != nil {
			return false
		}
	}
	return true
}

// IsShortcutBound reports whether Compass is currently bound to Super+Space.
func IsShortcutBound(ctx context.Context) (bool, error) {
	if !ShortcutAvailable() {
		return false, nil
	}
	paths, err := getList(ctx, mediaSchema, "custom-keybindings")
	if err != nil || !slices.Contains(paths, shortcutPath) {
		return false, err
	}
	command, err := get(ctx, shortcutSchema, "command")
	if err != nil {
		return false, err
	}
	key, err := get(ctx, shortcutSchema, "binding")
	if err != nil {
		return false, err
	}
	inputs, err := getList(ctx, inputSchema, inputKey)
	bound := command == quote(launchCommand) && key == quote(Binding) && !slices.ContainsFunc(inputs, isBinding)
	return bound, err
}

// SetShortcut configures or removes the Super+Space keybinding for Compass.
// When enabled, it binds flatpak run org.tunaos.compass start.
func SetShortcut(ctx context.Context, enable bool) error {
	if !ShortcutAvailable() {
		return nil
	}
	paths, err := getList(ctx, mediaSchema, "custom-keybindings")
	if err != nil {
		return err
	}
	if enable {
		if !slices.Contains(paths, shortcutPath) {
			paths = append(paths, shortcutPath)
			if err := setList(ctx, mediaSchema, "custom-keybindings", paths); err != nil {
				return err
			}
		}
		for _, pair := range [][2]string{
			{"name", quote("Compass Launcher")},
			{"command", quote(launchCommand)},
			{"binding", quote(Binding)},
		} {
			if err := set(ctx, shortcutSchema, pair[0], pair[1]); err != nil {
				return err
			}
		}
		inputs, err := getList(ctx, inputSchema, inputKey)
		if err != nil {
			return err
		}
		if slices.ContainsFunc(inputs, isBinding) {
			if _, err := runCmd(ctx, "dconf", "write", displacedKey, "true"); err != nil {
				return err
			}
			filtered := slices.DeleteFunc(inputs, isBinding)
			if err := setList(ctx, inputSchema, inputKey, filtered); err != nil {
				return err
			}
		}
	} else {
		if slices.Contains(paths, shortcutPath) {
			filtered := slices.DeleteFunc(paths, func(p string) bool { return p == shortcutPath })
			if err := setList(ctx, mediaSchema, "custom-keybindings", filtered); err != nil {
				return err
			}
		}
		displacedVal, err := runCmd(ctx, "dconf", "read", displacedKey)
		if err != nil {
			return err
		}
		wasDisplaced := strings.TrimSpace(displacedVal) == "true" || unquote(strings.TrimSpace(displacedVal)) == "true"
		if wasDisplaced {
			inputs, err := getList(ctx, inputSchema, inputKey)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(inputs, isBinding) {
				if err := setList(ctx, inputSchema, inputKey, append(inputs, Binding)); err != nil {
					return err
				}
			}
		}
		if _, err := runCmd(ctx, "dconf", "reset", "-f", shortcutPath); err != nil {
			return err
		}
	}
	return nil
}

func get(ctx context.Context, schema, key string) (string, error) {
	return runCmd(ctx, "gsettings", "get", schema, key)
}

func set(ctx context.Context, schema, key, value string) error {
	_, err := runCmd(ctx, "gsettings", "set", schema, key, value)
	return err
}

func getList(ctx context.Context, schema, key string) ([]string, error) {
	out, err := get(ctx, schema, key)
	if err != nil {
		return nil, err
	}
	return parseList(out), nil
}

func setList(ctx context.Context, schema, key string, items []string) error {
	return set(ctx, schema, key, formatList(items))
}

func parseList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	body := strings.TrimSpace(s[1 : len(s)-1])
	if body == "" {
		return nil
	}
	var res []string
	for _, item := range strings.Split(body, ",") {
		item = strings.TrimSpace(item)
		if unquoted := unquote(item); unquoted != "" {
			res = append(res, unquoted)
		}
	}
	return res
}

func formatList(items []string) string {
	if len(items) == 0 {
		return "@as []"
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = quote(item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func quote(s string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		inner := s[1 : len(s)-1]
		return strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\\`, `\`).Replace(inner)
	}
	return s
}

func isBinding(s string) bool {
	return strings.EqualFold(s, Binding) || strings.EqualFold(s, "<Mod4>space")
}

// SuppressInternalHotkey disables Compass's built-in global shortcut request
// in its flatpak configuration so the desktop never pops up a GlobalShortcuts portal
// permission prompt on startup.
func SuppressInternalHotkey(homeDir string) error {
	if homeDir == "" {
		return nil
	}
	dir := filepath.Join(homeDir, ".var", "app", AppID, "config", "compass")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cfgFile := filepath.Join(dir, "compass.json")
	data, err := os.ReadFile(cfgFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var root map[string]any
	if err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("decoding Compass configuration: %w", err)
		}
	}
	if root == nil {
		root = make(map[string]any)
	}
	launcher, _ := root["launcher"].(map[string]any)
	if launcher == nil {
		launcher = make(map[string]any)
	}
	// Setting hotkey to "" tells Compass's desired() shortcuts reconciler to bind nothing.
	launcher["hotkey"] = ""
	root["launcher"] = launcher

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgFile, append(out, '\n'), 0o644)
}
