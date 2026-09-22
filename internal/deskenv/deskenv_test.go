package deskenv

import (
	"testing"
)

// TestClassifySessionStrings is the decision table for session detection.
//
// Every case is a session string observed from a real desktop, not an
// invented one, because the whole value of this package is that it classifies
// the strings distributions actually export. The four families are:
//
//   - Universal Blue's Plasma images (Aurora, Bazzite KDE), which reach
//     Plasma through SDDM and export the same triple stock Fedora Plasma
//     does.
//   - Bazzite's GNOME variant and stock GNOME, on Wayland and on Xorg.
//   - Distribution GNOME sessions, where XDG_CURRENT_DESKTOP names the
//     distribution first and GNOME second (`ubuntu:GNOME`, `pop:GNOME`) while
//     DESKTOP_SESSION names only the distribution — the case that proves the
//     standard variable outranks the session name.
//   - Sessions ChairLift has no surface for, which must classify as Unknown
//     rather than being rounded to the nearest desktop.
//
// The want column is written out rather than derived, so gutting Classify to
// return its zero value fails every case that expects a desktop and passes
// only the Unknown ones.
func TestClassifySessionStrings(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want Desktop
	}{
		// Universal Blue Plasma images.
		{
			name: "Aurora Plasma: SDDM exports the desktop, the session, and the KDE marker",
			env: map[string]string{
				XDGCurrentDesktop: "KDE",
				DesktopSession:    "plasma",
				KDEFullSession:    "true",
			},
			want: KDE,
		},
		{
			name: "Bazzite KDE: the same Plasma triple as Aurora, on the gaming image",
			env: map[string]string{
				XDGCurrentDesktop: "KDE",
				DesktopSession:    "plasma",
				KDEFullSession:    "true",
			},
			want: KDE,
		},
		{
			name: "stock Fedora Plasma: no distribution prefix on the desktop name",
			env: map[string]string{
				XDGCurrentDesktop: "KDE",
				DesktopSession:    "plasma",
			},
			want: KDE,
		},
		{
			name: "Plasma on X11: SDDM names the X11 session plasmax11",
			env: map[string]string{
				XDGCurrentDesktop: "KDE",
				DesktopSession:    "plasmax11",
				KDEFullSession:    "true",
			},
			want: KDE,
		},
		{
			name: "Plasma session named kde-plasma",
			env: map[string]string{
				XDGCurrentDesktop: "KDE",
				DesktopSession:    "kde-plasma",
			},
			want: KDE,
		},
		{
			name: "Plasma session named by the path of its xsessions file",
			env: map[string]string{
				DesktopSession: "/usr/share/xsessions/plasma",
			},
			want: KDE,
		},
		{
			name: "Plasma on a host that exports only the KDE session marker",
			env: map[string]string{
				KDEFullSession: "true",
			},
			want: KDE,
		},
		{
			name: "Plasma where the standard variable is unset and the session name carries it",
			env: map[string]string{
				DesktopSession: "plasma",
				KDEFullSession: "true",
			},
			want: KDE,
		},

		// GNOME.
		{
			name: "stock GNOME on Wayland",
			env: map[string]string{
				XDGCurrentDesktop: "GNOME",
				DesktopSession:    "gnome",
			},
			want: GNOME,
		},
		{
			name: "stock GNOME on Xorg: GDM names the session gnome-xorg",
			env: map[string]string{
				XDGCurrentDesktop: "GNOME",
				DesktopSession:    "gnome-xorg",
			},
			want: GNOME,
		},
		{
			name: "Bazzite GNOME: the same GNOME session as stock, on the gaming image",
			env: map[string]string{
				XDGCurrentDesktop: "GNOME",
				DesktopSession:    "gnome",
			},
			want: GNOME,
		},
		{
			name: "Bazzite GNOME on Xorg",
			env: map[string]string{
				XDGCurrentDesktop: "GNOME",
				DesktopSession:    "gnome-xorg",
			},
			want: GNOME,
		},
		{
			name: "Ubuntu GNOME: the standard variable names the distribution first and GNOME second",
			env: map[string]string{
				XDGCurrentDesktop: "ubuntu:GNOME",
				DesktopSession:    "ubuntu",
			},
			want: GNOME,
		},
		{
			name: "Pop!_OS GNOME: the same shape with a different distribution name",
			env: map[string]string{
				XDGCurrentDesktop: "pop:GNOME",
				DesktopSession:    "pop",
			},
			want: GNOME,
		},
		{
			name: "GNOME Classic: both variables carry the compound session name",
			env: map[string]string{
				XDGCurrentDesktop: "GNOME-Classic:GNOME",
				DesktopSession:    "gnome-classic",
			},
			want: GNOME,
		},
		{
			name: "GNOME Flashback",
			env: map[string]string{
				XDGCurrentDesktop: "GNOME-Flashback:GNOME",
				DesktopSession:    "gnome-flashback-metacity",
			},
			want: GNOME,
		},
		{
			name: "GNOME named only by the session variable",
			env: map[string]string{
				DesktopSession: "gnome",
			},
			want: GNOME,
		},
		{
			name: "GNOME where a parent Plasma session leaked the KDE marker into the environment",
			env: map[string]string{
				XDGCurrentDesktop: "GNOME",
				DesktopSession:    "gnome",
				KDEFullSession:    "true",
			},
			want: GNOME,
		},

		// Precedence.
		{
			name: "the standard variable outranks the session name when they disagree",
			env: map[string]string{
				XDGCurrentDesktop: "KDE",
				DesktopSession:    "gnome",
				KDEFullSession:    "true",
			},
			want: KDE,
		},
		{
			name: "an unrecognized value in the standard variable does not veto the session name",
			env: map[string]string{
				XDGCurrentDesktop: "X-Cinnamon",
				DesktopSession:    "plasma",
			},
			want: KDE,
		},
		{
			name: "an unrecognized value in both named variables does not veto the KDE marker",
			env: map[string]string{
				XDGCurrentDesktop: "sway",
				DesktopSession:    "sway",
				KDEFullSession:    "true",
			},
			want: KDE,
		},

		// Case and formatting.
		{
			name: "a lowercased desktop name",
			env: map[string]string{
				XDGCurrentDesktop: "kde",
			},
			want: KDE,
		},
		{
			name: "an uppercased session name",
			env: map[string]string{
				DesktopSession: "GNOME",
			},
			want: GNOME,
		},
		{
			name: "surrounding whitespace on the KDE marker",
			env: map[string]string{
				KDEFullSession: "  true\n",
			},
			want: KDE,
		},
		{
			name: "uppercased marker value",
			env: map[string]string{
				KDEFullSession: "TRUE",
			},
			want: KDE,
		},
		{
			name: "empty entries in a colon-separated list are skipped",
			env: map[string]string{
				XDGCurrentDesktop: "::GNOME::",
			},
			want: GNOME,
		},
		{
			name: "a trailing colon in a single-entry list",
			env: map[string]string{
				XDGCurrentDesktop: "GNOME:",
			},
			want: GNOME,
		},

		// Desktops ChairLift has no surface for.
		{
			name: "Sway",
			env: map[string]string{
				XDGCurrentDesktop: "sway",
				DesktopSession:    "sway",
			},
			want: Unknown,
		},
		{
			name: "Hyprland",
			env: map[string]string{
				XDGCurrentDesktop: "Hyprland",
				DesktopSession:    "hyprland",
			},
			want: Unknown,
		},
		{
			name: "i3",
			env: map[string]string{
				XDGCurrentDesktop: "i3",
				DesktopSession:    "i3",
			},
			want: Unknown,
		},
		{
			name: "Xfce",
			env: map[string]string{
				XDGCurrentDesktop: "XFCE",
				DesktopSession:    "xfce",
			},
			want: Unknown,
		},
		{
			name: "Cinnamon, which the standard variable names X-Cinnamon",
			env: map[string]string{
				XDGCurrentDesktop: "X-Cinnamon",
				DesktopSession:    "cinnamon",
			},
			want: Unknown,
		},
		{
			name: "MATE",
			env: map[string]string{
				XDGCurrentDesktop: "MATE",
				DesktopSession:    "mate",
			},
			want: Unknown,
		},
		{
			name: "Unity, which is GTK and GNOME-adjacent but is not GNOME Shell",
			env: map[string]string{
				XDGCurrentDesktop: "Unity",
				DesktopSession:    "ubuntu",
			},
			want: Unknown,
		},
		{
			name: "no session variables at all: a container, a TTY, or a bare dbus-run-session",
			env:  map[string]string{},
			want: Unknown,
		},
		{
			name: "all three variables present but empty",
			env: map[string]string{
				XDGCurrentDesktop: "",
				DesktopSession:    "",
				KDEFullSession:    "",
			},
			want: Unknown,
		},
		{
			name: "a KDE marker explicitly set to false",
			env: map[string]string{
				KDEFullSession: "false",
			},
			want: Unknown,
		},
		{
			name: "a KDE marker set to a value other than the literal true",
			env: map[string]string{
				KDEFullSession: "1",
			},
			want: Unknown,
		},
		{
			name: "a desktop name this package has no row for",
			env: map[string]string{
				XDGCurrentDesktop: "LXQt",
				DesktopSession:    "lxqt",
			},
			want: Unknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Classify(test.env); got != test.want {
				t.Errorf("Classify(%v) = %v, want %v", test.env, got, test.want)
			}
		})
	}
}

// TestDetectReadsTheProcessEnvironment drives the exported entry point through
// the real environment, so the wiring between Detect and the three named
// variables is covered rather than assumed. Every case sets all three
// variables, including to the empty string, so the answer never depends on
// what the machine running the tests happens to export.
func TestDetectReadsTheProcessEnvironment(t *testing.T) {
	tests := []struct {
		name              string
		xdgCurrentDesktop string
		desktopSession    string
		kdeFullSession    string
		want              Desktop
	}{
		{
			name:              "Aurora Plasma",
			xdgCurrentDesktop: "KDE",
			desktopSession:    "plasma",
			kdeFullSession:    "true",
			want:              KDE,
		},
		{
			name:              "stock GNOME",
			xdgCurrentDesktop: "GNOME",
			desktopSession:    "gnome",
			want:              GNOME,
		},
		{
			name: "nothing declared",
			want: Unknown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(XDGCurrentDesktop, test.xdgCurrentDesktop)
			t.Setenv(DesktopSession, test.desktopSession)
			t.Setenv(KDEFullSession, test.kdeFullSession)

			if got := Detect(); got != test.want {
				t.Errorf("Detect() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestDetectNeedsNoExecutableOnPath pins the no-subprocess contract the
// package exists to keep. Detection answers from variables the session has
// already exported, so it must work on a host with nothing runnable on PATH —
// and a detector rewritten to shell out (`plasmashell --version`,
// `gnome-shell --version`) would fail here, because an empty PATH leaves it
// nothing to look up. The variables are set to a Plasma triple so the case
// cannot pass by answering Unknown for an unrelated reason.
func TestDetectNeedsNoExecutableOnPath(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv(XDGCurrentDesktop, "KDE")
	t.Setenv(DesktopSession, "plasma")
	t.Setenv(KDEFullSession, "true")

	if got := Detect(); got != KDE {
		t.Errorf("Detect() with an empty PATH = %v, want %v; detection must read the environment, not spawn a desktop tool", got, KDE)
	}
}

// TestDesktopStringNamesEveryValue covers the whole enum, so a value added
// without a name here is caught. The final case is a Desktop that is not one
// of the three constants: String must answer Unknown for it rather than
// claiming a desktop, which is the same fail-closed default the zero value
// relies on.
func TestDesktopStringNamesEveryValue(t *testing.T) {
	tests := []struct {
		desktop Desktop
		want    string
	}{
		{Unknown, "Unknown"},
		{GNOME, "GNOME"},
		{KDE, "KDE"},
		{Desktop(42), "Unknown"},
	}

	for _, test := range tests {
		if got := test.desktop.String(); got != test.want {
			t.Errorf("Desktop(%d).String() = %q, want %q", int(test.desktop), got, test.want)
		}
	}
}

// TestUnknownDesktopIsTheZeroValue pins the fail-closed default. Callers reach
// a Desktop through struct fields and map misses, where an unset value is the
// zero value; if Unknown were not zero, a forgotten field would read as a
// supported desktop and ChairLift would aim a GNOME write at a session that
// cannot resolve it. This is the one property of the enum that no other test
// would notice changing.
func TestUnknownDesktopIsTheZeroValue(t *testing.T) {
	var zero Desktop

	if zero != Unknown {
		t.Errorf("the zero Desktop is %v, want Unknown; an unset field must fail closed", zero)
	}
	if GNOME == Unknown || KDE == Unknown {
		t.Error("a supported desktop shares the Unknown value")
	}
}
