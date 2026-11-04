package imageinfo

import (
	"os"
	"path/filepath"
	"testing"
)

// Catch dropped streams, accidental driver changes, and suffix ordering that
// would send NVIDIA hosts to the unpublished dakota-gaming-nvidia image.
func TestGamingTargetPublishedPairs(t *testing.T) {
	ResetTable()
	t.Cleanup(ResetTable)
	pairs := []struct {
		base   string
		gaming string
	}{
		{"ghcr.io/projectbluefin/dakota", "ghcr.io/projectbluefin/dakota-gaming"},
		{"ghcr.io/projectbluefin/dakota-nvidia", "ghcr.io/projectbluefin/dakota-nvidia-gaming"},
	}
	for _, pair := range pairs {
		for _, tag := range []string{"stable", "testing", "next", "btw"} {
			t.Run(pair.base+"/"+tag, func(t *testing.T) {
				if got, ok := GamingTarget(pair.base, tag, true); !ok || got != pair.gaming+":"+tag {
					t.Errorf("enable = (%q, %v), want %s:%s", got, ok, pair.gaming, tag)
				}
				if got, ok := GamingTarget(pair.gaming, tag, false); !ok || got != pair.base+":"+tag {
					t.Errorf("disable = (%q, %v), want %s:%s", got, ok, pair.base, tag)
				}
				for _, state := range []struct {
					ref     string
					enabled bool
				}{{pair.base, false}, {pair.gaming, true}} {
					if got, ok := GamingTarget(state.ref, tag, state.enabled); ok || got != "" {
						t.Errorf("already requested state = (%q, %v), want empty, false", got, ok)
					}
				}
			})
		}
	}
}

// Catch guessed family/registry migrations and resurrection of dead or pinned
// tags, even when the image name resembles a supported Dakota image.
func TestGamingTargetRejectsUnsupportedReferences(t *testing.T) {
	ResetTable()
	t.Cleanup(ResetTable)
	refs := []string{
		"", "dakota", "ghcr.io/ublue-os/bluefin", "ghcr.io/projectbluefin/bluefin-lts",
		"ghcr.io/projectbluefin/dakota-dx", "ghcr.io/projectbluefin/dakota-nvidia-open",
		"ghcr.io/projectbluefin/dakota-gaming-nvidia", "ghcr.io/projectbluefin/dakota-dx-gaming",
		"example.org/projectbluefin/dakota", "ghcr.io/someone/dakota",
		"ghcr.io/projectbluefin/dakota:stable", "ghcr.io/projectbluefin/dakota@sha256:abc",
		"ghcr.io/projectbluefin/dakota-nvidia-open-gaming", "ghcr.io/projectbluefin/dakota-gaming-gaming",
	}
	for _, ref := range refs {
		for _, enabled := range []bool{false, true} {
			if got, ok := GamingTarget(ref, "stable", enabled); ok || got != "" {
				t.Errorf("GamingTarget(%q, stable, %v) = (%q, %v)", ref, enabled, got, ok)
			}
		}
	}
	for _, tag := range []string{"", "latest", "beta", "lts", "stable-daily", "stable-20261104", "20261104", "sha256:abc", "stable@sha256:abc"} {
		for _, ref := range []string{"ghcr.io/projectbluefin/dakota", "ghcr.io/projectbluefin/dakota-nvidia", "ghcr.io/projectbluefin/dakota-gaming", "ghcr.io/projectbluefin/dakota-nvidia-gaming"} {
			for _, enabled := range []bool{false, true} {
				if got, ok := GamingTarget(ref, tag, enabled); ok || got != "" {
					t.Errorf("GamingTarget(%q, %q, %v) = (%q, %v)", ref, tag, enabled, got, ok)
				}
			}
		}
	}
}

// Catch bypasses of the root-owned publication table, including an override
// that failed to load while leaving the previous built-in table in memory.
func TestGamingTargetHonorsAuthoritativeTables(t *testing.T) {
	for _, test := range []struct {
		name  string
		table string
		bad   bool
	}{
		{"malformed", "drivers: [broken", true},
		{"target stream absent", "drivers:\n  ghcr.io/projectbluefin/dakota-gaming:\n    standard: [testing]\n    nvidia: [testing]\n", false},
		{"return stream absent", "drivers:\n  ghcr.io/projectbluefin/dakota:\n    standard: [testing]\n    nvidia: [testing]\n", false},
		{"target driver absent", "drivers:\n  ghcr.io/projectbluefin/dakota-gaming:\n    standard: [stable, testing]\n", false},
		{"return driver absent", "drivers:\n  ghcr.io/projectbluefin/dakota:\n    standard: [stable, testing]\n", false},
		{"target channel absent", "images:\n  ghcr.io/projectbluefin/dakota-gaming:\n    stable_tags: [next]\n    testing_tags: [testing]\n    to_stable: {testing: next}\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ResetTable()
			t.Cleanup(ResetTable)
			path := filepath.Join(t.TempDir(), "channels.yml")
			if err := os.WriteFile(path, []byte(test.table), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadTable([]string{path}); (err != nil) != test.bad {
				t.Fatalf("LoadTable error = %v, want failure %v", err, test.bad)
			}
			for _, state := range []struct {
				ref     string
				enabled bool
			}{
				{"ghcr.io/projectbluefin/dakota-nvidia", true},
				{"ghcr.io/projectbluefin/dakota-nvidia-gaming", false},
			} {
				if got, ok := GamingTarget(state.ref, "stable", state.enabled); ok || got != "" {
					t.Errorf("GamingTarget(%q, stable, %v) = (%q, %v), want empty, false", state.ref, state.enabled, got, ok)
				}
			}
		})
	}
}
