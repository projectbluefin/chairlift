package printerapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrinterDiagnosticsProbeInstalledReference(t *testing.T) {
	for _, image := range []string{"ghcr.io/projectbluefin/ghostscript-printer-app@sha256:" + strings.Repeat("a", 64), "ghcr.io/projectbluefin/ghostscript-printer-app:stable"} {
		t.Run(image, func(t *testing.T) {
			dir, _ := stubUnitDir(t)
			app := Select(Families()[0])
			installed := app
			installed.Family.Image = image
			if err := os.WriteFile(filepath.Join(dir, app.UnitName()), []byte(RenderUnit(installed)), 0o644); err != nil {
				t.Fatal(err)
			}
			previous := runCommand
			t.Cleanup(func() { runCommand = previous })
			runSystemctlOutput = func(context.Context, ...string) (string, error) { return "failed", nil }
			probed := false
			runCommand = func(_ context.Context, name string, args ...string) (string, error) {
				switch name {
				case "systemctl":
					return "SubState=failed\nResult=exit-code\nExecMainStatus=1\n", nil
				case "journalctl":
					return "trying to pull image before application exited", nil
				case "podman":
					probed = true
					if strings.Join(args, " ") != "image exists "+image {
						t.Errorf("image probe = %v; want installed %s", args, image)
						return "", errors.New("tag absent")
					}
				}
				return "", nil
			}
			if got := ProbeDiagnostics(context.Background(), app, true); got == StateFailedImage {
				t.Fatalf("cached installed image misclassified as unavailable: %v", got)
			}
			if !probed {
				t.Fatal("installed image was not probed")
			}
		})
	}
}
