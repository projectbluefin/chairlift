package firstrun

import "github.com/projectbluefin/chairlift/internal/navigation"

// Pages snapshots only visible primary destinations, in the explicit setup
// order. Disabled or unsupported pages are omitted rather than resolved to Help.
func Pages(visible []navigation.Item) []string {
	var pages []string
	for _, name := range []string{"livery", "features", "applications", "agents"} {
		for _, item := range visible {
			if item.Name == name {
				pages = append(pages, name)
				break
			}
		}
	}
	return pages
}
