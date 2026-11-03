package updateproviders

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/projectbluefin/chairlift/internal/printerapp"
	"github.com/projectbluefin/chairlift/internal/updateflow"
	"github.com/projectbluefin/chairlift/internal/userprefs"
)

var (
	printerDigestA = "sha256:" + strings.Repeat("a", 64)
	printerDigestB = "sha256:" + strings.Repeat("b", 64)
)

func printerFamilies() []printerapp.App {
	return []printerapp.App{
		printerapp.Select(printerapp.Family{ID: "ghostscript", DisplayName: "Ghostscript", Image: "ghcr.io/projectbluefin/ghostscript-printer-app:stable"}),
		printerapp.Select(printerapp.Family{ID: "hplip", DisplayName: "HPLIP", Image: "ghcr.io/projectbluefin/hplip-printer-app:stable"}),
		printerapp.Select(printerapp.Family{ID: "gutenprint", DisplayName: "Gutenprint", Image: "ghcr.io/projectbluefin/gutenprint-printer-app:stable"}),
	}
}

func pinned(app printerapp.App, digest string) string {
	return strings.TrimSuffix(app.Family.Image, ":stable") + "@" + digest
}

// fakePrinterApps holds the installed image per enabled app and the verified
// image per family, and records each Refresh.
type fakePrinterApps struct {
	mu        sync.Mutex
	installed map[string]string
	verified  map[string]string
	verifyErr map[string]error
	refreshEr map[string]error
	refreshed []string
	dryRun    bool
}

func (f *fakePrinterApps) deps() PrinterAppDeps {
	return PrinterAppDeps{
		Available: func() bool { return true },
		Apps:      printerFamilies,
		Enabled: func(app printerapp.App) bool {
			_, ok := f.installed[app.UnitName()]
			return ok
		},
		Installed: func(app printerapp.App) (string, error) {
			return f.installed[app.UnitName()], nil
		},
		Verified: func(_ context.Context, app printerapp.App) (string, error) {
			if err := f.verifyErr[app.Family.ID]; err != nil {
				return "", err
			}
			return f.verified[app.Family.ID], nil
		},
		Refresh: func(_ context.Context, app printerapp.App) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.refreshed = append(f.refreshed, app.UnitName())
			return f.refreshEr[app.Family.ID]
		},
		DryRun: func() bool { return f.dryRun },
	}
}

func TestPrinterAppsAvailabilityFollowsPodman(t *testing.T) {
	for _, podman := range []bool{true, false} {
		provider := newPrinterApps(PrinterAppDeps{Available: func() bool { return podman }})
		if provider.ID() != updateflow.PrinterDrivers {
			t.Fatalf("provider ID = %q, want %q", provider.ID(), updateflow.PrinterDrivers)
		}
		if provider.Available() != podman {
			t.Fatalf("Available() = %v with podman present %v", provider.Available(), podman)
		}
	}
	if newPrinterApps(PrinterAppDeps{}).Available() {
		t.Fatal("provider without a podman probe is available")
	}
}

func TestPrinterAppsCheckOffersOnlyEnabledAppsWithANewVerifiedDigest(t *testing.T) {
	apps := printerFamilies()
	ghostscript, hplip := apps[0], apps[1]
	fake := &fakePrinterApps{
		installed: map[string]string{
			ghostscript.UnitName(): pinned(ghostscript, printerDigestA),
			hplip.UnitName():       pinned(hplip, printerDigestB),
		},
		verified: map[string]string{
			"ghostscript": pinned(ghostscript, printerDigestB),
			"hplip":       pinned(hplip, printerDigestB),
			"gutenprint":  pinned(apps[2], printerDigestB),
		},
	}

	got, err := newPrinterApps(fake.deps()).Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	want := updateflow.CheckResult{Items: []updateflow.Item{{
		ID:               ghostscript.UnitName(),
		Name:             "Ghostscript",
		CurrentVersion:   strings.Repeat("a", 12),
		AvailableVersion: strings.Repeat("b", 12),
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Check() = %#v, want %#v", got, want)
	}
}

// A unit written before verification runs the moving tag; the verified pin
// differs from it, so it is offered and Refresh replaces the tag.
func TestPrinterAppsCheckOffersALegacyTaggedUnit(t *testing.T) {
	app := printerFamilies()[0]
	fake := &fakePrinterApps{
		installed: map[string]string{app.UnitName(): app.Family.Image},
		verified:  map[string]string{"ghostscript": pinned(app, printerDigestA)},
	}
	got, err := newPrinterApps(fake.deps()).Check(context.Background())
	if err != nil || len(got.Items) != 1 || got.Items[0].CurrentVersion != "stable" {
		t.Fatalf("Check() = %#v, %v; want the tagged unit offered", got, err)
	}
}

// One unverifiable family must not hide another family's update: the
// provider returns a warning for it while preserving the verified item.
func TestPrinterAppsCheckOffersVerifiedUpdatesDespiteAnUnverifiableFamily(t *testing.T) {
	apps := printerFamilies()
	ghostscript, hplip := apps[0], apps[1]
	fake := &fakePrinterApps{
		installed: map[string]string{
			ghostscript.UnitName(): pinned(ghostscript, printerDigestA),
			hplip.UnitName():       pinned(hplip, printerDigestA),
		},
		verified:  map[string]string{"hplip": pinned(hplip, printerDigestB)},
		verifyErr: map[string]error{"ghostscript": printerapp.ErrSignatureUnverified},
	}

	got, err := newPrinterApps(fake.deps()).Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v, want the verified HPLIP update offered without a source failure", err)
	}
	if len(got.Items) != 1 || got.Items[0].ID != hplip.UnitName() {
		t.Fatalf("Check() items = %#v, want the verified HPLIP update still offered", got.Items)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "Ghostscript" {
		t.Fatalf("Check() warnings = %#v, want the unchecked Ghostscript family named", got.Warnings)
	}
}

// With nothing to offer, an unverifiable family still produces a named
// warning so the row cannot read as up to date.
func TestPrinterAppsCheckReportsAnUnverifiableFamilyByName(t *testing.T) {
	apps := printerFamilies()
	ghostscript, hplip := apps[0], apps[1]
	fake := &fakePrinterApps{
		installed: map[string]string{
			ghostscript.UnitName(): pinned(ghostscript, printerDigestA),
			hplip.UnitName():       pinned(hplip, printerDigestA),
		},
		verified:  map[string]string{"hplip": pinned(hplip, printerDigestA)},
		verifyErr: map[string]error{"ghostscript": printerapp.ErrSignatureUnverified},
	}

	got, err := newPrinterApps(fake.deps()).Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v, want the failed family reported as a warning", err)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "Ghostscript" {
		t.Fatalf("Check() warnings = %#v, want Ghostscript", got.Warnings)
	}
	if len(got.Items) != 0 {
		t.Fatalf("Check() items = %#v, want none", got.Items)
	}
}

func TestPrinterAppsApplyRefreshesEachSelectedApp(t *testing.T) {
	apps := printerFamilies()
	fake := &fakePrinterApps{}
	var messages []string
	result, err := newPrinterApps(fake.deps()).Apply(context.Background(), []updateflow.Item{
		{ID: apps[0].UnitName()},
		{ID: apps[2].UnitName()},
		{ID: apps[0].UnitName()},
	}, func(p updateflow.Progress) {
		if p.Source != updateflow.PrinterDrivers {
			t.Errorf("progress source = %q", p.Source)
		}
		messages = append(messages, p.Message)
	})
	if err != nil || !result.Changed || result.Preview {
		t.Fatalf("Apply() = %#v, %v; want changed", result, err)
	}
	want := []string{apps[0].UnitName(), apps[2].UnitName()}
	if !reflect.DeepEqual(fake.refreshed, want) {
		t.Fatalf("refreshed = %v, want %v", fake.refreshed, want)
	}
	if len(messages) != 2 {
		t.Fatalf("progress messages = %v", messages)
	}
}

func TestPrinterAppsApplyReportsAVerificationFailurePerItem(t *testing.T) {
	apps := printerFamilies()
	fake := &fakePrinterApps{refreshEr: map[string]error{"ghostscript": printerapp.ErrSignatureUnverified}}
	result, err := newPrinterApps(fake.deps()).Apply(context.Background(), []updateflow.Item{
		{ID: apps[0].UnitName()},
		{ID: apps[1].UnitName()},
	}, nil)
	if !errors.Is(err, printerapp.ErrSignatureUnverified) || !strings.Contains(err.Error(), "Ghostscript") {
		t.Fatalf("Apply() error = %v, want the Ghostscript verification failure", err)
	}
	if result.Changed {
		t.Fatal("Apply() claimed a change despite a failed refresh")
	}
	if len(result.PendingItems) != 1 || result.PendingItems[0].ID != apps[0].UnitName() {
		t.Fatalf("Apply() pending items = %#v, want only Ghostscript", result.PendingItems)
	}
	if len(result.Warnings) != 1 || result.Warnings[0] != "Ghostscript" {
		t.Fatalf("Apply() warnings = %#v, want Ghostscript", result.Warnings)
	}
	if want := []string{apps[0].UnitName(), apps[1].UnitName()}; !reflect.DeepEqual(fake.refreshed, want) {
		t.Fatalf("refreshed = %v, want %v: one failure must not stop the others", fake.refreshed, want)
	}
}

func TestPrinterAppsApplyRejectsAnUnknownItem(t *testing.T) {
	fake := &fakePrinterApps{}
	if _, err := newPrinterApps(fake.deps()).Apply(context.Background(), []updateflow.Item{{ID: "chairlift-unknown.container"}}, nil); err == nil {
		t.Fatal("Apply() accepted an item ChairLift does not manage")
	}
	if len(fake.refreshed) != 0 {
		t.Fatalf("refreshed = %v", fake.refreshed)
	}
}

func TestPrinterAppsApplyIsAPreviewInDryRun(t *testing.T) {
	fake := &fakePrinterApps{dryRun: true}
	result, err := newPrinterApps(fake.deps()).Apply(context.Background(), []updateflow.Item{{ID: printerFamilies()[0].UnitName()}}, nil)
	if err != nil || !result.Preview || result.Changed {
		t.Fatalf("Apply() = %#v, %v; want a preview", result, err)
	}
}

func TestPrinterAppImageVersion(t *testing.T) {
	for image, want := range map[string]string{
		"ghcr.io/projectbluefin/hplip-printer-app@" + printerDigestA: strings.Repeat("a", 12),
		"ghcr.io/projectbluefin/hplip-printer-app:stable":            "stable",
		"localhost:5000/hplip-printer-app":                           "localhost:5000/hplip-printer-app",
	} {
		if got := imageVersion(image); got != want {
			t.Errorf("imageVersion(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestPrinterAppsRetryOnlyRefreshesFailedFamilies(t *testing.T) {
	apps := printerFamilies()
	ghostscript, hplip := apps[0], apps[1]
	fake := &fakePrinterApps{
		installed: map[string]string{
			ghostscript.UnitName(): pinned(ghostscript, printerDigestA),
			hplip.UnitName():       pinned(hplip, printerDigestA),
		},
		verified: map[string]string{
			"ghostscript": pinned(ghostscript, printerDigestB),
			"hplip":       pinned(hplip, printerDigestB),
		},
		refreshEr: map[string]error{"ghostscript": printerapp.ErrSignatureUnverified},
	}
	provider := newPrinterApps(fake.deps())
	coordinator := updateflow.New([]updateflow.Provider{provider}, nil)
	preferences := userprefs.Values{PrinterDrivers: true}
	policy := map[updateflow.SourceID]updateflow.Policy{
		updateflow.PrinterDrivers: {Configured: true, Supported: true},
	}
	checked := coordinator.Check(context.Background(), preferences, policy, nil)
	if len(checked.Sources) != 1 || len(checked.Sources[0].Items) != 2 {
		t.Fatalf("checked snapshot = %#v, want both printer updates", checked)
	}

	failed := coordinator.UpdateAll(context.Background(), checked, preferences, nil)
	source := failed.Sources[0]
	if len(source.Items) != 1 || source.Items[0].ID != ghostscript.UnitName() {
		t.Fatalf("failed source items = %#v, want only Ghostscript pending", source.Items)
	}
	if len(source.ApplyWarnings) != 1 || source.ApplyWarnings[0] != "Ghostscript" {
		t.Fatalf("failed source warnings = %#v, want Ghostscript", source.ApplyWarnings)
	}

	fake.refreshEr = nil
	retried := coordinator.UpdateAll(context.Background(), failed, preferences, nil)
	want := []string{ghostscript.UnitName(), hplip.UnitName(), ghostscript.UnitName()}
	if !reflect.DeepEqual(fake.refreshed, want) {
		t.Fatalf("Refresh calls = %v, want first attempt of both and retry of Ghostscript only", fake.refreshed)
	}
	if retried.TotalUpdates != 0 || retried.Sources[0].ApplyErr != nil {
		t.Fatalf("retry snapshot = %#v, want no pending updates or apply error", retried)
	}
}
