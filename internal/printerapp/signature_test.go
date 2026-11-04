package printerapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestVerifiedImageUsesEachFamiliesOwnWorkflow(t *testing.T) {
	_, _ = stubUnitDir(t)
	digest := "sha256:" + strings.Repeat("b", 64)
	for _, family := range Families() {
		t.Run(family.ID, func(t *testing.T) {
			runCosign = func(_ context.Context, args ...string) ([]byte, error) {
				want := []string{"verify", "--output=json", "--certificate-oidc-issuer", "https://token.actions.githubusercontent.com", "--certificate-identity-regexp", `^https://github\.com/projectbluefin/` + family.ID + `-printer-app/\.github/workflows/registry-actions\.yml@refs/heads/main$`, family.Image}
				if !reflect.DeepEqual(args, want) {
					t.Fatalf("cosign args = %q, want %q", args, want)
				}
				return []byte(`[{"critical":{"image":{"docker-manifest-digest":"` + digest + `"}}}]`), nil
			}
			got, err := verifiedImage(context.Background(), family)
			want := strings.TrimSuffix(family.Image, ":stable") + "@" + digest
			if err != nil || got != want {
				t.Fatalf("verifiedImage = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestSignatureFailuresDoNotWriteOrStart(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		err          error
	}{
		{name: "unsigned or wrong signer", err: errors.New("signature rejected")},
		{name: "missing cosign", err: os.ErrNotExist},
		{name: "canceled", err: context.Canceled},
		{name: "malformed", output: "not json"},
		{name: "no signatures", output: "[]"},
		{name: "missing digest", output: `[{"critical":{}}]`},
		{name: "bad digest", output: `[{"critical":{"image":{"docker-manifest-digest":"sha256:wrong"}}}]`},
		{name: "conflicting digests", output: `[{"critical":{"image":{"docker-manifest-digest":"sha256:` + strings.Repeat("a", 64) + `"}}},{"critical":{"image":{"docker-manifest-digest":"sha256:` + strings.Repeat("b", 64) + `"}}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, calls := stubUnitDir(t)
			app := Select(Families()[0])
			runCosign = func(context.Context, ...string) ([]byte, error) { return []byte(tc.output), tc.err }
			if err := Enable(context.Background(), app); !errors.Is(err, ErrSignatureUnverified) {
				t.Fatalf("Enable = %v", err)
			}
			if IsEnabled(app) || len(*calls) != 0 {
				t.Fatalf("failed verify changed state: %v", *calls)
			}
			path := filepath.Join(dir, app.UnitName())
			before := []byte("previous unit\n")
			if err := os.WriteFile(path, before, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := Refresh(context.Background(), app); !errors.Is(err, ErrSignatureUnverified) {
				t.Fatalf("Refresh = %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) || len(*calls) != 0 {
				t.Fatalf("failed refresh changed previous state: %q, %v, %v", after, err, *calls)
			}
		})
	}
}

func TestRefreshReplacesLegacyTagWithVerifiedPin(t *testing.T) {
	dir, calls := stubUnitDir(t)
	app := Select(Families()[0])
	if err := os.WriteFile(filepath.Join(dir, app.UnitName()), []byte("Image="+app.Family.Image+"\nAutoUpdate=registry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Refresh(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, app.UnitName()))
	if err != nil || strings.Contains(string(data), "AutoUpdate=") || !strings.Contains(string(data), "@sha256:") {
		t.Fatalf("refreshed unit: %s, %v", data, err)
	}
	if want := []string{"daemon-reload", "restart " + app.ServiceName()}; !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestPinnedOverrideStillRequiresVerification(t *testing.T) {
	_, _ = stubUnitDir(t)
	family := Families()[0]
	family.Image = "mirror.example/printer@sha256:" + strings.Repeat("a", 64)
	got, err := verifiedImage(context.Background(), family)
	if err != nil || got != family.Image {
		t.Fatalf("verified pin = %q, %v", got, err)
	}
	family.Image = "mirror.example/printer@sha256:" + strings.Repeat("b", 64)
	if _, err := verifiedImage(context.Background(), family); !errors.Is(err, ErrSignatureUnverified) {
		t.Fatalf("different digest accepted: %v", err)
	}
	family.ID = "untrusted"
	if _, err := verifiedImage(context.Background(), family); !errors.Is(err, ErrSignatureUnverified) {
		t.Fatalf("unknown family accepted: %v", err)
	}
}

func TestCosignRunnerSeparatesDiagnosticsAndReportsFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	path := filepath.Join(dir, "cosign")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '[]'\nprintf 'verification diagnostics' >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	output, err := execCosign(context.Background(), "verify")
	if err != nil || string(output) != "[]" {
		t.Fatalf("stdout = %q, error = %v", output, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := execCosign(context.Background(), "verify"); err == nil {
		t.Fatal("ignored unsuccessful verification")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := execCosign(ctx, "verify"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled verification = %v", err)
	}
}

func TestRefreshAbsentAndDryRunDoNotVerify(t *testing.T) {
	_, _ = stubUnitDir(t)
	runCosign = func(context.Context, ...string) ([]byte, error) { t.Fatal("unexpected verification"); return nil, nil }
	app := Select(Families()[0])
	if err := Refresh(context.Background(), app); err == nil {
		t.Fatal("refreshed absent unit")
	}
	dryrun.Set(true)
	if err := Enable(context.Background(), app); err != nil {
		t.Fatal(err)
	}
}
