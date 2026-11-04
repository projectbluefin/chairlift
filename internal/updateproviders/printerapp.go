package updateproviders

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/printerapp"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

// PrinterAppDeps contains the printer application operations used by the
// update provider.
type PrinterAppDeps struct {
	// Available reports whether the host can run printer applications at
	// all; it is the same Podman probe that shows the Printers group.
	Available func() bool
	// Apps lists the printer applications ChairLift manages.
	Apps func() []printerapp.App
	// Installed reads the image reference an installed unit runs.
	Installed func(printerapp.App) (string, error)
	// Verified verifies the family's signed reference and returns the
	// digest-pinned image Refresh would install, writing nothing.
	Verified func(context.Context, printerapp.App) (string, error)
	// Refresh re-verifies and restarts an installed app on the verified digest.
	Refresh func(context.Context, printerapp.App) error
	DryRun  func() bool
}

type printerAppProvider struct {
	deps PrinterAppDeps
}

// NewPrinterApps constructs the production printer application update
// provider.
func NewPrinterApps() updateflow.Provider {
	return newPrinterApps(PrinterAppDeps{
		Available: printerapp.IsAvailable,
		Apps: func() []printerapp.App {
			families := printerapp.Families()
			apps := make([]printerapp.App, 0, len(families))
			for _, family := range families {
				apps = append(apps, printerapp.Select(family))
			}
			return apps
		},
		Installed: printerapp.InstalledImage,
		Verified:  printerapp.VerifiedImage,
		Refresh:   printerapp.Refresh,
		DryRun:    dryrun.Enabled,
	})
}

func newPrinterApps(deps PrinterAppDeps) updateflow.Provider {
	return &printerAppProvider{deps: deps}
}

func (p *printerAppProvider) ID() updateflow.SourceID {
	return updateflow.PrinterDrivers
}

func (p *printerAppProvider) Available() bool {
	return p.deps.Available != nil && p.deps.Available()
}

// Check offers every installed app whose verified :stable digest differs
// from the image its unit runs. A family that cannot be checked — its
// signature does not verify, or its unit cannot be read — never hides the
// others: Check returns available items and names every unchecked family in
// Warnings. It logs underlying errors; only cancellation is returned as an error.
func (p *printerAppProvider) Check(ctx context.Context) (updateflow.CheckResult, error) {
	var items []updateflow.Item
	var warnings []string
	var errs []error
	for _, app := range p.deps.Apps() {
		if err := ctx.Err(); err != nil {
			return updateflow.CheckResult{}, err
		}
		installed, err := p.deps.Installed(app)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			warnings = append(warnings, printerAppName(app))
			errs = append(errs, fmt.Errorf("%s: read installed driver: %w", printerAppName(app), err))
			continue
		}
		verified, err := p.deps.Verified(ctx, app)
		if err != nil {
			warnings = append(warnings, printerAppName(app))
			errs = append(errs, fmt.Errorf("%s: %w", printerAppName(app), err))
			continue
		}
		if verified == installed {
			continue
		}
		items = append(items, updateflow.Item{
			ID:               app.UnitName(),
			Name:             printerAppName(app),
			CurrentVersion:   imageVersion(installed),
			AvailableVersion: imageVersion(verified),
		})
	}
	if len(errs) > 0 {
		for _, failure := range errs {
			log.Printf("updates: checking %s: %v", updateflow.PrinterDrivers, failure)
		}
	}
	return updateflow.CheckResult{Items: items, Warnings: warnings}, nil
}

// Apply refreshes each selected app in turn. One app's failure — including a
// signature that no longer verifies, which leaves its unit untouched — is
// reported against that app and does not stop the others.
func (p *printerAppProvider) Apply(ctx context.Context, items []updateflow.Item, progress func(updateflow.Progress)) (updateflow.ApplyResult, error) {
	apps := make(map[string]printerapp.App)
	for _, app := range p.deps.Apps() {
		apps[app.UnitName()] = app
	}

	var errs []error
	var pending []updateflow.Item
	var warnings []string
	ran := 0
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		if seen[item.ID] {
			continue
		}
		seen[item.ID] = true
		app, ok := apps[item.ID]
		if !ok {
			errs = append(errs, fmt.Errorf("printer driver %q is not managed by %s", item.ID, branding.AppName))
			pending = append(pending, item)
			name := item.Name
			if name == "" {
				name = item.ID
			}
			warnings = append(warnings, name)
			continue
		}
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			pending = append(pending, item)
			warnings = append(warnings, printerAppName(app))
			for _, rest := range items[i+1:] {
				if seen[rest.ID] {
					continue
				}
				seen[rest.ID] = true
				pending = append(pending, rest)
				if next, ok := apps[rest.ID]; ok {
					warnings = append(warnings, printerAppName(next))
				} else if rest.Name != "" {
					warnings = append(warnings, rest.Name)
				} else {
					warnings = append(warnings, rest.ID)
				}
			}
			return updateflow.ApplyResult{PendingItems: pending, Warnings: warnings}, errors.Join(errs...)
		}
		if progress != nil {
			progress(updateflow.Progress{
				Source:   updateflow.PrinterDrivers,
				Message:  "Updating " + printerAppName(app) + " printer driver…",
				Fraction: float64(i) / float64(len(items)),
			})
		}
		if err := p.deps.Refresh(ctx, app); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", printerAppName(app), err))
			pending = append(pending, item)
			warnings = append(warnings, printerAppName(app))
			continue
		}
		ran++
	}
	if err := errors.Join(errs...); err != nil {
		return updateflow.ApplyResult{PendingItems: pending, Warnings: warnings}, err
	}
	if ran > 0 && p.deps.DryRun != nil && p.deps.DryRun() {
		return updateflow.ApplyResult{Preview: true}, nil
	}
	return updateflow.ApplyResult{Changed: ran > 0}, nil
}

func printerAppName(app printerapp.App) string {
	if app.Name == app.Family.ID {
		return app.Family.DisplayName
	}
	return app.Family.DisplayName + " (" + app.Name + ")"
}

// imageVersion shortens an image reference to what a row can show: the first
// twelve hex digits of a pinned digest, or the tag of an unpinned reference.
func imageVersion(image string) string {
	if _, digest, ok := strings.Cut(image, "@sha256:"); ok {
		if len(digest) > 12 {
			digest = digest[:12]
		}
		return digest
	}
	if colon := strings.LastIndex(image, ":"); colon > strings.LastIndex(image, "/") {
		return image[colon+1:]
	}
	return image
}
