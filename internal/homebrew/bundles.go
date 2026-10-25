package homebrew

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const maxBundleDescriptionBytes = 64 * 1024

// Bundle describes one installable Brewfile discovered from configured
// bundle directories.
type Bundle struct {
	Name        string
	Description string
	Path        string
	ItemCount   int
}

// AvailableBundles discovers regular *.Brewfile entries immediately inside
// each configured directory. Missing directories are ignored because bundle
// paths may target a different distribution variant. Other path and file
// errors are joined and returned alongside any bundles that were discovered.
//
// Exact duplicate paths are emitted once. Same-named files in different
// directories remain distinct and are ordered by name, then absolute path.
func AvailableBundles(paths []string) ([]Bundle, error) {
	var (
		bundles  []Bundle
		problems []error
		seen     = make(map[string]struct{})
	)

	for _, configuredPath := range paths {
		if configuredPath == "" {
			problems = append(problems, errors.New("bundle directory path is empty"))
			continue
		}

		dir, err := filepath.Abs(configuredPath)
		if err != nil {
			problems = append(problems, fmt.Errorf("resolve bundle directory %q: %w", configuredPath, err))
			continue
		}
		dir = filepath.Clean(dir)

		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			problems = append(problems, fmt.Errorf("read bundle directory %q: %w", dir, err))
			continue
		}

		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".Brewfile") {
				continue
			}

			path := filepath.Join(dir, entry.Name())
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}

			info, err := os.Stat(path)
			if err != nil {
				problems = append(problems, fmt.Errorf("inspect Brewfile %q: %w", path, err))
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}

			description, err := readBundleDescription(path)
			if err != nil {
				problems = append(problems, fmt.Errorf("read Brewfile %q: %w", path, err))
				continue
			}

			name := strings.TrimSuffix(entry.Name(), ".Brewfile")
			if name == "" {
				name = entry.Name()
			}
			itemCount, _ := countBundleItems(path)
			bundles = append(bundles, Bundle{
				Name:        name,
				Description: description,
				Path:        path,
				ItemCount:   itemCount,
			})
		}
	}

	sort.Slice(bundles, func(i, j int) bool {
		if bundles[i].Name != bundles[j].Name {
			return bundles[i].Name < bundles[j].Name
		}
		return bundles[i].Path < bundles[j].Path
	})

	return bundles, errors.Join(problems...)
}

func readBundleDescription(path string) (description string, resultErr error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		resultErr = errors.Join(resultErr, file.Close())
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), maxBundleDescriptionBytes)

	var commentLines []string
	inCommentBlock := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			inCommentBlock = true
			content := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if content != "" {
				commentLines = append(commentLines, content)
			}
		} else if line == "" && !inCommentBlock {
			// Skip leading empty lines before any comment block
			continue
		} else {
			// Hit the end of the leading comment block
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	if len(commentLines) > 0 {
		return strings.Join(commentLines, " "), nil
	}

	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".Brewfile")
	return humanizeBundleName(name), nil
}

// humanizeBundleName converts a bundle filename or identifier into a human-readable title.
func humanizeBundleName(name string) string {
	cleaned := strings.ReplaceAll(name, "-", " ")
	cleaned = strings.ReplaceAll(cleaned, "_", " ")
	words := strings.Fields(cleaned)
	if len(words) == 0 {
		return name
	}
	acronyms := map[string]string{
		"cli": "CLI",
		"ai":  "AI",
		"k8s": "K8s",
		"ide": "IDE",
		"dx":  "DX",
		"gui": "GUI",
		"vm":  "VM",
		"os":  "OS",
	}
	for i, w := range words {
		lower := strings.ToLower(w)
		if acr, ok := acronyms[lower]; ok {
			words[i] = acr
		} else {
			runes := []rune(lower)
			if len(runes) > 0 {
				runes[0] = unicode.ToUpper(runes[0])
			}
			words[i] = string(runes)
		}
	}
	return strings.Join(words, " ")
}

// BundleItemKind names the Brewfile directive that introduced an entry.
// Brewfile recognises five entry kinds (brew, cask, flatpak, mas, vscode)
// and a tap directive that has nothing to install on its own; taps are not
// items, so the kind stays the installable set.
type BundleItemKind string

const (
	BundleItemBrew    BundleItemKind = "brew"
	BundleItemCask    BundleItemKind = "cask"
	BundleItemFlatpak BundleItemKind = "flatpak"
	BundleItemMas     BundleItemKind = "mas"
	BundleItemVSCode  BundleItemKind = "vscode"
)

// BundleItem is one installable entry in a Brewfile. The on-disk identifier
// is whatever the Brewfile spells in quotes (a formula name, a cask token,
// a Flatpak ref, an App Store id, a VS Code extension id); Kind records
// which directive it came from so a presenter can group or label the list.
type BundleItem struct {
	Name        string
	Kind        BundleItemKind
	Description string
}

// AvailableItems is the cap used by both the bundle scanner and the
// presenter. Reading more would let a runaway file dominate a GTK widget
// and silently crop a real collection's tail; the same value in both
// places keeps the row count honest.
const AvailableItems = 256

// bundleEntryPrefixes lists the installable Brewfile directives. The order
// matches BundleItemKind, so the presenter's grouping reads in the same
// sequence a reader meets in the file: formulae first, then graphical apps,
// then non-Homebrew installables. Keeping the prefix list in one place
// removes any way for the count, the presenter, and the parser to disagree
// on what counts as an entry.
var bundleEntryPrefixes = []string{"brew ", "cask ", "flatpak ", "mas ", "vscode "}

// BundleContents parses a Brewfile and returns every installable entry it
// recognises, in source order, deduplicated by (Kind, Name). A directive
// without a recognisable token (a bare `tap "owner/repo"` or a malformed
// line) is skipped silently; an entry outside the five installable kinds
// is also skipped. The cap protects the parser and the UI from runaway
// files; a Brewfile that exceeds it is truncated at AvailableItems entries.
// An open failure returns no items; a read failure part-way through returns
// the entries read so far alongside the error.
func BundleContents(path string) ([]BundleItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var items []BundleItem
	seen := make(map[BundleItemKind]map[string]struct{})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), maxBundleDescriptionBytes)
	for scanner.Scan() {
		if len(items) >= AvailableItems {
			break
		}
		kind, name, ok := parseBundleItemLine(scanner.Text())
		if !ok {
			continue
		}
		bucket := seen[kind]
		if bucket == nil {
			bucket = make(map[string]struct{})
			seen[kind] = bucket
		}
		if _, dup := bucket[name]; dup {
			continue
		}
		bucket[name] = struct{}{}
		items = append(items, BundleItem{Name: name, Kind: kind})
	}
	if err := scanner.Err(); err != nil {
		return items, err
	}
	return items, nil
}

// parseBundleItemLine returns the (kind, name) of a Brewfile line, or
// ok=false when the line is empty, a comment, a continuation, a directive
// Brewfile does not install (tap), or a recognised directive whose quoted
// token cannot be recovered. The scanner is permissive on purpose: a
// Brewfile that ships a misspelt entry must not derail the rest of the
// list, and a Brewfile that quotes a name in single quotes reads the same
// way as one that uses double quotes.
func parseBundleItemLine(raw string) (BundleItemKind, string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	for _, prefix := range bundleEntryPrefixes {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		kind, err := classifyBundlePrefix(prefix)
		if err != nil {
			return "", "", false
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
		name, ok := bundleEntryName(rest)
		if !ok {
			return "", "", false
		}
		return kind, name, true
	}
	return "", "", false
}

func classifyBundlePrefix(prefix string) (BundleItemKind, error) {
	switch prefix {
	case "brew ":
		return BundleItemBrew, nil
	case "cask ":
		return BundleItemCask, nil
	case "flatpak ":
		return BundleItemFlatpak, nil
	case "mas ":
		return BundleItemMas, nil
	case "vscode ":
		return BundleItemVSCode, nil
	default:
		return "", fmt.Errorf("unknown bundle entry prefix %q", prefix)
	}
}

// bundleEntryName recovers a single quoted token from the remainder of a
// Brewfile directive. Brewfile accepts double or single quotes around an
// identifier and may follow the closing quote with arguments on the same
// line (`brew "jq", link: true`); only the leading token matters for the
// on-disk identifier. A line that opens with no quote, or that closes its
// quote only after the line ends, is rejected so a malformed entry never
// turns into an empty row.
func bundleEntryName(rest string) (string, bool) {
	if rest == "" {
		return "", false
	}
	quote := rest[0]
	if quote != '"' && quote != '\'' {
		return "", false
	}
	close := strings.IndexByte(rest[1:], quote)
	if close < 0 {
		return "", false
	}
	return rest[1 : 1+close], true
}

func countBundleItems(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	seen := make(map[BundleItemKind]map[string]struct{})
	count := 0
	for scanner.Scan() {
		kind, name, ok := parseBundleItemLine(scanner.Text())
		if !ok {
			continue
		}
		bucket := seen[kind]
		if bucket == nil {
			bucket = make(map[string]struct{})
			seen[kind] = bucket
		}
		if _, dup := bucket[name]; dup {
			continue
		}
		bucket[name] = struct{}{}
		count++
	}
	return count, scanner.Err()
}
