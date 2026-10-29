package contribute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockFileInfo struct {
	isDir bool
}

func (m mockFileInfo) Name() string       { return "contributor.env" }
func (m mockFileInfo) Size() int64        { return 42 }
func (m mockFileInfo) Mode() os.FileMode  { return 0o600 }
func (m mockFileInfo) ModTime() time.Time { return time.Now() }
func (m mockFileInfo) IsDir() bool        { return m.isDir }
func (m mockFileInfo) Sys() any           { return nil }

func readyProber(homeDir string) Prober {
	regPath := filepath.Join(homeDir, ".config", "hive", "contributor.env")
	return Prober{
		LookPath: func(file string) (string, error) {
			return "/usr/bin/" + file, nil
		},
		Stat: func(path string) (os.FileInfo, error) {
			if path == regPath {
				return mockFileInfo{isDir: false}, nil
			}
			return nil, os.ErrNotExist
		},
		RecipeSummary: func(ctx context.Context) (string, error) {
			return "benchmark update contribute clean-system", nil
		},
		Getenv: func(key string) string {
			return ""
		},
		HomeDir: func() (string, error) {
			return homeDir, nil
		},
	}
}

func TestPreflightOutcomes(t *testing.T) {
	const home = "/home/testuser"

	tests := []struct {
		name         string
		mutateProber func(p *Prober)
		wantStatus   Status
		wantReady    bool
		wantSub      string
		wantHelp     string
	}{
		{
			name:         "ready when all preflight requirements are met",
			mutateProber: func(p *Prober) {},
			wantStatus:   StatusReady,
			wantReady:    true,
			wantSub:      "Opens a terminal to help build Bluefin.",
		},
		{
			name: "missing xdg-terminal-exec runner",
			mutateProber: func(p *Prober) {
				orig := p.LookPath
				p.LookPath = func(file string) (string, error) {
					if file == DefaultRunner {
						return "", errors.New("not found")
					}
					return orig(file)
				}
			},
			wantStatus: StatusMissingRunner,
			wantReady:  false,
			wantSub:    "Contribute needs a terminal app this computer doesn't have.",
		},
		{
			name: "missing ujust executable",
			mutateProber: func(p *Prober) {
				orig := p.LookPath
				p.LookPath = func(file string) (string, error) {
					if file == DefaultUjust {
						return "", errors.New("not found")
					}
					return orig(file)
				}
			},
			wantStatus: StatusMissingUjust,
			wantReady:  false,
			wantSub:    "This computer is missing a part Contribute needs.",
		},
		{
			name: "ujust summary lacks contribute recipe",
			mutateProber: func(p *Prober) {
				p.RecipeSummary = func(ctx context.Context) (string, error) {
					return "benchmark update clean-system", nil
				}
			},
			wantStatus: StatusMissingRecipe,
			wantReady:  false,
			wantSub:    "This version of Bluefin can't run Contribute yet.",
		},
		{
			name: "ujust summary fails with an error",
			mutateProber: func(p *Prober) {
				p.RecipeSummary = func(ctx context.Context) (string, error) {
					return "", errors.New("execution failed")
				}
			},
			wantStatus: StatusMissingRecipe,
			wantReady:  false,
			wantSub:    "This version of Bluefin can't run Contribute yet.",
		},
		{
			name: "no recipe summary probe is treated as a missing recipe",
			mutateProber: func(p *Prober) {
				p.RecipeSummary = nil
			},
			wantStatus: StatusMissingRecipe,
			wantReady:  false,
			wantSub:    "This version of Bluefin can't run Contribute yet.",
		},
		{
			name: "missing podman executable",
			mutateProber: func(p *Prober) {
				orig := p.LookPath
				p.LookPath = func(file string) (string, error) {
					if file == "podman" {
						return "", errors.New("not found")
					}
					return orig(file)
				}
			},
			wantStatus: StatusMissingPodman,
			wantReady:  false,
			wantSub:    "This computer is missing a part Contribute needs.",
		},
		{
			name: "registration file does not exist",
			mutateProber: func(p *Prober) {
				p.Stat = func(path string) (os.FileInfo, error) {
					return nil, os.ErrNotExist
				}
			},
			wantStatus: StatusMissingRegistration,
			wantReady:  false,
			wantSub:    "Sign up as a contributor first.",
			wantHelp:   RegistrationURL,
		},
		{
			name: "registration path is a directory",
			mutateProber: func(p *Prober) {
				p.Stat = func(path string) (os.FileInfo, error) {
					return mockFileInfo{isDir: true}, nil
				}
			},
			wantStatus: StatusMissingRegistration,
			wantReady:  false,
			wantSub:    "Sign up as a contributor first.",
			wantHelp:   RegistrationURL,
		},
		{
			// Without a home directory there is no default registration
			// path; the check must fail closed rather than stat "".
			name: "home directory cannot be resolved",
			mutateProber: func(p *Prober) {
				p.HomeDir = func() (string, error) {
					return "", errors.New("$HOME is not defined")
				}
				p.Stat = func(path string) (os.FileInfo, error) {
					return mockFileInfo{isDir: false}, nil
				}
			},
			wantStatus: StatusMissingRegistration,
			wantReady:  false,
			wantSub:    "Sign up as a contributor first.",
			wantHelp:   RegistrationURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := readyProber(home)
			tt.mutateProber(&p)
			got := Preflight(context.Background(), p)
			if got.Status != tt.wantStatus {
				t.Errorf("Preflight status = %v, want %v", got.Status, tt.wantStatus)
			}
			if got.Ready != tt.wantReady {
				t.Errorf("Preflight ready = %v, want %v", got.Ready, tt.wantReady)
			}
			if got.Subtitle != tt.wantSub {
				t.Errorf("Preflight subtitle = %q, want %q", got.Subtitle, tt.wantSub)
			}
			for _, jargon := range []string{"xdg-terminal-exec", "ujust", "podman", "recipe", "Hive", "/", ".env"} {
				if strings.Contains(got.Subtitle, jargon) {
					t.Errorf("Preflight subtitle %q names %q", got.Subtitle, jargon)
				}
			}
			// A URL is offered as a link control, never as unclickable
			// subtitle text.
			if got.HelpURL != tt.wantHelp {
				t.Errorf("Preflight help URL = %q, want %q", got.HelpURL, tt.wantHelp)
			}
			if strings.Contains(got.Subtitle, "://") {
				t.Errorf("Preflight subtitle %q spells out a URL", got.Subtitle)
			}
		})
	}
}

func TestPreflightRegistrationOverride(t *testing.T) {
	const home = "/home/testuser"
	const custom = "/custom/path/to/custom-registration.env"

	p := readyProber(home)
	p.Getenv = func(key string) string {
		if key == RegistrationEnv {
			return custom
		}
		return ""
	}
	p.Stat = func(path string) (os.FileInfo, error) {
		if path == custom {
			return mockFileInfo{isDir: false}, nil
		}
		return nil, os.ErrNotExist
	}

	got := Preflight(context.Background(), p)
	if !got.Ready {
		t.Errorf("Preflight with custom registration should be ready, got %+v", got)
	}

	// Now make the custom path missing
	p.Stat = func(path string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	gotMissing := Preflight(context.Background(), p)
	if gotMissing.Ready || gotMissing.Status != StatusMissingRegistration {
		t.Errorf("Preflight with missing custom registration should fail registration check, got %+v", gotMissing)
	}
}

func TestCommandConstruction(t *testing.T) {
	t.Run("default command arguments", func(t *testing.T) {
		cmd := Command("", "", "")
		if cmd.Path != "xdg-terminal-exec" && filepath.Base(cmd.Path) != "xdg-terminal-exec" {
			t.Errorf("cmd.Path = %q, want xdg-terminal-exec", cmd.Path)
		}
		wantArgs := []string{"xdg-terminal-exec", "ujust", "contribute"}
		if len(cmd.Args) != len(wantArgs) {
			t.Fatalf("cmd.Args = %v, want %v", cmd.Args, wantArgs)
		}
		for i, arg := range wantArgs {
			if cmd.Args[i] != arg {
				t.Errorf("cmd.Args[%d] = %q, want %q", i, cmd.Args[i], arg)
			}
		}
	})

	t.Run("custom command arguments", func(t *testing.T) {
		cmd := Command("foot", "/custom/ujust", "custom-recipe")
		wantArgs := []string{"foot", "/custom/ujust", "custom-recipe"}
		if len(cmd.Args) != len(wantArgs) {
			t.Fatalf("cmd.Args = %v, want %v", cmd.Args, wantArgs)
		}
		for i, arg := range wantArgs {
			if cmd.Args[i] != arg {
				t.Errorf("cmd.Args[%d] = %q, want %q", i, cmd.Args[i], arg)
			}
		}
	})
}

func TestRecipeSummaryParsing(t *testing.T) {
	tests := []struct {
		summary string
		recipe  string
		want    bool
	}{
		{"", "contribute", false},
		{"contribute", "contribute", true},
		{"benchmark contribute update", "contribute", true},
		{"benchmark update", "contribute", false},
		{"contribute-more contributor", "contribute", false},
		{"foo\ncontribute\nbar", "contribute", true},
	}

	for _, tt := range tests {
		got := HasRecipe(tt.summary, tt.recipe)
		if got != tt.want {
			t.Errorf("HasRecipe(%q, %q) = %v, want %v", tt.summary, tt.recipe, got, tt.want)
		}
	}
}

func TestDefaultRegistrationPath(t *testing.T) {
	if got := DefaultRegistrationPath(""); got != "" {
		t.Errorf("DefaultRegistrationPath(\"\") = %q, want empty", got)
	}
	want := filepath.Join("/home/testuser", ".config", "hive", "contributor.env")
	if got := DefaultRegistrationPath("/home/testuser"); got != want {
		t.Errorf("DefaultRegistrationPath = %q, want %q", got, want)
	}
	if got := RegistrationPath(nil, "/home/testuser"); got != want {
		t.Errorf("RegistrationPath(nil getenv) = %q, want %q", got, want)
	}
}

// A zero Prober falls back to the live system probes; with an empty $PATH
// the first of them, the terminal lookup, must report the runner missing.
func TestPreflightZeroProberUsesSystemProbes(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	got := Preflight(context.Background(), Prober{})
	if got.Status != StatusMissingRunner || got.Ready {
		t.Errorf("Preflight(Prober{}) = %+v, want StatusMissingRunner and not ready", got)
	}
}

// withFakeUjust puts an executable script named "ujust" first on $PATH so
// RealProber's recipe probe runs a real subprocess.
func withFakeUjust(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, DefaultUjust), []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake ujust: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRealProberRecipeSummary(t *testing.T) {
	t.Run("returns the ujust --summary output", func(t *testing.T) {
		withFakeUjust(t, "#!/bin/sh\n[ \"$1\" = --summary ] || exit 2\necho 'benchmark contribute update'\n")

		p := RealProber()
		if p.LookPath == nil || p.Stat == nil || p.RecipeSummary == nil || p.Getenv == nil || p.HomeDir == nil {
			t.Fatalf("RealProber left a probe unset: %+v", p)
		}
		summary, err := p.RecipeSummary(context.Background())
		if err != nil {
			t.Fatalf("RecipeSummary error = %v", err)
		}
		if !HasRecipe(summary, DefaultRecipe) {
			t.Errorf("RecipeSummary = %q, want it to list %q", summary, DefaultRecipe)
		}
	})

	t.Run("reports a failing ujust as an error with no summary", func(t *testing.T) {
		withFakeUjust(t, "#!/bin/sh\necho contribute\nexit 1\n")

		summary, err := RealProber().RecipeSummary(context.Background())
		if err == nil {
			t.Fatal("RecipeSummary error = nil, want the ujust exit status")
		}
		if summary != "" {
			t.Errorf("RecipeSummary = %q, want empty on failure", summary)
		}
	})

	t.Run("reports a missing ujust as an error", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		if _, err := RealProber().RecipeSummary(context.Background()); err == nil {
			t.Fatal("RecipeSummary error = nil, want ujust not found")
		}
	})
}
