package installcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestErrorToastRendersAWrappingTitle pins the fix for issue #140's visible
// symptom. Every failure ChairLift reports reaches the user through
// Window.ShowErrorToast, and AdwToast's built-in title is a single ellipsized
// line: the reported bundle failure arrived as "Brew command failed:
// Installing io.podman_des…", with brew's actual diagnosis cut off. The toast
// therefore supplies a wrapping custom title. Losing any of those calls
// silently restores the single-line title and the truncation with it.
//
// It lives in internal/installcheck rather than internal/window because that
// package imports puregotk and cannot host a test binary on a headless host —
// see docs/agents/skills/gtk-headless-tests.md.
func TestErrorToastRendersAWrappingTitle(t *testing.T) {
	path := filepath.Join(RepoRoot(), "internal", "window", "window.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(source)

	for _, required := range []string{
		"func (w *Window) ShowErrorToast(message string) {",
		"title := gtk.NewLabel(message)",
		"title.SetWrap(true)",
		"title.SetMaxWidthChars(errorToastWidthChars)",
		"toast.SetCustomTitle(&title.Widget)",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("error toast wiring does not contain %q", required)
		}
	}

	// The plain toast carries short confirmations and must stay a one-line
	// AdwToast; only the error path pays for a custom widget.
	const plainSignature = "func (w *Window) ShowToast(message string) {"
	start := strings.Index(text, plainSignature)
	if start < 0 {
		t.Fatalf("%s no longer declares %s", path, plainSignature)
	}
	body := text[start+len(plainSignature):]
	end := strings.Index(body, "\n}")
	if end < 0 {
		t.Fatalf("%s: could not find the end of ShowToast", path)
	}
	if strings.Contains(body[:end], "SetCustomTitle") {
		t.Error("ShowToast gained a custom title; only ShowErrorToast needs one")
	}
}
