package stageexec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptAvailableReportsFilePresence(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "stage-script")
	if err := os.WriteFile(present, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("writing stage script: %v", err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"installed script", present, true},
		{"absent script", filepath.Join(dir, "no-such-script"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ScriptAvailable(tc.path); got != tc.want {
				t.Errorf("ScriptAvailable(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
