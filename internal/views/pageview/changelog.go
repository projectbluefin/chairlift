package pageview

import (
	"fmt"

	"github.com/projectbluefin/chairlift/internal/sbom"
)

// ChangelogRow returns the drill-down row inside the system-update expander,
// before anything has been fetched. staged is false when there is no update
// to compare against.
func ChangelogRow(staged bool) Row {
	row := Row{Title: "What's changing"}
	if !staged {
		row.Subtitle = "Available once an update is ready to install"
		return row
	}
	// The fetch is tens of megabytes per side, so it is never automatic —
	// the subtitle has to say that pressing the button costs a download.
	row.Subtitle = "See which programs change, by comparing your version with the new one. This is a large download."
	return row
}

// ChangelogSummary returns the subtitle describing a completed comparison.
func ChangelogSummary(result sbom.Result) string {
	if result.Empty() {
		return "No programs change between the two versions"
	}

	parts := make([]string, 0, 5)
	for _, part := range []struct {
		count int
		label string
	}{
		{len(result.Upgraded), "upgraded"},
		{len(result.Downgraded), "downgraded"},
		{len(result.Added), "added"},
		{len(result.Removed), "removed"},
		{len(result.Changed), "changed"},
	} {
		if part.count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", part.count, part.label))
		}
	}
	return joinWithCommas(parts)
}

// ChangelogSections returns the diff as titled lists ready to render, in the
// order a user cares about: what moved forward first, then the surprises.
// Each section is omitted when empty.
func ChangelogSections(result sbom.Result) []ChangelogSection {
	sections := make([]ChangelogSection, 0, 5)
	for _, candidate := range []struct {
		title   string
		changes []sbom.Change
	}{
		{"Upgraded", result.Upgraded},
		{"Downgraded", result.Downgraded},
		{"Added", result.Added},
		{"Removed", result.Removed},
		// "Changed" holds pairs whose order could not be established, so it
		// is labelled for what it is rather than folded into Upgraded.
		{"Changed (version order unknown)", result.Changed},
	} {
		if len(candidate.changes) == 0 {
			continue
		}
		entries := make([]Row, 0, len(candidate.changes))
		for _, change := range candidate.changes {
			entries = append(entries, Row{
				Title:    change.Name,
				Subtitle: changeSubtitle(change),
			})
		}
		sections = append(sections, ChangelogSection{Title: candidate.title, Entries: entries})
	}
	return sections
}

// ChangelogSection is one titled group of package changes.
type ChangelogSection struct {
	Title   string
	Entries []Row
}

func changeSubtitle(change sbom.Change) string {
	switch {
	case change.From == "":
		return change.To
	case change.To == "":
		return change.From
	default:
		return fmt.Sprintf("%s → %s", change.From, change.To)
	}
}

func joinWithCommas(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	result := parts[0]
	for _, part := range parts[1 : len(parts)-1] {
		result += ", " + part
	}
	return result + ", " + parts[len(parts)-1]
}
