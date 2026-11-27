package firstrun

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetupIntroVideoHonorsOverrideAndAbsence(t *testing.T) {
	previous := introVideoOverride
	t.Cleanup(func() { introVideoOverride = previous })

	// A present file reports its path.
	media := filepath.Join(t.TempDir(), "intro.mp4")
	if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}
	SetIntroVideoPath(media)
	if path, ok := IntroVideo(); !ok || path != media {
		t.Fatalf("IntroVideo() = (%q, %v), want (%q, true)", path, ok, media)
	}

	// A directory is not media.
	SetIntroVideoPath(filepath.Dir(media))
	if path, ok := IntroVideo(); ok {
		t.Fatalf("IntroVideo() = (%q, true), want absent for a directory", path)
	}

	// An empty override falls back to the fixed path the image installs.
	SetIntroVideoPath("")
	if path, _ := IntroVideo(); path != IntroVideoPath {
		t.Fatalf("IntroVideo() = %q, want the fixed %q", path, IntroVideoPath)
	}
}
