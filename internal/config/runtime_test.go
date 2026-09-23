package config

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func withReadFile(t *testing.T, fn func(string) ([]byte, error)) {
	t.Helper()
	original := readFile
	t.Cleanup(func() { readFile = original })
	readFile = fn
}

func assertAllKnownGroupsDisabled(t *testing.T, got *Config) {
	t.Helper()
	pages, err := SchemaPages()
	if err != nil {
		t.Fatalf("SchemaPages(): %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("SchemaPages() returned no pages")
	}

	defaults := defaultConfig()
	for _, page := range pages {
		groups, err := SchemaGroups(page)
		if err != nil {
			t.Fatalf("SchemaGroups(%q): %v", page, err)
		}
		if len(groups) == 0 {
			t.Fatalf("SchemaGroups(%q) returned no groups", page)
		}
		for _, group := range groups {
			gotGroup := got.GetGroupConfig(page, group)
			if gotGroup == nil {
				t.Errorf("fail-closed config missing %s.%s", page, group)
				continue
			}
			if gotGroup.Enabled {
				t.Errorf("fail-closed config left %s.%s enabled", page, group)
			}

			wantGroup := defaults.GetGroupConfig(page, group)
			if wantGroup == nil {
				t.Errorf("default config missing canonical %s.%s", page, group)
				continue
			}
			wantGroup.Enabled = false
			if !reflect.DeepEqual(*gotGroup, *wantGroup) {
				t.Errorf("fail-closed %s.%s = %+v, want defaults with Enabled=false: %+v",
					page, group, *gotGroup, *wantGroup)
			}
		}
	}
}

func TestLoadMissingHigherPriorityContinuesToValidCandidate(t *testing.T) {
	dir := t.TempDir()
	high := filepath.Join(dir, "missing.yml")
	low := writeConfigFile(t, "agents_page:\n  agents_group:\n    enabled: false\n")
	withConfigPaths(t, []string{high, low})

	cfg, loadErr := Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v, want nil", loadErr)
	}
	if cfg.AgentsPage["agents_group"].Enabled {
		t.Fatal("lower-priority valid overlay was not loaded after absent candidate")
	}
}

func TestLoadReadFailureStopsPrecedenceAndFailsClosed(t *testing.T) {
	dir := t.TempDir()
	high := filepath.Join(dir, "high.yml")
	low := filepath.Join(dir, "low.yml")
	withConfigPaths(t, []string{high, low})

	var reads []string
	withReadFile(t, func(path string) ([]byte, error) {
		reads = append(reads, path)
		switch path {
		case high:
			return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrPermission}
		case low:
			return []byte("agents_page:\n  agents_group:\n    enabled: true\n"), nil
		default:
			return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
		}
	})

	cfg, loadErr := Load()
	if loadErr == nil {
		t.Fatal("Load() error = nil, want authoritative read failure")
	}
	if loadErr.Kind != KindRead {
		t.Fatalf("Load() error kind = %q, want %q", loadErr.Kind, KindRead)
	}
	if loadErr.Path != high {
		t.Fatalf("Load() error path = %q, want %q", loadErr.Path, high)
	}
	if !errors.Is(loadErr, fs.ErrPermission) {
		t.Fatalf("errors.Is(%v, fs.ErrPermission) = false", loadErr)
	}
	if !reflect.DeepEqual(reads, []string{high}) {
		t.Fatalf("read paths = %v, want only authoritative candidate %q", reads, high)
	}
	assertAllKnownGroupsDisabled(t, cfg)
}

func TestLoadInvalidAuthoritativeStopsPrecedenceAndFailsClosed(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		wantKind ErrorKind
	}{
		{
			name:     "schema",
			contents: "updates_pages:\n  brew_updates_group:\n    enabled: false\n",
			wantKind: KindSchema,
		},
		{
			name:     "parse-type",
			contents: "agents_page: [\n",
			wantKind: KindParseType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			high := filepath.Join(dir, "high.yml")
			low := filepath.Join(dir, "low.yml")
			withConfigPaths(t, []string{high, low})

			var reads []string
			withReadFile(t, func(path string) ([]byte, error) {
				reads = append(reads, path)
				switch path {
				case high:
					return []byte(tt.contents), nil
				case low:
					return []byte("agents_page:\n  agents_group:\n    enabled: true\n"), nil
				default:
					return nil, &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
				}
			})

			cfg, loadErr := Load()
			if loadErr == nil {
				t.Fatal("Load() error = nil, want authoritative validation failure")
			}
			if loadErr.Kind != tt.wantKind {
				t.Fatalf("Load() error kind = %q, want %q: %v", loadErr.Kind, tt.wantKind, loadErr)
			}
			if loadErr.Path != high {
				t.Fatalf("Load() error path = %q, want %q", loadErr.Path, high)
			}
			if !reflect.DeepEqual(reads, []string{high}) {
				t.Fatalf("read paths = %v, want only authoritative candidate %q", reads, high)
			}
			assertAllKnownGroupsDisabled(t, cfg)
		})
	}
}

func TestLoadFromPathRejectsUnknownSchemaName(t *testing.T) {
	path := writeConfigFile(t, "not_a_page:\n  anything: true\n")
	cfg, loadErr := loadFromPath(path)
	if cfg != nil {
		t.Fatalf("loadFromPath(%q) config = %+v, want nil", path, cfg)
	}
	if loadErr == nil || loadErr.Kind != KindSchema {
		t.Fatalf("loadFromPath(%q) error = %v, want KindSchema", path, loadErr)
	}
	if loadErr.Path != path || !strings.Contains(loadErr.Detail, "not_a_page") {
		t.Fatalf("loadFromPath(%q) error = %+v, want path and offending key", path, loadErr)
	}
}

func TestLoadAuthoritativeFailureLogsHighSignalDiagnostic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	withConfigPaths(t, []string{path})
	withReadFile(t, func(got string) ([]byte, error) {
		return nil, &fs.PathError{Op: "open", Path: got, Err: fs.ErrPermission}
	})

	var output bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()
	originalPrefix := log.Prefix()
	t.Cleanup(func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
		log.SetPrefix(originalPrefix)
	})
	log.SetOutput(&output)
	log.SetFlags(0)
	log.SetPrefix("")

	_, loadErr := Load()
	if loadErr == nil {
		t.Fatal("Load() error = nil, want read failure")
	}
	got := output.String()
	for _, want := range []string{
		"CONFIGURATION ERROR",
		path,
		"permission denied",
		"all feature groups were disabled",
		"restart ChairLift",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("startup log %q does not contain %q", got, want)
		}
	}
}

func TestLoadDanglingAuthoritativeSymlinkFailsClosed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "missing-target.yml")
	link := filepath.Join(dir, "config.yml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("creating dangling symlink: %v", err)
	}
	// The issue's reproduction: ReadFile follows the link and reports ENOENT,
	// indistinguishable from an absent path without an Lstat on the candidate.
	if _, err := os.ReadFile(link); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("setup: ReadFile(dangling symlink) err = %v, want ENOENT", err)
	}
	withConfigPaths(t, []string{link})

	cfg, loadErr := Load()
	if loadErr == nil {
		t.Fatal("Load() error = nil, want authoritative failure for dangling symlink")
	}
	if loadErr.Kind != KindRead {
		t.Fatalf("Load() error kind = %q, want %q", loadErr.Kind, KindRead)
	}
	if loadErr.Path != link {
		t.Fatalf("Load() error path = %q, want %q", loadErr.Path, link)
	}
	if cfg == nil {
		t.Fatal("Load() config = nil on authoritative failure")
	}
	// A dangling authoritative symlink must not fall back to package defaults:
	// every known group stays disabled with a persistent error.
	assertAllKnownGroupsDisabled(t, cfg)
}

func TestLoadDanglingHigherPriorityDoesNotFallThroughToValidCandidate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "missing-target.yml")
	high := filepath.Join(dir, "high.yml")
	low := writeConfigFile(t, "agents_page:\n  agents_group:\n    enabled: true\n")
	if err := os.Symlink(target, high); err != nil {
		t.Fatalf("creating dangling symlink: %v", err)
	}
	withConfigPaths(t, []string{high, low})

	cfg, loadErr := Load()
	if loadErr == nil {
		t.Fatal("Load() error = nil, want authoritative failure for dangling symlink")
	}
	if loadErr.Path != high {
		t.Fatalf("Load() error path = %q, want the dangling %q", loadErr.Path, high)
	}
	if cfg.AgentsPage["agents_group"].Enabled {
		t.Fatal("dangling authoritative symlink fell through to a valid lower-priority candidate")
	}
	assertAllKnownGroupsDisabled(t, cfg)
}

func TestLoadAllCandidatesAbsentReturnsDefaultsWithoutError(t *testing.T) {
	dir := t.TempDir()
	withConfigPaths(t, []string{
		filepath.Join(dir, "one.yml"),
		filepath.Join(dir, "two.yml"),
		filepath.Join(dir, "three.yml"),
	})
	withReadFile(t, func(path string) ([]byte, error) {
		return nil, &fs.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	})

	cfg, loadErr := Load()
	if loadErr != nil {
		t.Fatalf("Load() error = %v, want nil", loadErr)
	}
	if !reflect.DeepEqual(cfg, defaultConfig()) {
		t.Fatalf("Load() all-absent config = %+v, want built-in defaults %+v", cfg, defaultConfig())
	}
}

func TestWindowConfigFailureWiringIsPersistent(t *testing.T) {
	sourcePath := filepath.Join(repoRoot(), "internal", "window", "window.go")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", sourcePath, err)
	}

	text := string(source)
	for _, required := range []string{
		"config.Load()",
		"configError:       configErr",
		"w.ShowErrorToast(w.configError.ToastMessage())",
		"toast.SetTimeout(0)",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("window config-error wiring does not contain %q", required)
		}
	}
}

func TestConfigurationGuideExamplePassesStrictValidation(t *testing.T) {
	tests := []struct {
		relPath string
		heading string
	}{
		{"CONFIG.md", "## Example: Disabling Homebrew Features"},
		{filepath.Join("docs", "reference.md"), "## Example"},
	}

	for _, tc := range tests {
		t.Run(tc.relPath, func(t *testing.T) {
			path := filepath.Join(repoRoot(), tc.relPath)
			guide, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			afterHeading := string(guide)
			headingIndex := strings.Index(afterHeading, tc.heading)
			if headingIndex < 0 {
				t.Fatalf("%s does not contain %q", path, tc.heading)
			}
			afterHeading = afterHeading[headingIndex+len(tc.heading):]
			fenceStart := strings.Index(afterHeading, "```yaml")
			if fenceStart < 0 {
				t.Fatalf("%s example has no YAML fence", tc.heading)
			}
			afterFence := afterHeading[fenceStart+len("```yaml"):]
			fenceEnd := strings.Index(afterFence, "```")
			if fenceEnd < 0 {
				t.Fatalf("%s example has no closing fence", tc.heading)
			}

			example := afterFence[:fenceEnd]
			tmpPath := writeConfigFile(t, example)
			merged, loadErr := loadFromPath(tmpPath)
			if loadErr != nil {
				t.Fatalf("%s documented YAML is rejected by the runtime validator: %v", tc.heading, loadErr)
			}

			for _, check := range []struct {
				page  string
				group string
			}{
				{"updates_page", "automatic_updates_group"},
				{"updates_page", "brew_updates_group"},
				{"updates_page", "brew_trust_group"},
				{"applications_page", "brew_group"},
				{"applications_page", "brew_search_group"},
				{"applications_page", "brew_bundles_group"},
				{"maintenance_page", "maintenance_freespace_group"},
				{"help_page", "troubleshooting_group"},
			} {
				if merged.IsGroupEnabled(check.page, check.group) {
					t.Errorf("%s example leaves %s.%s enabled", tc.relPath, check.page, check.group)
				}
			}
		})
	}
}

// TestLoadUntrustedCandidateSymlinkedIntoTrustedDirFailsClosed pins the
// provenance decision to which fixed candidate matched rather than to what the
// filesystem says about the path after the bytes were read. An attacker who
// controls an untrusted candidate can point it at a trusted directory (or
// re-point it between read and validation); Load() must still treat the
// candidate as untrusted and refuse the privileged action it inherits.
func TestLoadUntrustedCandidateSymlinkedIntoTrustedDirFailsClosed(t *testing.T) {
	trustedDir := t.TempDir()
	withTrustedConfigDirectories(t, []string{trustedDir})

	target := filepath.Join(trustedDir, "config.yml")
	if err := os.WriteFile(target, []byte("maintenance_page:\n  maintenance_cleanup_group:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	candidate := filepath.Join(t.TempDir(), "config.dev.yml")
	if err := os.Symlink(target, candidate); err != nil {
		t.Fatalf("linking candidate: %v", err)
	}
	withConfigPaths(t, []string{candidate})

	cfg, err := Load()
	if err == nil {
		t.Fatalf("Load() err = nil, want provenance failure (cfg = %+v)", cfg)
	}
	if err.Kind != KindSchema {
		t.Fatalf("err.Kind = %v, want %v", err.Kind, KindSchema)
	}
	if !strings.Contains(err.Detail, "sudo actions are only permitted in trusted configurations") {
		t.Fatalf("err.Detail = %q, want provenance error", err.Detail)
	}
	assertAllKnownGroupsDisabled(t, cfg)
}

// TestLoadTrustedCandidateKeepsPrivilegedDefault is the other half: a fixed
// trusted candidate still loads the privileged default, so the index-free
// provenance rule did not simply disable sudo everywhere.
func TestLoadTrustedCandidateKeepsPrivilegedDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("maintenance_page:\n  maintenance_cleanup_group:\n    enabled: true\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	withTrustedConfigPaths(t, []string{path})
	withConfigPaths(t, []string{path})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() err = %v, want nil", err)
	}
	group := cfg.MaintenancePage["maintenance_cleanup_group"]
	if !group.Enabled || len(group.Actions) != 1 || !group.Actions[0].Sudo {
		t.Fatalf("maintenance_cleanup_group = %+v, want enabled with the privileged default action", group)
	}
}
