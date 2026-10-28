package homebrew

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/projectbluefin/chairlift/internal/flatpak"
)

// describeFlatpakApps reads Flatpak application summaries; tests replace it.
var describeFlatpakApps = flatpak.DescribeApps

// DescribeBundleItems returns a copy enriched with short descriptions: formulae
// and casks from Homebrew, Flatpak applications from Flatpak's cached
// AppStream data. App Store and VS Code entries and missing metadata retain an
// empty description. Lookups are batched by namespace and bounded by one
// shared read timeout. This does not install packages or trust taps; Homebrew
// may fetch its package metadata, while the Flatpak lookup stays offline.
// `brew info` never auto-taps: names in untapped taps fail and are reported
// unavailable. A failed batch is retried per name so one unresolvable entry
// does not blank the rest of its namespace.
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
		var names []string
		seen := make(map[string]bool)
		for _, item := range items {
			if item.Kind == kind && validDescriptionToken(item.Name) && !seen[item.Name] {
				names = append(names, item.Name)
				seen[item.Name] = true
			}
		}
		if len(names) == 0 {
			continue
		}
		descriptions, err := describeNames(ctx, flag, kind, names)
		if err != nil && len(names) > 1 && ctx.Err() == nil {
			// Homebrew emits no JSON when any single name is unresolvable
			// (untapped tap, renamed formula, typo), so retry each name alone
			// to keep the resolvable ones.
			descriptions = make(map[string]string)
			var nameErrs []error
			for _, name := range names {
				if ctx.Err() != nil {
					nameErrs = append(nameErrs, ctx.Err())
					break
				}
				one, oneErr := describeNames(ctx, flag, kind, []string{name})
				if oneErr != nil {
					nameErrs = append(nameErrs, oneErr)
					continue
				}
				for key, value := range one {
					descriptions[key] = value
				}
			}
			err = errors.Join(nameErrs...)
		}
		if err != nil {
			problems = append(problems, err)
		}
		for i := range result {
			if result[i].Kind == kind && seen[result[i].Name] {
				result[i].Description = descriptions[result[i].Name]
			}
		}
	}
	if err := describeFlatpaks(ctx, result); err != nil {
		problems = append(problems, err)
	}
	return result, errors.Join(problems...)
}

func describeFlatpaks(ctx context.Context, items []BundleItem) error {
	var ids []string
	for _, item := range items {
		if item.Kind == BundleItemFlatpak {
			ids = append(ids, item.Name)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	descriptions, err := describeFlatpakApps(ctx, ids)
	for i := range items {
		if items[i].Kind == BundleItemFlatpak {
			items[i].Description = descriptions[items[i].Name]
		}
	}
	return err
}

func describeNames(ctx context.Context, flag string, kind BundleItemKind, names []string) (map[string]string, error) {
	output, err := runBrewCommandCtx(ctx, append([]string{"info", "--json=v2", flag}, names...)...)
	if err != nil {
		return nil, err
	}
	return parseBundleDescriptions(output, kind)
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
