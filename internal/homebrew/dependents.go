package homebrew

import (
	"regexp"
	"strings"
)

// DependentsError reports that `brew uninstall` refused because installed
// packages still need the package being removed. Dependents names them as
// Homebrew printed them, so the caller can say what is in the way instead of
// offering a retry that fails the same way.
type DependentsError struct {
	Message    string
	Dependents []string
}

func (e *DependentsError) Error() string {
	return e.Message
}

// uninstallDependentsRe matches the refusal Homebrew's DependentsMessage
// prints (Library/Homebrew/dependents_message.rb) for formulae and casks:
//
//	Error: Refusing to uninstall <packages>
//	because it is required by <dependents>, which is currently installed.
//
// with "they are" and "which are" for several. The required packages are
// printed as Cellar paths for formulae, so only the dependents are captured.
var uninstallDependentsRe = regexp.MustCompile(`(?m)^Error: Refusing to uninstall .+\nbecause (?:it is|they are) required by (.+), which (?:is|are) currently installed\.`)

// dependentNameRe is the character set of a formula name, cask token, or
// tap-qualified name. A name outside it means the line is not the message
// above, and the refusal stays unclassified rather than half-parsed.
var dependentNameRe = regexp.MustCompile(`^[A-Za-z0-9@+._/-]+$`)

// uninstallDependents reads the dependents out of `brew uninstall`'s stderr.
// Homebrew joins them as "a", "a and b", or "a, b and c".
func uninstallDependents(stderr string) ([]string, bool) {
	m := uninstallDependentsRe.FindStringSubmatch(stderr)
	if m == nil {
		return nil, false
	}
	parts := strings.Split(m[1], ", ")
	last := parts[len(parts)-1]
	parts = parts[:len(parts)-1]
	if head, tail, ok := strings.Cut(last, " and "); ok {
		parts = append(parts, head, tail)
	} else {
		parts = append(parts, last)
	}
	for _, name := range parts {
		if !dependentNameRe.MatchString(name) {
			return nil, false
		}
	}
	return parts, true
}
