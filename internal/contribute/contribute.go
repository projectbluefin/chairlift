package contribute

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// DefaultRunner is the desktop command used to launch a terminal session.
	DefaultRunner = "xdg-terminal-exec"

	// DefaultUjust is the executable name for ujust.
	DefaultUjust = "ujust"

	// DefaultRecipe is the recipe name in ujust.
	DefaultRecipe = "contribute"

	// RegistrationEnv is the environment variable that can override the registration file path.
	RegistrationEnv = "HIVE_CONTRIBUTE_REGISTRATION"

	// RegistrationURL is the link to documentation explaining how to register with Hive.
	RegistrationURL = "https://github.com/projectbluefin/contribute#configuration"

	// RegistrationGuideLabel labels the control that opens RegistrationURL.
	RegistrationGuideLabel = "Registration Guide"

	// missingRegistrationSubtitle is shown with the RegistrationURL link
	// control rather than spelling the URL out as unclickable text.
	missingRegistrationSubtitle = "Sign up as a contributor first."
)

// DefaultRegistrationPath returns ~/.config/hive/contributor.env given a home directory.
func DefaultRegistrationPath(homeDir string) string {
	if homeDir == "" {
		return ""
	}
	return filepath.Join(homeDir, ".config", "hive", "contributor.env")
}

// RegistrationPath returns the configured or default path to the registration file.
func RegistrationPath(getenv func(string) string, homeDir string) string {
	if getenv != nil {
		if custom := getenv(RegistrationEnv); custom != "" {
			return custom
		}
	}
	return DefaultRegistrationPath(homeDir)
}

// Command returns the *exec.Cmd to launch the contributor session.
func Command(runner, ujustBin, recipe string) *exec.Cmd {
	if runner == "" {
		runner = DefaultRunner
	}
	if ujustBin == "" {
		ujustBin = DefaultUjust
	}
	if recipe == "" {
		recipe = DefaultRecipe
	}
	return exec.Command(runner, ujustBin, recipe)
}

// HasRecipe reports whether the ujust summary contains the named recipe.
func HasRecipe(summary, recipe string) bool {
	fields := strings.Fields(summary)
	for _, f := range fields {
		if f == recipe {
			return true
		}
	}
	return false
}

// Status represents the preflight outcome.
type Status int

const (
	// StatusReady indicates all preflight checks passed.
	StatusReady Status = iota
	// StatusMissingRunner indicates xdg-terminal-exec is not on PATH.
	StatusMissingRunner
	// StatusMissingUjust indicates ujust is not on PATH.
	StatusMissingUjust
	// StatusMissingRecipe indicates the contribute recipe is not listed by ujust.
	StatusMissingRecipe
	// StatusMissingPodman indicates podman is not on PATH.
	StatusMissingPodman
	// StatusMissingRegistration indicates the contributor registration file is absent.
	StatusMissingRegistration
)

// Result is the structured preflight outcome.
type Result struct {
	Status   Status
	Ready    bool
	Subtitle string
	// HelpURL, when set, is a page that resolves the unmet requirement; the
	// view offers it as a link control labelled RegistrationGuideLabel.
	HelpURL string
}

// Prober provides external system probes for preflight.
type Prober struct {
	LookPath      func(file string) (string, error)
	Stat          func(path string) (os.FileInfo, error)
	RecipeSummary func(ctx context.Context) (string, error)
	Getenv        func(key string) string
	HomeDir       func() (string, error)
}

// Preflight runs the read-only preflight checks using p.
func Preflight(ctx context.Context, p Prober) Result {
	lookPath := p.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	stat := p.Stat
	if stat == nil {
		stat = os.Stat
	}
	getenv := p.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	homeDirFn := p.HomeDir
	if homeDirFn == nil {
		homeDirFn = os.UserHomeDir
	}

	// 1. xdg-terminal-exec on PATH
	if _, err := lookPath(DefaultRunner); err != nil {
		return Result{
			Status:   StatusMissingRunner,
			Ready:    false,
			Subtitle: "Contribute needs a terminal app this computer doesn't have.",
		}
	}

	// 2. ujust on PATH
	if _, err := lookPath(DefaultUjust); err != nil {
		return Result{
			Status:   StatusMissingUjust,
			Ready:    false,
			Subtitle: "This computer is missing a part Contribute needs.",
		}
	}

	// 3. ujust recipe list contains contribute
	if p.RecipeSummary == nil {
		return Result{
			Status:   StatusMissingRecipe,
			Ready:    false,
			Subtitle: "This version of Bluefin can't run Contribute yet.",
		}
	}
	summary, err := p.RecipeSummary(ctx)
	if err != nil || !HasRecipe(summary, DefaultRecipe) {
		return Result{
			Status:   StatusMissingRecipe,
			Ready:    false,
			Subtitle: "This version of Bluefin can't run Contribute yet.",
		}
	}

	// 4. podman on PATH
	if _, err := lookPath("podman"); err != nil {
		return Result{
			Status:   StatusMissingPodman,
			Ready:    false,
			Subtitle: "This computer is missing a part Contribute needs.",
		}
	}

	// 5. registration file present
	homeDir, _ := homeDirFn()
	regPath := RegistrationPath(getenv, homeDir)
	if regPath == "" {
		return missingRegistration()
	}
	if fi, err := stat(regPath); err != nil || fi.IsDir() {
		return missingRegistration()
	}

	return Result{
		Status:   StatusReady,
		Ready:    true,
		Subtitle: "Opens a terminal to help build Bluefin.",
	}
}

func missingRegistration() Result {
	return Result{
		Status:   StatusMissingRegistration,
		Ready:    false,
		Subtitle: missingRegistrationSubtitle,
		HelpURL:  RegistrationURL,
	}
}

// RealProber returns a Prober using the live system environment.
func RealProber() Prober {
	return Prober{
		LookPath: exec.LookPath,
		Stat:     os.Stat,
		RecipeSummary: func(ctx context.Context) (string, error) {
			cmd := exec.CommandContext(ctx, DefaultUjust, "--summary")
			out, err := cmd.Output()
			if err != nil {
				return "", err
			}
			return string(out), nil
		},
		Getenv:  os.Getenv,
		HomeDir: os.UserHomeDir,
	}
}
