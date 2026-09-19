package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

var (
	executablePath = os.Executable
	workingDir     = os.Getwd
	statPath       = os.Stat
)

// trustedConfigDirectories are the only directories from which privilege-escalating
// actions (sudo: true) may be loaded.
var trustedConfigDirectories = []string{
	"/etc/chairlift",
	"/usr/share/chairlift",
}

// configSource pairs a candidate's resolved path with the provenance decision
// made for it. Trust is decided once, before the file is read, and then travels
// with the bytes through parsing and validation: no later stage re-inspects the
// filesystem, so a candidate cannot be re-pointed between read and validation to
// launder untrusted content as trusted.
type configSource struct {
	path    string
	trusted bool
}

// isTrustedCandidate reports whether candidate is one of the fixed trusted
// entries in configPaths. The comparison is on the candidate literal, not on
// anything the filesystem says about it, so a candidate cannot be promoted to
// trusted by pointing it somewhere else.
func isTrustedCandidate(candidate string) bool {
	for _, trusted := range trustedConfigPaths {
		if candidate == trusted {
			return true
		}
	}
	return false
}

// isTrustedConfigPath reports whether path resides in an administrator- or package-owned
// location permitted to define sudo actions. Symlinks are resolved to prevent
// untrusted paths from escaping through trusted symlinks. It is the provenance
// rule for an explicitly supplied path; Load() never uses it, deriving trust from
// which fixed configPaths candidate matched instead.
//
// Resolution failures other than a missing entry (permission denied, symlink
// loops) fail closed: an unresolvable path that exists cannot be proven trusted.
// A missing entry keeps the lexical path, which cannot be read anyway.
func isTrustedConfigPath(path string) bool {
	clean, ok := resolveForTrust(filepath.Clean(path))
	if !ok {
		return false
	}
	for _, dir := range trustedConfigDirectories {
		cleanDir, ok := resolveForTrust(filepath.Clean(dir))
		if !ok {
			continue
		}
		if clean == cleanDir || filepath.Dir(clean) == cleanDir {
			return true
		}
	}
	return false
}

// resolveForTrust resolves clean's symlinks for a provenance comparison. It
// reports false when resolution fails for any reason other than the entry not
// existing, so callers fail closed rather than falling back to a lexical path an
// attacker may have made meaningless.
func resolveForTrust(clean string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(clean)
	switch {
	case err == nil:
		return resolved, true
	case errors.Is(err, fs.ErrNotExist):
		return clean, true
	default:
		return "", false
	}
}

// resolveCandidatePath returns the exact path loadFromPath will read and a
// diagnostic will report. Relative candidates prefer a file alongside the
// executable, preserving ChairLift's existing development behavior, then
// fall back to an absolute path under the current working directory.
func resolveCandidatePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	if executable, err := executablePath(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), path)
		if _, err := statPath(candidate); err == nil || !errors.Is(err, fs.ErrNotExist) {
			return filepath.Clean(candidate)
		}
	}

	if cwd, err := workingDir(); err == nil {
		return filepath.Join(cwd, path)
	}

	// A failed Getwd leaves no absolute base to use. Keep the cleaned relative
	// path so the subsequent read still returns a classified, actionable error.
	return filepath.Clean(path)
}
