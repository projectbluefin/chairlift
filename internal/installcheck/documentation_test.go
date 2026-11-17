package installcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/config"
)

func readRepoFile(t *testing.T, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(RepoRoot(), relative))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(data)
}

func TestDocumentedConfigInventoryMatchesCanonicalSchema(t *testing.T) {
	pages, err := config.SchemaPages()
	if err != nil {
		t.Fatalf("config.SchemaPages(): %v", err)
	}

	content := readRepoFile(t, "CONFIG.md")
	for _, page := range pages {
		groups, err := config.SchemaGroups(page)
		if err != nil {
			t.Fatalf("config.SchemaGroups(%q): %v", page, err)
		}
		for _, group := range groups {
			if !strings.Contains(content, "`"+group+"`") {
				t.Errorf("CONFIG.md does not document canonical group %s.%s", page, group)
			}
		}
	}
}

func TestDocumentedOptionalFieldsCoverCanonicalGroupFields(t *testing.T) {
	fields, err := config.SchemaGroupFields()
	if err != nil {
		t.Fatalf("config.SchemaGroupFields(): %v", err)
	}

	configDoc := readRepoFile(t, "CONFIG.md")
	for _, field := range fields {
		if field == "enabled" {
			continue // handled separately in docs
		}
		if !strings.Contains(configDoc, "`"+field+"`") {
			t.Errorf("CONFIG.md does not document optional field %q", field)
		}
	}
}
