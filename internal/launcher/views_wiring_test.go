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
		required []string
	}{
		{
			name: "desktop application launcher",
			file: filepath.Join(repoRoot, "internal", "views", "applications_page.go"),
			required: []string{
				`cmd := exec.Command("gtk-launch", appID)`,
				`if err := launcher.Start(cmd, func(err error) {`,
				`sgtk.RunOnMainThread(func() {`,
				`uh.toastAdder.ShowErrorToast(fmt.Sprintf("Failed to launch %s", appID))`,
			},
		},
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
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			text := string(source)
			for _, required := range tt.required {
				if !strings.Contains(text, required) {
					t.Errorf("launcher wiring does not contain %q", required)
				}
			}
		})
	}
}
