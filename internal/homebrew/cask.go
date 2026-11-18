package homebrew

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// tapCaskAmbiguity is the phrase Homebrew prints when a bare cask token is
// defined by more than one installed tap. Its token lookup refuses to guess,
// so every command that resolves a cask by token alone exits non-zero once
// both projectbluefin/tap and ublue-os/tap ship the same cask (#835).
const tapCaskAmbiguity = "exists in multiple taps"

// isAmbiguousCaskError reports whether a brew failure is Homebrew refusing to
// choose between taps that define the same cask token.
func isAmbiguousCaskError(err error) bool {
	return err != nil && strings.Contains(err.Error(), tapCaskAmbiguity)
}

// InstalledCaskTaps maps each installed cask token to the tap recorded in its
// installation receipt (Caskroom/<token>/.metadata/INSTALL_RECEIPT.json).
//
// The receipt is the one cask resolution that cannot become ambiguous: it
// records where this copy actually came from, while Homebrew's own token
// lookup loads a cask definition and refuses when several taps offer the
// token. Callers qualify a token with this tap instead of asking brew to
// choose.
func InstalledCaskTaps() (map[string]string, error) {
	prefix, err := brewPrefix()
	if err != nil {
		return nil, err
	}
	return installedCaskTaps(filepath.Join(prefix, "Caskroom"))
}

// installedCaskTaps reads the source tap out of each Caskroom receipt. Only a
// missing Caskroom is an empty inventory; a directory or receipt that exists
// but cannot be read is reported, so a damaged install record is never
// mistaken for "this cask is not installed" (#835 review).
func installedCaskTaps(caskroomDir string) (map[string]string, error) {
	taps := make(map[string]string)
	entries, err := os.ReadDir(caskroomDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return taps, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		tap, err := caskReceiptTap(filepath.Join(caskroomDir, entry.Name(), ".metadata", "INSTALL_RECEIPT.json"))
		if err != nil {
			return nil, err
		}
		if tap != "" {
			taps[entry.Name()] = tap
		}
	}
	return taps, nil
}

// caskReceiptTap reads source.tap from one cask installation receipt. The
// older versioned Casks/<token>.json metadata is not used: current Homebrew
// no longer records the source tap there. A receipt that is missing names no
// tap and no error; one that exists but cannot be read or parsed is reported,
// because it is the only record of where the installed copy came from.
func caskReceiptTap(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("reading cask receipt %s: %w", path, err)
	}
	var receipt struct {
		Source struct {
			Tap string `json:"tap"`
		} `json:"source"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		return "", fmt.Errorf("parsing cask receipt %s: %w", path, err)
	}
	return receipt.Source.Tap, nil
}

// qualifiedInstalledCask returns name qualified by the tap that owns its
// installed receipt. A bare token with no installed copy is returned
// unchanged: there is nothing on this host to disambiguate it against, and
// brew reports the real problem itself.
func qualifiedInstalledCask(name string) string {
	return qualifyInstalledCaskNames([]string{name})[0]
}

// qualifyInstalledCaskNames applies qualifiedInstalledCask to a batch, reading
// the receipts once.
func qualifyInstalledCaskNames(names []string) []string {
	qualified := append([]string(nil), names...)
	taps, err := InstalledCaskTaps()
	if err != nil {
		log.Printf("reading installed cask receipts failed: %v", err)
		return qualified
	}
	for i, name := range qualified {
		if strings.Contains(name, "/") {
			continue
		}
		if tap := taps[name]; tap != "" && tap != CaskTap {
			qualified[i] = tap + "/" + name
		}
	}
	return qualified
}

// caskFullName is Homebrew's qualified name for a cask token: a third-party
// tap's token is "tap/token", while an official homebrew/cask token stays
// bare, the same convention Package.QualifiedName documents for formulae.
func caskFullName(tap, token string) string {
	if tap == "" || tap == CaskTap {
		return token
	}
	return tap + "/" + token
}

// applyReceiptTaps replaces the tap brew reported for each installed cask
// with the one its receipt records. Homebrew's JSON comes from loading a cask
// definition, which can name another tap that shares the token; the receipt
// names the tap this copy was installed from.
func applyReceiptTaps(packages []Package) []Package {
	if len(packages) == 0 {
		return packages
	}
	taps, err := InstalledCaskTaps()
	if err != nil {
		log.Printf("reading installed cask receipts failed: %v", err)
		return packages
	}
	for i := range packages {
		if tap := taps[packages[i].Name]; tap != "" {
			packages[i].Tap = tap
			packages[i].FullName = caskFullName(tap, packages[i].Name)
		}
	}
	return packages
}

// brewRepository returns Homebrew's installation repository, the directory
// that holds Library/Taps.
func brewRepository() (string, error) {
	out, err := runBrewCommand("--repository")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// installedCaskInventory builds the installed-cask inventory from Caskroom
// receipts instead of from `brew info --installed`. It is the fallback for a
// Homebrew that cannot answer the installed-cask JSON query because two taps
// define the same token; the receipt still says which tap this copy came
// from, and the versioned Caskroom directory says which version is staged.
func installedCaskInventory() ([]Package, error) {
	prefix, err := brewPrefix()
	if err != nil {
		return nil, err
	}
	caskroom := filepath.Join(prefix, "Caskroom")
	entries, err := os.ReadDir(caskroom)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, &Error{Message: "Failed to read installed casks: " + err.Error(), Err: err}
	}
	var packages []Package
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		token := entry.Name()
		tap, err := caskReceiptTap(filepath.Join(caskroom, token, ".metadata", "INSTALL_RECEIPT.json"))
		if err != nil {
			return nil, &Error{Message: "Failed to read installed casks: " + err.Error(), Err: err}
		}
		pkg := Package{
			Name:     token,
			FullName: caskFullName(tap, token),
			Tap:      tap,
			Version:  caskInstalledVersion(filepath.Join(caskroom, token)),
		}
		packages = append(packages, pkg)
	}
	return packages, nil
}

// caskInstalledVersion reports the newest staged version directory, which is
// the version `brew info --installed` reports as installed. .metadata and
// symlinks are not staged versions.
func caskInstalledVersion(caskroomPath string) string {
	entries, err := os.ReadDir(caskroomPath)
	if err != nil {
		return ""
	}
	version := ""
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Name() > version {
			version = entry.Name()
		}
	}
	return version
}

// TapDefinesCask reports whether an installed tap's Casks directory defines
// token. It reads the file path directly rather than loading the cask, so it
// neither hits an ambiguous token nor triggers Homebrew's tap-trust refusal.
// A tap that is not installed defines nothing.
//
// The Casks directory hangs off Homebrew's repository (`brew --repository`),
// not its prefix: a Linuxbrew install keeps Library/Taps under
// <prefix>/Homebrew, and a prefix-relative guess silently reports every tap
// as retired.
func TapDefinesCask(tap, token string) (bool, error) {
	user, repo, ok := strings.Cut(tap, "/")
	if !ok || user == "" || repo == "" || strings.Contains(repo, "/") {
		return false, errors.New("invalid tap name: " + tap)
	}
	repository, err := brewRepository()
	if err != nil {
		return false, err
	}
	path := filepath.Join(repository, "Library", "Taps", user, "homebrew-"+repo, "Casks", token+".rb")
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return info.Mode().IsRegular(), nil
}
