package homebrew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// DescribeBundleItems returns a copy enriched with Homebrew's short descriptions.
// Non-Homebrew entries and missing metadata retain an empty description. Lookups
// are batched by namespace and bounded by one shared read timeout. This does not
// install packages or trust taps; Homebrew may fetch its package metadata.
func DescribeBundleItems(ctx context.Context, items []BundleItem) ([]BundleItem, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	result := append([]BundleItem(nil), items...)
	var problems []error
	for _, kind := range []BundleItemKind{BundleItemBrew, BundleItemCask} {
		flag := "--formula"
		if kind == BundleItemCask {
			flag = "--cask"
		}
		args := []string{"info", "--json=v2", flag}
		seen := make(map[string]bool)
		for _, item := range items {
			if item.Kind == kind && validDescriptionToken(item.Name) && !seen[item.Name] {
				args = append(args, item.Name)
				seen[item.Name] = true
			}
		}
		if len(seen) == 0 {
			continue
		}
		output, err := runBrewCommandCtx(ctx, args...)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		descriptions, err := parseBundleDescriptions(output, kind)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		for i := range result {
			if result[i].Kind == kind && seen[result[i].Name] {
				result[i].Description = descriptions[result[i].Name]
			}
		}
	}
	return result, errors.Join(problems...)
}

// Accept package identifiers only, never options, URLs or local Ruby paths.
func validDescriptionToken(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || strings.HasSuffix(name, ".rb") {
		return false
	}
	for _, r := range name {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+@", r)
		if !allowed {
			return false
		}
	}
	return true
}

func parseBundleDescriptions(output string, kind BundleItemKind) (map[string]string, error) {
	type metadata struct {
		Name      string   `json:"name"`
		FullName  string   `json:"full_name"`
		Token     string   `json:"token"`
		FullToken string   `json:"full_token"`
		Aliases   []string `json:"aliases"`
		Desc      string   `json:"desc"`
	}
	var document struct {
		Formulae []metadata `json:"formulae"`
		Casks    []struct {
			Token     string `json:"token"`
			FullToken string `json:"full_token"`
			Desc      string `json:"desc"`
		} `json:"casks"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		return nil, fmt.Errorf("read collection descriptions: %w", err)
	}
	entries := document.Formulae
	if kind == BundleItemCask {
		entries = nil
		for _, cask := range document.Casks {
			entries = append(entries, metadata{Token: cask.Token, FullToken: cask.FullToken, Desc: cask.Desc})
		}
	}
	descriptions := make(map[string]string)
	for _, entry := range entries {
		names := []string{entry.Name, entry.FullName}
		if kind == BundleItemCask {
			names = []string{entry.Token, entry.FullToken}
		} else {
			names = append(names, entry.Aliases...)
		}
		for _, name := range names {
			if name != "" {
				descriptions[name] = strings.Join(strings.Fields(entry.Desc), " ")
			}
		}
	}
	return descriptions, nil
}
