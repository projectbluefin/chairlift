package installcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isExampleConfig reports whether a repository-root file name is an example
// configuration: a committed YAML file that exists to be read by a human
// rather than by a build, a service, or the application. The name is the
// classifier because that is the convention already in use
// (channels.example.yml), and it keeps tool configuration consumed by external
// services (codecov.yml) out of scope without an allowlist that would itself
// need maintaining.
func isExampleConfig(name string) bool {
	if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
		return false
	}
	return strings.Contains(name, "example")
}

// rootExampleConfigs returns the example configuration files committed at the
// repository root, in directory order.
func rootExampleConfigs(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(RepoRoot())
	if err != nil {
		t.Fatalf("reading repository root: %v", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isExampleConfig(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	return names
}

// installDestinations returns every path a packaging file installs to: the
// argument of a Makefile `install -D`, and the value of a .goreleaser.yaml
// `dst:` key. Only destinations are collected, never prose, because both files
// legitimately name paths in comments that explain why they are *not*
// installed — the same distinction TestChannelTableIsNotInstalledLive draws.
func installDestinations(t *testing.T, relative string) []string {
	t.Helper()

	var destinations []string
	for _, line := range strings.Split(readRepoFile(t, relative), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.Contains(trimmed, "install -D"):
			fields := strings.Fields(trimmed)
			if len(fields) > 0 {
				destinations = append(destinations, fields[len(fields)-1])
			}
		case strings.HasPrefix(trimmed, "dst:"):
			destinations = append(destinations, strings.TrimSpace(strings.TrimPrefix(trimmed, "dst:")))
		}
	}
	return destinations
}

// An example configuration that no package ships is documentation its audience
// cannot reach, and — because no gate reads it — a second statement of the
// configuration surface that drifts silently behind config.yml. That is exactly
// how config.bootc-example.yml fell nine groups behind before it was deleted
// (issue #144).
//
// channels.example.yml is the shape this asserts: installed by the Makefile and
// packaged by both nFPM entries, so a bootc or deb/rpm user finds it under
// /usr/share/doc/chairlift/. Any future example must arrive wired the same way
// or not at all.
func TestEveryCommittedExampleConfigIsShipped(t *testing.T) {
	examples := rootExampleConfigs(t)
	if len(examples) == 0 {
		t.Fatal("no example configuration found at the repository root; " +
			"this gate assumes channels.example.yml exists and would otherwise pass vacuously")
	}

	makefileDestinations := installDestinations(t, "Makefile")
	goreleaserDestinations := installDestinations(t, ".goreleaser.yaml")

	for _, example := range examples {
		installedBy := func(destinations []string) bool {
			for _, destination := range destinations {
				if filepath.Base(destination) == example {
					return true
				}
			}
			return false
		}

		if !installedBy(makefileDestinations) {
			t.Errorf("%s is committed but no Makefile `install -D` ships it; "+
				"install it beside channels.example.yml or delete it", example)
		}
		if !installedBy(goreleaserDestinations) {
			t.Errorf("%s is committed but no .goreleaser.yaml `dst:` packages it; "+
				"an example absent from the packages reaches no user", example)
		}
	}
}

// Packaging an example is only half of reaching its audience: a file installed
// under /usr/share/doc that no document names is found by accident or not at
// all. README.md and CONFIG.md are the two entry points a configuring user
// reads, so an example must be named by at least one of them.
func TestEveryCommittedExampleConfigIsDocumented(t *testing.T) {
	readme := readRepoFile(t, "README.md")
	configDoc := readRepoFile(t, "CONFIG.md")

	for _, example := range rootExampleConfigs(t) {
		if !strings.Contains(readme, example) && !strings.Contains(configDoc, example) {
			t.Errorf("%s is shipped but named by neither README.md nor CONFIG.md; "+
				"an undocumented example is not discoverable", example)
		}
	}
}
