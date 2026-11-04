package printerapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrinterAppInstalledImageReadsTheUnitsImage(t *testing.T) {
	dir, _ := stubUnitDir(t)
	app := Select(Families()[0])
	installed := app
	installed.Family.Image = "ghcr.io/projectbluefin/ghostscript-printer-app@sha256:" + strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(dir, app.UnitName()), []byte(RenderUnit(installed)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := InstalledImage(app)
	if err != nil || got != installed.Family.Image {
		t.Fatalf("InstalledImage = %q, %v; want %q", got, err, installed.Family.Image)
	}
}

func TestPrinterAppInstalledImageFailsWithoutUnitOrImage(t *testing.T) {
	dir, _ := stubUnitDir(t)
	app := Select(Families()[0])
	if _, err := InstalledImage(app); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("InstalledImage without unit = %v; want not exist", err)
	}
	if err := os.WriteFile(filepath.Join(dir, app.UnitName()), []byte("[Container]\nImage=\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstalledImage(app); err == nil {
		t.Fatal("InstalledImage accepted a unit without an image")
	}
}

func TestVerifiedImagePinsTheVerifiedDigestWithoutWriting(t *testing.T) {
	_, calls := stubUnitDir(t)
	app := Select(Families()[0])
	digest := "sha256:" + strings.Repeat("c", 64)
	runCosign = func(context.Context, ...string) ([]byte, error) {
		return []byte(`[{"critical":{"image":{"docker-manifest-digest":"` + digest + `"}}}]`), nil
	}
	got, err := VerifiedImage(context.Background(), app)
	want := strings.TrimSuffix(app.Family.Image, ":stable") + "@" + digest
	if err != nil || got != want {
		t.Fatalf("VerifiedImage = %q, %v; want %q", got, err, want)
	}
	if IsEnabled(app) || len(*calls) != 0 {
		t.Fatalf("VerifiedImage changed state: %v", *calls)
	}

	runCosign = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("rejected") }
	if _, err := VerifiedImage(context.Background(), app); !errors.Is(err, ErrSignatureUnverified) {
		t.Fatalf("VerifiedImage = %v; want ErrSignatureUnverified", err)
	}
}
