// Package deskenv identifies the desktop environment the current session is
// running, so ChairLift can tell whether a desktop-specific write has a
// surface to land on.
//
// It exists because ChairLift's livery marks are GNOME surfaces. The app-grid
// mark is written into the Adwaita icon theme and the panel mark into the
// `org.gnome.shell.extensions.custom-command-list` schema, neither of which a
// Plasma session resolves. On Plasma every write therefore succeeds and
// nothing changes — the worst outcome, because the UI reports success and the
// desktop is untouched. Knowing the environment is what lets those groups be
// omitted instead of silently no-op'ing.
//
// Detection is an environment-variable read, never a process spawn. The three
// variables below are already exported into the session by the display
// manager or the session manager, so reading them costs nothing, needs no
// privilege, and answers on a machine where no desktop tool is installed.
// Shelling out (`gnome-shell --version`, `plasmashell --version`) would be
// slower, would fail on a minimal host, and would report which desktop is
// installed rather than which one is running.
//
// Only these three variables are consulted. `XDG_SESSION_DESKTOP` is
// deliberately not among them: it duplicates `XDG_CURRENT_DESKTOP` wherever
// it is set, and every variable added to this decision is another value that
// can disagree with the others and has to be ranked.
package deskenv

import (
	"os"
	"strings"
	"unicode"
)

// The session variables this package reads, named once so no call site spells
// the literals.
const (
	// XDGCurrentDesktop is the freedesktop.org standard variable. It holds a
	// colon-separated list, most general first, so `ubuntu:GNOME` names both
	// the distribution's session and the desktop underneath it.
	XDGCurrentDesktop = "XDG_CURRENT_DESKTOP"
	// DesktopSession is the session manager's own name for the session. It is
	// not standardized: SDDM and GDM write the session file's base name
	// (`plasma`, `gnome`, `plasmax11`), and a session started by path can
	// carry the full path instead.
	DesktopSession = "DESKTOP_SESSION"
	// KDEFullSession is set to the literal `true` by Plasma's session manager
	// (ksmserver) for the whole session, including processes it starts for
	// other toolkits. It is the oldest of the three and the last resort on a
	// host where the other two are unset or name something unrecognized.
	KDEFullSession = "KDE_FULL_SESSION"
)

// Desktop is a desktop environment ChairLift can classify a session as.
//
// The zero value is Unknown on purpose. A zero-valued Desktop reaches this
// package's callers through a struct field or a map miss far more easily than
// through an explicit assignment, and Unknown is the answer that fails closed:
// it omits a desktop-specific write rather than aiming a GNOME write at a
// session that cannot resolve it. Every other ordering would make a forgotten
// field mean "this is GNOME" and reintroduce the silent no-op this package
// exists to prevent.
type Desktop int

const (
	// Unknown means the session did not declare a desktop this package
	// recognizes — a compositor with no ChairLift surface (Sway, i3,
	// Xfce, Cinnamon), a session whose variables are all unset (a bare
	// `dbus-run-session`, a container, a TTY), or a declaration this
	// package has no table row for. It is not an error and must not be
	// treated as one: an unrecognized desktop is a desktop ChairLift
	// simply has nothing to offer.
	Unknown Desktop = iota
	// GNOME is GNOME Shell and the sessions built on it, including GNOME
	// Classic, GNOME Flashback, and the distribution sessions that name
	// GNOME second (`ubuntu:GNOME`, `pop:GNOME`).
	GNOME
	// KDE is Plasma, named either as the desktop (`KDE`) or as the session
	// (`plasma`, `plasmax11`, `kde-plasma`).
	KDE
)

// String returns the desktop's name as it appears in this package's
// constants, so a log line or a diagnostic reads the same as the code.
func (d Desktop) String() string {
	switch d {
	case GNOME:
		return "GNOME"
	case KDE:
		return "KDE"
	default:
		return "Unknown"
	}
}

// Detect identifies the desktop environment of the current session from the
// process environment.
func Detect() Desktop {
	return Classify(map[string]string{
		XDGCurrentDesktop: os.Getenv(XDGCurrentDesktop),
		DesktopSession:    os.Getenv(DesktopSession),
		KDEFullSession:    os.Getenv(KDEFullSession),
	})
}

// Classify decides the desktop from the raw session variables. It is the pure
// half of detection: every session string is covered by a table test, and no
// test needs a desktop to be running.
//
// It takes a map keyed by the variable names rather than three positional
// strings because all three values are the same type and carry the same kind
// of text. A transposed argument in a positional signature is not a compile
// error and does not look wrong at the call site — it is a session classified
// as the wrong desktop, which is exactly the silent misbehavior this package
// exists to remove. A missing key and an empty value are the same thing here:
// no declaration, which is how an unset variable arrives.
//
// Variables are consulted in order of authority — XDG_CURRENT_DESKTOP, then
// DESKTOP_SESSION, then KDE_FULL_SESSION — and the first one that names a
// recognized desktop decides. A variable that is unset, empty, or names
// something unrecognized does not end the search, because each variable is an
// independent declaration: a session that sets `X-Cinnamon` in the standard
// variable has made no claim at all about the two others, and a later one may
// still be the specific answer. Precedence matters in the other direction
// too: on Ubuntu, DESKTOP_SESSION is `ubuntu` while XDG_CURRENT_DESKTOP is
// `ubuntu:GNOME`, so reading the session name first would find nothing and
// then blame the search order for the miss.
func Classify(env map[string]string) Desktop {
	for _, variable := range []string{XDGCurrentDesktop, DesktopSession} {
		for _, token := range sessionTokens(env[variable]) {
			if desktop, ok := classifyToken(token); ok {
				return desktop
			}
		}
	}
	if isTrue(env[KDEFullSession]) {
		return KDE
	}
	return Unknown
}

// sessionTokens splits a session variable into the words that can name a
// desktop. The list variable is colon-separated and a session name may be a
// path, so the split is on every rune that is neither a letter nor a digit
// rather than on a chosen delimiter: `ubuntu:GNOME` yields `ubuntu` and
// `gnome`, `/usr/share/xsessions/plasma` yields `plasma`, and `gnome-classic`
// yields `gnome` and `classic`. Splitting this way keeps a compound name from
// hiding the desktop inside it, whichever punctuation a distribution used to
// build it.
func sessionTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// classifyToken maps one session word to a desktop. The match is by prefix
// rather than by whole word because the sources concatenate a variant onto
// the desktop's name: GDM ships `gnome-classic` and `gnome-xorg`, and SDDM
// ships `plasmax11` for the X11 session. After sessionTokens has split on
// punctuation, those arrive as `gnome` + `classic` and `plasmax11`
// respectively, and the prefix covers the second shape.
//
// These three prefixes are unambiguous: no other desktop identifier in
// circulation begins `gnome`, `kde`, or `plasma`. A session named `gNOME` or
// `Plasma` classifies the same way, because the token arrives lowercased.
func classifyToken(token string) (Desktop, bool) {
	switch {
	case strings.HasPrefix(token, "gnome"):
		return GNOME, true
	case strings.HasPrefix(token, "kde"), strings.HasPrefix(token, "plasma"):
		return KDE, true
	default:
		return Unknown, false
	}
}

// isTrue reports whether KDE_FULL_SESSION carries Plasma's session-wide
// marker. ksmserver writes the literal `true`, so only that value counts: any
// other non-empty text is a value this package does not recognize, and an
// unrecognized declaration must not become a KDE answer.
func isTrue(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}
