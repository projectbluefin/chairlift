package pageview

import "strings"

// selfCaskToken is the Homebrew cask token this application ships under.
const selfCaskToken = "chairlift"

// IsSelfCask reports whether a Homebrew cask token, bare or tap-qualified
// (ublue-os/tap/chairlift), names this application's own cask. The Apps page
// offers no Uninstall for it: removing it from the page would delete the
// running application out from under the person using it (issue #493).
func IsSelfCask(token string) bool {
	if i := strings.LastIndex(token, "/"); i >= 0 {
		token = token[i+1:]
	}
	return token == selfCaskToken
}
