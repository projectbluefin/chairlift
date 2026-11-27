package firstrun

import "os"

// IntroVideoPath is where the OS image installs the setup assistant's
// introduction video.
//
// ChairLift does not ship the media: like the privileged helpers and the bootc
// stage script, the image supplies it at a fixed path, and the assistant
// consumes it when present. The path never comes from user input — it is
// handed to gio/xdg-open as a single argv element, so production keeps it a
// constant and never falls back to a working-directory-relative copy.
const IntroVideoPath = "/usr/share/chairlift/intro.mp4"

// introVideoOverride replaces IntroVideoPath in the chairlift_e2e build. The
// Dakota harness masks the fixed path with a read-only tmpfs, so a scenario
// cannot plant media there; the tagged override in internal/app points the
// assistant at a fixture instead.
var introVideoOverride string

// SetIntroVideoPath points the assistant at a different introduction video.
// The chairlift_e2e override calls it; an empty path restores the fixed
// IntroVideoPath.
func SetIntroVideoPath(path string) { introVideoOverride = path }

// IntroVideo returns the introduction video's path and whether it is present.
// A missing video is not an error: the assistant still runs, it simply omits
// the introduction and hides its button.
func IntroVideo() (string, bool) {
	path := IntroVideoPath
	if introVideoOverride != "" {
		path = introVideoOverride
	}
	info, err := os.Stat(path)
	return path, err == nil && !info.IsDir()
}
