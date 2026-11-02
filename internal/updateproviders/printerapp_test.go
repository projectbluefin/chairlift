package updateproviders

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/printerapp"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

func TestPrinterProviderID(t *testing.T) {
	provider := newPrinterApp(PrinterDeps{}).(*printerProvider)
	if provider.ID() != updateflow.Printers {
		t.Fatalf("provider ID = %q, want %q", provider.ID(), updateflow.Printers)
	}
}

func TestPrinterProviderAvailable(t *testing.T) {
	provider := newPrinterApp(PrinterDeps{}).(*printerProvider)

	// Availability follows Podman presence (the Printers group floor), not the
	// per-source Enabled toggle, so it must track printerapp.IsAvailable().
	if provider.Available() != printerapp.IsAvailable() {
		t.Fatal("provider availability must follow the Printers group (Podman present)")
	}
}

func TestPrinterCheckSkipsDisabledFamilies(t *testing.T) {
	provider := newPrinterApp(PrinterDeps{Enabled: func(printerapp.App) bool { return false }})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("Check() items = %#v, want none (no family enabled)", got.Items)
	}
}

func TestPrinterCheckReportsUpdateWhenVerifiedDigestDiverges(t *testing.T) {
	installed := "ghcr.io/projectbluefin/hplip-printer-app:stable"
	verified := "ghcr.io/projectbluefin/hplip-printer-app@sha256:" + repeat('a', 64)

	provider := newPrinterApp(PrinterDeps{
		Enabled: func(app printerapp.App) bool { return app.Family.ID == "hplip" },
		InstalledImage: func(app printerapp.App) (string, error) {
			if app.Family.ID != "hplip" {
				t.Fatalf("InstalledImage called for disabled family %q", app.Family.ID)
			}
			return installed, nil
		},
		VerifiedImage: func(_ context.Context, family printerapp.Family) (string, error) {
			if family.ID != "hplip" {
				t.Fatalf("VerifiedImage called for family %q", family.ID)
			}
			return verified, nil
		},
	})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	want := updateflow.CheckResult{Items: []updateflow.Item{{
		ID:               "hplip",
		Name:             "HPLIP",
		CurrentVersion:   installed,
		AvailableVersion: verified,
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Check() = %#v, want %#v", got, want)
	}
}

func TestPrinterCheckNoItemWhenInstalledMatchesVerified(t *testing.T) {
	provider := newPrinterApp(PrinterDeps{
		Enabled: func(printerapp.App) bool { return true },
		InstalledImage: func(app printerapp.App) (string, error) {
			return app.Family.Image + "@sha256:" + repeat('b', 64), nil
		},
		VerifiedImage: func(_ context.Context, family printerapp.Family) (string, error) {
			return family.Image + "@sha256:" + repeat('b', 64), nil
		},
	})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("Check() items = %#v, want none (already verified)", got.Items)
	}
}

// The verified refresh must never be a silent skip. If cosign cannot verify a
// family, that is a source failure, not "nothing to do".
func TestPrinterCheckSurfacesVerificationFailure(t *testing.T) {
	provider := newPrinterApp(PrinterDeps{
		Enabled: func(printerapp.App) bool { return true },
		InstalledImage: func(app printerapp.App) (string, error) {
			return app.Family.Image + "@sha256:" + repeat('c', 64), nil
		},
		VerifiedImage: func(context.Context, printerapp.Family) (string, error) {
			return "", printerapp.ErrSignatureUnverified
		},
	})

	_, err := provider.Check(context.Background())
	if !errors.Is(err, printerapp.ErrSignatureUnverified) {
		t.Fatalf("Check() error = %v, want ErrSignatureUnverified to surface", err)
	}
}

// An unreadable unit is a permission problem on one printer, not a driver
// update: skip it and keep checking the rest, rather than failing the source.
func TestPrinterCheckSkipsUnreadableUnit(t *testing.T) {
	readErr := fmt.Errorf("permission denied")
	provider := newPrinterApp(PrinterDeps{
		Enabled: func(app printerapp.App) bool { return true },
		InstalledImage: func(app printerapp.App) (string, error) {
			if app.Family.ID == "hplip" {
				return "", readErr
			}
			return app.Family.Image + "@sha256:" + repeat('d', 64), nil
		},
		VerifiedImage: func(_ context.Context, family printerapp.Family) (string, error) {
			return family.Image + "@sha256:" + repeat('e', 64), nil
		},
	})

	got, err := provider.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v, want no error after skipping unreadable unit", err)
	}
	// ghostscript and gutenprint are enabled and readable; hplip is skipped.
	if len(got.Items) != 2 {
		t.Fatalf("Check() items = %d, want 2 (hplip skipped)", len(got.Items))
	}
}

func TestPrinterApplyRefreshesEachFamilyOnce(t *testing.T) {
	var calls []string
	provider := newPrinterApp(PrinterDeps{
		Refresh: func(_ context.Context, app printerapp.App) error {
			calls = append(calls, app.Family.ID)
			return nil
		},
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{
		{ID: "hplip"},
		{ID: "gutenprint"},
	}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"hplip", "gutenprint"}) {
		t.Fatalf("Apply() refresh order = %v, want serial per family", calls)
	}
	if !result.Changed || result.Preview {
		t.Fatalf("Apply() result = %#v, want live changed result", result)
	}
}

func TestPrinterApplyRefreshFailurePropagates(t *testing.T) {
	wantErr := printerapp.ErrSignatureUnverified
	var calls []string
	provider := newPrinterApp(PrinterDeps{
		Refresh: func(_ context.Context, app printerapp.App) error {
			calls = append(calls, app.Family.ID)
			return wantErr
		},
	})

	_, err := provider.Apply(context.Background(), []updateflow.Item{{ID: "hplip"}, {ID: "gutenprint"}}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Apply() error = %v, want %v", err, wantErr)
	}
	if len(calls) != 1 {
		t.Fatalf("Apply() refresh calls = %v, want the failing family to stop the run", calls)
	}
}

func TestPrinterApplyDryRunReportsPreview(t *testing.T) {
	var calls []string
	provider := newPrinterApp(PrinterDeps{
		Refresh: func(_ context.Context, app printerapp.App) error {
			calls = append(calls, app.Family.ID)
			return nil
		},
		DryRun: func() bool { return true },
	})

	result, err := provider.Apply(context.Background(), []updateflow.Item{{ID: "hplip"}}, nil)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Changed {
		t.Fatal("dry-run Apply() reports a completed mutation")
	}
	if !result.Preview {
		t.Fatal("dry-run Apply() does not report preview")
	}
}

func TestPrinterAppReconstructsFamily(t *testing.T) {
	app, ok := printerApp(updateflow.Item{ID: "hplip"})
	if !ok {
		t.Fatalf("printerApp(hplip) not resolved")
	}
	if app.Family.ID != "hplip" || app.Name != "hplip" {
		t.Fatalf("printerApp(hplip) = %+v, want family+name hplip", app)
	}
	if _, ok := printerApp(updateflow.Item{ID: "nope"}); ok {
		t.Fatal("printerApp(nope) resolved to a family")
	}
}

// The individual-update path calls the real printerapp.Refresh, so it is
// tested by placing the printer quadlet where the real unit dir is read
// (XDG_CONFIG_HOME) rather than stubbing the unexported printer seams: the
// routing must reach Refresh and honour dry-run.
func TestUpdateItemPrintersUnderDryRun(t *testing.T) {
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	app := printerapp.Select(printerapp.Family{ID: "hplip", DisplayName: "HPLIP", Image: "ghcr.io/projectbluefin/hplip-printer-app:stable"})
	if err := os.MkdirAll(filepath.Join(tmp, "containers", "systemd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "containers", "systemd", app.UnitName()),
		[]byte("Image=ghcr.io/projectbluefin/hplip-printer-app:stable\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := UpdateItem(context.Background(), updateflow.Printers, updateflow.Item{ID: "hplip"})
	if err != nil {
		t.Fatalf("UpdateItem(Printers, dry-run) error = %v", err)
	}
	if !result.Preview || result.Changed {
		t.Fatalf("dry-run UpdateItem result = %#v, want a preview that changed nothing", result)
	}
}

// An enabled printer that is not actually installed fails the individual update
// through the same Refresh the batch path uses, so the row stays pending
// instead of reporting a completed refresh.
func TestUpdateItemPrintersNotEnabled(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	// Isolate the unit dir so this never accidentally sees a real printer
	// quadlet on the host.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	result, err := UpdateItem(context.Background(), updateflow.Printers, updateflow.Item{ID: "hplip"})
	if err == nil {
		t.Fatalf("UpdateItem(Printers, not installed) = %#v, want Refresh to refuse", result)
	}
	if !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("UpdateItem(Printers) error = %v, want it to name the missing installation", err)
	}
	if result.Changed || result.Preview {
		t.Fatalf("failed update reported %#v, want the zero result", result)
	}
}

func TestUpdateItemPrintersUnknownFamilyFails(t *testing.T) {
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })

	_, err := UpdateItem(context.Background(), updateflow.Printers, updateflow.Item{ID: "nope"})
	if err == nil {
		t.Fatal("UpdateItem(Printers, unknown family) = nil error, want failure")
	}
}

// repeat fills a stable-length string so the test digests read like real ones.
func repeat(r byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}
