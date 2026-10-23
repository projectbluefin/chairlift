package homebrew

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// untrustedTapErrRe matches the tap name in brew's "from untrusted tap
// <user>/<repo>" error line. The capture is restricted to the characters
// GitHub allows in an owner and a repository name, so output replayed from a
// third-party installer cannot smuggle shell metacharacters into the
// `brew trust <tap>` command the toast suggests. Dots are allowed in the repo
// segment (repository names may contain them); the sentence-ending period
// brew appends is trimmed afterwards instead of being excluded from the
// charset, which would truncate a tap such as foo/bar.baz to foo/bar.
var untrustedTapErrRe = regexp.MustCompile(`(?m)^Error: .*from untrusted tap ([A-Za-z0-9_-]+/[A-Za-z0-9._-]+)`)

func untrustedTapFromErrorLine(s string) (string, bool) {
	m := untrustedTapErrRe.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	tap := strings.TrimRight(m[1], ".")
	if _, repo, ok := strings.Cut(tap, "/"); !ok || repo == "" {
		return "", false
	}
	return tap, true
}

// UntrustedTap describes an untrusted tap and the packages installed from it.
// Package names are fully qualified (user/tap/name), ready for `brew trust`.
type UntrustedTap struct {
	Name     string
	Formulae []string
	Casks    []string
}

// Without returns the tap with one package dropped from the namespace kind
// names, after that package was trusted on its own. The receiver is left
// untouched, so a copy handed to a running trust keeps its own list.
func (t UntrustedTap) Without(kind PackageKind, name string) UntrustedTap {
	drop := func(names []string) []string {
		kept := make([]string, 0, len(names))
		for _, n := range names {
			if n != name {
				kept = append(kept, n)
			}
		}
		return kept
	}
	switch kind {
	case Formula:
		t.Formulae = drop(t.Formulae)
	case Cask:
		t.Casks = drop(t.Casks)
	}
	return t
}

// Count is how many installed packages from the tap are still untrusted.
func (t UntrustedTap) Count() int { return len(t.Formulae) + len(t.Casks) }

// parseUntrustedTapNames extracts names of untrusted taps from
// `brew tap-info --installed --json` output. The "trusted" key was added in
// Homebrew 6; older brew versions omit it entirely (and have no `brew
// trust` command), so a nil/missing field must be treated as trusted rather
// than defaulting to false, or every tap on pre-6 brew would be reported as
// untrusted.
func parseUntrustedTapNames(data []byte) ([]string, error) {
	var taps []struct {
		Name    string `json:"name"`
		Trusted *bool  `json:"trusted"`
	}
	if err := json.Unmarshal(data, &taps); err != nil {
		return nil, &Error{Message: fmt.Sprintf("failed to parse tap-info JSON: %v", err)}
	}
	var names []string
	for _, t := range taps {
		if t.Trusted != nil && !*t.Trusted {
			names = append(names, t.Name)
		}
	}
	return names, nil
}

// installedFormulaeByTap maps tap name -> qualified installed formula names
// by reading Cellar keg INSTALL_RECEIPT.json files. This is the only
// reliable source: brew itself refuses to load (and therefore list)
// formulae from untrusted taps.
func installedFormulaeByTap(cellarDir string) map[string][]string {
	byTap := make(map[string][]string)
	kegs, err := os.ReadDir(cellarDir)
	if err != nil {
		return byTap
	}
	for _, keg := range kegs {
		if !keg.IsDir() {
			continue
		}
		versions, err := os.ReadDir(filepath.Join(cellarDir, keg.Name()))
		if err != nil {
			continue
		}
		for _, v := range versions {
			receiptPath := filepath.Join(cellarDir, keg.Name(), v.Name(), "INSTALL_RECEIPT.json")
			data, err := os.ReadFile(receiptPath)
			if err != nil {
				continue
			}
			var receipt struct {
				Source struct {
					Tap string `json:"tap"`
				} `json:"source"`
			}
			if err := json.Unmarshal(data, &receipt); err != nil || receipt.Source.Tap == "" {
				continue
			}
			byTap[receipt.Source.Tap] = append(byTap[receipt.Source.Tap], receipt.Source.Tap+"/"+keg.Name())
			break // one receipt per keg is enough
		}
	}
	return byTap
}

// installedCasksByTap maps tap name -> qualified installed cask tokens by
// reading each cask's .metadata/INSTALL_RECEIPT.json (the path
// Cask::Tab.create writes: <prefix>/Caskroom/<token>/.metadata/
// INSTALL_RECEIPT.json), whose source.tap field is the authoritative origin
// Homebrew records at install time. This mirrors installedFormulaeByTap: the
// receipt is the only reliable source, because brew itself refuses to load
// (and therefore list) casks from untrusted taps. The older versioned
// Casks/<token>.json metadata is not used: current Homebrew no longer records
// the source tap there, so casks from untrusted taps would otherwise be
// silently omitted from the remediation UI.
func installedCasksByTap(caskroomDir string) map[string][]string {
	byTap := make(map[string][]string)
	entries, err := os.ReadDir(caskroomDir)
	if err != nil {
		return byTap
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		token := entry.Name()
		receiptPath := filepath.Join(caskroomDir, token, ".metadata", "INSTALL_RECEIPT.json")
		data, err := os.ReadFile(receiptPath)
		if err != nil {
			continue
		}
		var receipt struct {
			Source struct {
				Tap string `json:"tap"`
			} `json:"source"`
		}
		if err := json.Unmarshal(data, &receipt); err != nil || receipt.Source.Tap == "" {
			continue
		}
		byTap[receipt.Source.Tap] = append(byTap[receipt.Source.Tap], receipt.Source.Tap+"/"+token)
	}
	return byTap
}

// brewPrefix returns Homebrew's installation prefix.
func brewPrefix() (string, error) {
	output, err := runBrewCommand("--prefix")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// TrustStore is the per-user Homebrew trust state: the taps trusted
// wholesale and the packages trusted one by one with
// `brew trust --tap|--formula|--cask`, keyed by lowercased name (brew
// lowercases every entry it stores).
//
// It answers the two questions an install has to settle before it may load
// a package from a tap — is the whole tap trusted, and is this one package
// trusted?
type TrustStore struct {
	taps     map[string]bool
	formulae map[string]bool
	casks    map[string]bool
}

// parseTrustedPackages reads `brew trust --json=v1` output:
// {"taps": [...], "formulae": [...], "casks": [...], "commands": [...]}.
func parseTrustedPackages(data []byte) (TrustStore, error) {
	var entries struct {
		Taps     []string `json:"taps"`
		Formulae []string `json:"formulae"`
		Casks    []string `json:"casks"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return TrustStore{}, &Error{Message: fmt.Sprintf("failed to parse trust JSON: %v", err)}
	}
	set := func(names []string) map[string]bool {
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[strings.ToLower(n)] = true
		}
		return m
	}
	return TrustStore{
		taps:     set(entries.Taps),
		formulae: set(entries.Formulae),
		casks:    set(entries.Casks),
	}, nil
}

// TapTrusted reports whether the tap is trusted wholesale.
func (s TrustStore) TapTrusted(name string) bool {
	return s.taps[strings.ToLower(name)]
}

// PackageTrusted reports whether the package is trusted on its own.
func (s TrustStore) PackageTrusted(kind PackageKind, name string) bool {
	if kind == Cask {
		return s.casks[strings.ToLower(name)]
	}
	return s.formulae[strings.ToLower(name)]
}

// MarkTapTrusted records that the tap has become trusted wholesale, so a
// second package from the same tap does not repeat the work.
func (s *TrustStore) MarkTapTrusted(name string) {
	if s.taps == nil {
		s.taps = make(map[string]bool)
	}
	s.taps[strings.ToLower(name)] = true
}

// MarkPackageTrusted records that the package has become trusted on its
// own.
func (s *TrustStore) MarkPackageTrusted(kind PackageKind, name string) {
	if kind == Cask {
		if s.casks == nil {
			s.casks = make(map[string]bool)
		}
		s.casks[strings.ToLower(name)] = true
		return
	}
	if s.formulae == nil {
		s.formulae = make(map[string]bool)
	}
	s.formulae[strings.ToLower(name)] = true
}

// ReadTrustStore reads the per-user trust store with `brew trust --json=v1`.
// Homebrew before 6 has no `brew trust` command, so the read fails there
// and the caller gets an error alongside an empty store — an empty store
// never means "nothing is trusted" on its own.
func ReadTrustStore() (TrustStore, error) {
	output, err := runBrewCommand("trust", "--json=v1")
	if err != nil {
		return TrustStore{}, err
	}
	return parseTrustedPackages([]byte(output))
}

// TrustChange reports what preparing an install changed in the trust
// store.
type TrustChange int

const (
	// TrustUnchanged: nothing needed trusting.
	TrustUnchanged TrustChange = iota
	// TrustChangedPackage: one package was trusted on its own.
	TrustChangedPackage
	// TrustChangedTap: a whole tap was trusted.
	TrustChangedTap
)

// PrepareInstall makes a tap-qualified package installable: it clones the
// tap when the tap is missing, and it trusts the source when nothing
// trusts it yet.
//
// The trust decision is the narrow one by default. A package a person
// selected is trusted on its own, which is what Homebrew's trust model
// expects and what the selection itself consents to. The exception is a tap
// the collection's own Brewfile marks `trusted: true`: the distribution
// declared that tap safe, and `brew bundle` honours that by trusting the
// whole tap, so the per-item path honours it too.
//
// store is updated with whatever changed, so a second package from the same
// tap does not repeat the work. The returned change says what the trust
// store gained, so the caller can say so rather than escalating silently.
//
// A homebrew/core package needs neither a tap nor a trust and is left
// alone.
func PrepareInstall(name string, kind PackageKind, store *TrustStore, trustedTaps map[string]bool) (TrustChange, error) {
	i := strings.LastIndex(name, "/")
	if i <= 0 {
		return TrustUnchanged, nil
	}
	tapRepo := name[:i]

	state, err := TapInfo(tapRepo)
	if err != nil {
		// The tap's state could not be read. The install that follows
		// reports the real problem, so this only goes to the log.
		log.Printf("Could not read the state of tap %q: %v", tapRepo, err)
	} else {
		if !state.Installed {
			if err := Tap(tapRepo); err != nil {
				return TrustUnchanged, err
			}
		}
		if !state.HasTrust {
			// Homebrew before 6 has no tap trust at all: the package
			// loads as it is and nothing needs trusting.
			return TrustUnchanged, nil
		}
		if state.Trusted {
			return TrustUnchanged, nil
		}
	}
	if store.TapTrusted(tapRepo) {
		return TrustUnchanged, nil
	}
	if store.PackageTrusted(kind, name) {
		return TrustUnchanged, nil
	}
	if trustedTaps[tapRepo] {
		if err := TrustTap(tapRepo); err != nil {
			return TrustUnchanged, err
		}
		store.MarkTapTrusted(tapRepo)
		return TrustChangedTap, nil
	}
	if err := trustPackage(trustFlag(kind), name); err != nil {
		return TrustUnchanged, err
	}
	store.MarkPackageTrusted(kind, name)
	return TrustChangedPackage, nil
}

// trustFlag is the `brew trust` granularity for a package kind.
func trustFlag(kind PackageKind) string {
	if kind == Cask {
		return "--cask"
	}
	return "--formula"
}

// trustedPackages returns the per-package trust entries. A tap trusted only
// package by package still reports trusted: false in tap-info, so without
// this its trusted programs would be relisted as paused on every load.
// Homebrew releases without `brew trust --json=v1` yield an empty set: every
// installed package of an untrusted tap is then listed, as before.
func trustedPackages() TrustStore {
	store, err := ReadTrustStore()
	if err != nil {
		log.Printf("reading per-package brew trust failed: %v", err)
		return TrustStore{}
	}
	return store
}

// withoutTrusted drops the qualified names already in trusted.
func withoutTrusted(names []string, trusted map[string]bool) []string {
	if len(trusted) == 0 {
		return names
	}
	var kept []string
	for _, n := range names {
		if !trusted[strings.ToLower(n)] {
			kept = append(kept, n)
		}
	}
	return kept
}

// ListUntrustedTaps returns untrusted taps that have at least one package
// installed and not yet trusted on its own, with qualified package names
// ready for `brew trust`.
func ListUntrustedTaps() ([]UntrustedTap, error) {
	output, err := runBrewCommand("tap-info", "--installed", "--json")
	if err != nil {
		return nil, err
	}
	names, err := parseUntrustedTapNames([]byte(output))
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}

	prefix, err := brewPrefix()
	if err != nil {
		return nil, err
	}
	formulaeByTap := installedFormulaeByTap(filepath.Join(prefix, "Cellar"))
	casksByTap := installedCasksByTap(filepath.Join(prefix, "Caskroom"))
	trusted := trustedPackages()

	var result []UntrustedTap
	for _, name := range names {
		tap := UntrustedTap{
			Name:     name,
			Formulae: withoutTrusted(formulaeByTap[name], trusted.formulae),
			Casks:    withoutTrusted(casksByTap[name], trusted.casks),
		}
		if len(tap.Formulae) == 0 && len(tap.Casks) == 0 {
			continue // nothing installed from this tap; not actionable
		}
		sort.Strings(tap.Formulae)
		sort.Strings(tap.Casks)
		result = append(result, tap)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// UntrustedTapError indicates a brew command failed because packages come
// from untrusted taps (Homebrew 6 tap trust). Views should point users at
// the Untrusted Taps UI instead of dumping raw brew output.
type UntrustedTapError struct {
	Message string
	Tap     string
}

func (e *UntrustedTapError) Error() string {
	return e.Message
}

// isUntrustedTapMessage reports whether brew output complains about
// untrusted taps.
func isUntrustedTapMessage(s string) bool {
	if s == "" {
		return false
	}
	return strings.Contains(s, "untrusted tap") || strings.Contains(s, "taps are not trusted")
}

// TrustPackages trusts every installed package from the given tap using
// `brew trust`. Trust is per-user (~/.homebrew/trust.json); no root needed.
func TrustPackages(tap UntrustedTap) error {
	if len(tap.Formulae) > 0 {
		args := append([]string{"trust", "--formula"}, tap.Formulae...)
		if _, err := runBrewCommand(args...); err != nil {
			return err
		}
	}
	if len(tap.Casks) > 0 {
		args := append([]string{"trust", "--cask"}, tap.Casks...)
		if _, err := runBrewCommand(args...); err != nil {
			return err
		}
	}
	return nil
}

// TrustFormula trusts exactly one tap formula, by its qualified name, with
// `brew trust --formula`. Its tap must already be tapped. Per-user, like
// TrustPackages.
//
// It asks `brew tap-info --json` first and trusts only when brew reports the
// tap untrusted. Homebrew before 6 has no tap trust and no `brew trust`
// command — its tap-info carries no "trusted" key — so there the formula
// loads as is and running `brew trust` would only fail with an unknown
// command, aborting the install that needed nothing.
func TrustFormula(name string) error { return trustPackage("--formula", name) }

// TrustCask is TrustFormula for a tap cask: `brew trust --cask`, run only
// when brew reports the cask's tap untrusted.
func TrustCask(name string) error { return trustPackage("--cask", name) }

// TrustTap trusts a whole tap with `brew trust --tap <repo>`. It is the
// breadth `brew bundle` gives a tap its Brewfile marks `trusted: true`,
// and it is deliberately not the default: a package a person selected is
// normally trusted on its own.
//
// The caller must have established that the Homebrew in use has a trust
// concept at all; see TapState.HasTrust. Trust is per-user, never pkexec.
func TrustTap(name string) error {
	_, err := runBrewCommand("trust", "--tap", name)
	return err
}

// TapState is what Homebrew reports about one tap.
type TapState struct {
	Name      string
	Installed bool
	Trusted   bool
	// HasTrust reports whether Homebrew reported a trust state at all.
	// Homebrew before 6 has no tap trust, so its tap-info carries no
	// "trusted" key; there is nothing to trust and nothing to ask about.
	HasTrust bool
}

// TapInfo reads one tap's state with `brew tap-info --json <repo>`. The
// query answers for a repository that is not tapped yet, so deciding
// whether a tap has to be cloned needs no separate probe.
//
// The read runs with the GitHub API switched off, like every other
// tap-info query: its JSON also reports whether each tap is private, which
// brew asks the GitHub API, and on Linux that credential probe
// intermittently kills the command.
func TapInfo(name string) (TapState, error) {
	output, err := runBrewCommand("tap-info", "--json", name)
	if err != nil {
		return TapState{}, err
	}
	return parseTapInfo([]byte(output), name)
}

// parseTapInfo reads the tap-info entry for one repository. The "trusted"
// key is optional: Homebrew before 6 has no tap trust and omits it, and a
// missing key must read as "no trust concept" rather than "untrusted".
func parseTapInfo(data []byte, name string) (TapState, error) {
	var entries []struct {
		Name      string `json:"name"`
		Installed bool   `json:"installed"`
		Trusted   *bool  `json:"trusted"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return TapState{}, &Error{Message: fmt.Sprintf("failed to parse tap-info JSON: %v", err)}
	}
	for _, entry := range entries {
		if !strings.EqualFold(entry.Name, name) {
			continue
		}
		state := TapState{Name: entry.Name, Installed: entry.Installed}
		if entry.Trusted != nil {
			state.Trusted = *entry.Trusted
			state.HasTrust = true
		}
		return state, nil
	}
	return TapState{}, &Error{Message: fmt.Sprintf("tap %q was not found", name)}
}

func trustPackage(kind, name string) error {
	i := strings.LastIndex(name, "/")
	if i <= 0 {
		return &Error{Message: fmt.Sprintf("not a qualified tap package: %q", name)}
	}
	// A preview never tapped, so there is nothing to ask; the trust command
	// itself is skipped and logged by the dry-run gate.
	if dryrun.Enabled() {
		_, err := runBrewCommand("trust", kind, name)
		return err
	}
	output, err := runBrewCommand("tap-info", "--json", name[:i])
	if err != nil {
		return err
	}
	untrusted, err := parseUntrustedTapNames([]byte(output))
	if err != nil || len(untrusted) == 0 {
		return err
	}
	_, err = runBrewCommand("trust", kind, name)
	return err
}
