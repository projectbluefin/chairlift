package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestViewsReportAsyncLauncherFailuresOnMainThread(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller could not locate views_wiring_test.go")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))

	for _, tt := range []struct {
		name     string
		file     string
		function string
		required []string
	}{
		{
			name: "URL launcher",
			file: filepath.Join(repoRoot, "internal", "views", "help_page.go"),
			required: []string{
				`cmd := exec.Command("xdg-open", url)`,
				`if err := launcher.Start(cmd, func(err error) {`,
				`sgtk.RunOnMainThread(func() {`,
				`uh.toastAdder.ShowErrorToast(fmt.Sprintf("Failed to open URL: %s", url))`,
			},
		},
		{
			name:     "desktop application launcher",
			file:     filepath.Join(repoRoot, "internal", "views", "troubleshoot.go"),
			function: "func (uh *UserHome) launchApp",
			required: []string{
				`cmd := exec.Command("gtk-launch", appID)`,
				`if err := launcher.Start(cmd, func(err error) {`,
				`sgtk.RunOnMainThread(func() {`,
				`uh.toastAdder.ShowErrorToast("Could not open that application")`,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			text := string(source)
			if tt.function != "" {
				text = sourceFunction(t, text, tt.function)
			}
			for _, required := range tt.required {
				if !strings.Contains(text, required) {
					t.Errorf("launcher wiring does not contain %q", required)
				}
			}
		})
	}
}

func sourceFunction(t *testing.T, source, declaration string) string {
	t.Helper()

	start := strings.Index(source, declaration)
	if start < 0 {
		t.Fatalf("source does not contain %q", declaration)
	}
	end := strings.Index(source[start+len(declaration):], "\nfunc ")
	if end < 0 {
		return source[start:]
	}
	return source[start : start+len(declaration)+end]
}
