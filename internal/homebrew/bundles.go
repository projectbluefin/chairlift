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
			bundles = append(bundles, Bundle{
				Name:        name,
				Description: description,
				Path:        path,
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
