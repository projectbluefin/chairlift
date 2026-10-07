package gaming

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The read-only places in which the OS image names the Flatpaks it ships
// system-wide. Remove Selected leaves a component's system copy in place when
// any of them declares it: undoing gaming mode must not take a distro default
// (Flatseal, on Bluefin and Dakota) away from every account on the computer,
// and Flatpak records such an uninstall as a permanent opt-out, so the next
// `flatpak preinstall` does not bring it back either.
var (
	// preinstallDirs are Flatpak's own vendor declaration directories
	// (flatpak-preinstall(1)). A file in the later directory overrides the
	// file of the same name in the earlier one.
	preinstallDirs = []string{"/usr/share/flatpak/preinstall.d", "/etc/flatpak/preinstall.d"}
	// imageFlatpakBrewfiles are the Bluefin-family default system Flatpak
	// sets, which the installer and `ujust install-system-flatpaks` install
	// system-wide.
	imageFlatpakBrewfiles = []string{"/usr/share/ublue-os/homebrew/system-flatpaks.Brewfile"}
)

// imageShipped returns the IDs of the Flatpaks the image declares it ships.
// A missing declaration file or directory declares nothing; any other read
// error is returned, because removal must fail closed rather than treat an
// unreadable declaration as "not shipped".
func imageShipped() (map[string]bool, error) {
	shipped := map[string]bool{}

	files := map[string]string{}
	var names []string
	for _, dir := range preinstallDirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".preinstall") {
				continue
			}
			if _, seen := files[name]; !seen {
				names = append(names, name)
			}
			files[name] = filepath.Join(dir, name)
		}
	}
	for _, name := range names {
		if err := scanLines(files[name], func(lines []string) { parsePreinstall(lines, shipped) }); err != nil {
			return nil, err
		}
	}

	for _, path := range imageFlatpakBrewfiles {
		err := scanLines(path, func(lines []string) { parseBrewfileFlatpaks(lines, shipped) })
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return shipped, nil
}

func scanLines(path string, parse func([]string)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, strings.TrimSpace(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	parse(lines)
	return nil
}

// parsePreinstall adds every `[Flatpak Preinstall NAME]` group whose Install
// key is not false. Install defaults to true (flatpak-preinstall(1)).
func parsePreinstall(lines []string, shipped map[string]bool) {
	const prefix = "[Flatpak Preinstall "
	group := ""
	install := map[string]bool{}
	var order []string
	for _, line := range lines {
		if strings.HasPrefix(line, "[") {
			group = ""
			if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, "]") {
				group = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, prefix), "]"))
				order = append(order, group)
				install[group] = true
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if group == "" || !ok || strings.TrimSpace(key) != "Install" {
			continue
		}
		if enabled, err := strconv.ParseBool(strings.TrimSpace(value)); err == nil {
			install[group] = enabled
		}
	}
	for _, id := range order {
		if install[id] {
			shipped[id] = true
		}
	}
}

// parseBrewfileFlatpaks adds the ID of every `flatpak "ID"` entry.
func parseBrewfileFlatpaks(lines []string, shipped map[string]bool) {
	for _, line := range lines {
		rest, ok := strings.CutPrefix(line, "flatpak ")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" || (rest[0] != '"' && rest[0] != '\'') {
			continue
		}
		if id, _, closed := strings.Cut(rest[1:], rest[:1]); closed && id != "" {
			shipped[id] = true
		}
	}
}
