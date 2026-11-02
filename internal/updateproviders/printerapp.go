package updateproviders

import (
	"context"
	"fmt"
	"log"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/printerapp"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// PrinterDeps holds the printer-app operations the printer update provider
// needs. It mirrors the FlatpakDeps / HomebrewDeps seam so the provider is
// unit-testable with injected verification, unit reads, and refresh.
type PrinterDeps struct {
	// Enabled reports whether a specific printer application's quadlet is
	// installed.
	Enabled func(printerapp.App) bool
	// InstalledImage reads the Image= reference a printer unit currently runs.
	InstalledImage func(printerapp.App) (string, error)
	// VerifiedImage resolves a family's verified :stable digest.
	VerifiedImage func(ctx context.Context, family printerapp.Family) (string, error)
	// Refresh re-verifies a unit and restarts it at its verified digest.
	Refresh func(ctx context.Context, app printerapp.App) error
	// DryRun reports whether updates are staged to run.
	DryRun func() bool
}

// printerProvider offers verified updates for the printer applications whose
// families the user has enabled. It is additive: it appears on the Updates page
// exactly where the other sources do, and verification failures surface as a
// source error rather than a silent skip.
type printerProvider struct {
	deps PrinterDeps
}

// NewPrinterApp constructs the production printer update provider.
func NewPrinterApp() updateflow.Provider {
	return newPrinterApp(productionPrinterDeps())
}

func productionPrinterDeps() PrinterDeps {
	return PrinterDeps{
		Enabled:        printerapp.IsEnabled,
		InstalledImage: printerapp.InstalledImage,
		VerifiedImage:  printerapp.VerifiedImage,
		Refresh:        printerapp.Refresh,
		DryRun:         dryrun.Enabled,
	}
}

func newPrinterApp(deps PrinterDeps) updateflow.Provider {
	return &printerProvider{deps: deps}
}

// ID is the printer source's identity on the Updates page.
func (p *printerProvider) ID() updateflow.SourceID {
	return updateflow.Printers
}

// Available mirrors the Printers group visibility: the quadlet is a Podman
// feature, so without Podman there is nothing to refresh.
func (p *printerProvider) Available() bool {
	return printerapp.IsAvailable()
}

// Check offers one item per enabled family whose verified :stable digest no
// longer matches the digest its unit runs. A verification failure is not
// skipped: it is returned so the coordinator marks the source failed and the
// person can retry, rather than the provider pretending the driver is current.
func (p *printerProvider) Check(ctx context.Context) (updateflow.CheckResult, error) {
	var items []updateflow.Item
	for _, family := range printerapp.Families() {
		app := printerapp.Select(family)
		if !p.deps.Enabled(app) {
			continue
		}
		installed, err := p.deps.InstalledImage(app)
		if err != nil {
			// A unit we just confirmed exists is only unreadable on a real
			// permission problem; skip it rather than fail the whole source.
			log.Printf("updateproviders: reading installed printer image for %s: %v", family.ID, err)
			continue
		}
		verified, err := p.deps.VerifiedImage(ctx, family)
		if err != nil {
			// Never silently skip an unverified family: surface it.
			return updateflow.CheckResult{}, fmt.Errorf("verify %s printer image: %w", family.DisplayName, err)
		}
		if installed == verified {
			continue
		}
		items = append(items, updateflow.Item{
			ID:               family.ID,
			Name:             family.DisplayName,
			CurrentVersion:   installed,
			AvailableVersion: verified,
		})
	}
	return updateflow.CheckResult{Items: items}, nil
}

// Apply refreshes each selected family once, serialised, so two printers never
// drive the same family's verified pull at once as Refresh's doc comment
// requires. A failed verification leaves the unit untouched and surfaces the
// error; dry-run stages nothing and is reported as a preview.
func (p *printerProvider) Apply(ctx context.Context, items []updateflow.Item, progress func(updateflow.Progress)) (updateflow.ApplyResult, error) {
	if progress != nil {
		progress(updateflow.Progress{Source: updateflow.Printers, Message: "Refreshing printers…"})
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return updateflow.ApplyResult{}, err
		}
		app, ok := printerApp(item)
		if !ok {
			return updateflow.ApplyResult{}, fmt.Errorf("printer update %q has no family", item.ID)
		}
		if err := p.deps.Refresh(ctx, app); err != nil {
			return updateflow.ApplyResult{}, err
		}
	}
	if p.isDryRun() {
		return updateflow.ApplyResult{Preview: true}, nil
	}
	return updateflow.ApplyResult{Changed: true}, nil
}

func (p *printerProvider) isDryRun() bool {
	return p.deps.DryRun != nil && p.deps.DryRun()
}

// printerApp reconstructs the printer application an update row refers to from
// the family ID stored in the item's identity, so an individual update and a
// batch refresh resolve to the same unit.
func printerApp(item updateflow.Item) (printerapp.App, bool) {
	family, ok := printerapp.FamilyByID(item.ID)
	if !ok {
		return printerapp.App{}, false
	}
	return printerapp.Select(family), true
}
