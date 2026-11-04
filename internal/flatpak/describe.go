package flatpak

import (
	"context"
	"errors"
	"strings"
)

// DescribeApps returns the AppStream summary of each application ID, keyed by
// ID. It reads the remotes' locally cached AppStream data through
// `flatpak search`, which neither installs nor refreshes anything, so it
// works offline and never touches the network. `flatpak search` takes one
// term and matches substrings, so each ID is queried alone and only the row
// whose application ID matches exactly is kept. IDs that are not plain
// application IDs are skipped. An ID with no cached metadata is simply
// absent from the result; command failures are joined into the error while
// the IDs that did resolve are still returned.
func DescribeApps(ctx context.Context, ids []string) (map[string]string, error) {
	descriptions := make(map[string]string)
	seen := make(map[string]bool)
	var problems []error
	for _, id := range ids {
		if seen[id] || !validAppID(id) {
			continue
		}
		seen[id] = true
		if err := ctx.Err(); err != nil {
			problems = append(problems, err)
			break
		}
		output, err := runFlatpakCommandCtx(ctx, "search", "--columns=application,description", id)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if description := parseSearchDescription(output, id); description != "" {
			descriptions[id] = description
		}
	}
	return descriptions, errors.Join(problems...)
}

// parseSearchDescription returns the description column of the tab-separated
// row whose application column is exactly id. Headers, "No matches found"
// and substring matches for other applications never equal id.
func parseSearchDescription(output, id string) string {
	for _, line := range strings.Split(output, "\n") {
		application, description, ok := strings.Cut(line, "\t")
		if !ok || strings.TrimSpace(application) != id {
			continue
		}
		if description = strings.Join(strings.Fields(description), " "); description != "" {
			return description
		}
	}
	return ""
}

// validAppID accepts reverse-DNS application IDs only, never options, refs
// or paths, so a Brewfile entry cannot inject a flatpak argument.
func validAppID(id string) bool {
	if id == "" || strings.HasPrefix(id, "-") || strings.HasPrefix(id, ".") || !strings.Contains(id, ".") || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'
		if !allowed {
			return false
		}
	}
	return true
}
