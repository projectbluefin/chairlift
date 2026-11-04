package hardware

import (
	"context"
	"fmt"
	"log"
	"path"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/homebrew"
)

// Status describes detected OEM hardware and its verified package inventory.
type Status struct {
	Vendor    Vendor
	Profile   Profile
	Installed bool
	Missing   []string
}

// CheckStatus reads Homebrew's installed inventory once. A leftover Caskroom
// directory is not evidence that a cask finished installing.
func CheckStatus(sysfsRoot string) (Status, error) {
	vendor := Detect(sysfsRoot)
	profile, ok := OEMProfile(vendor)
	status := Status{Vendor: vendor, Profile: profile}
	if !ok {
		return status, nil
	}
	casks, err := homebrew.ListInstalledCasks()
	if err != nil {
		return status, fmt.Errorf("checking OEM packages: %w", err)
	}
	for _, pkg := range profile.Packages {
		installed := false
		for _, cask := range casks {
			if path.Base(cask.Name) == path.Base(pkg) && cask.Version != "" {
				installed = true
				break
			}
		}
		if !installed {
			status.Missing = append(status.Missing, pkg)
		}
	}
	status.Installed = len(status.Missing) == 0
	return status, nil
}

// EnableHardware installs missing OEM casks, then verifies their installed
// versions. The existing Homebrew runner bounds each command; ctx also stops
// work between commands. Dry-run never claims skipped installations succeeded.
func EnableHardware(ctx context.Context, sysfsRoot string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	status, err := CheckStatus(sysfsRoot)
	if err != nil || status.Vendor == VendorNone || status.Installed {
		return status, err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would install hardware packages for %s: %v", status.Profile.DisplayName, status.Missing)
		return status, nil
	}
	if err := ctx.Err(); err != nil {
		return status, err
	}
	if err := homebrew.Tap("ublue-os/tap"); err != nil {
		return status, fmt.Errorf("adding OEM package tap: %w", err)
	}
	for _, pkg := range status.Missing {
		if err := ctx.Err(); err != nil {
			return status, err
		}
		if err := homebrew.Install(pkg, true); err != nil {
			return status, fmt.Errorf("installing OEM package %s: %w", pkg, err)
		}
	}
	status, err = CheckStatus(sysfsRoot)
	if err != nil {
		return status, err
	}
	if !status.Installed {
		return status, fmt.Errorf("OEM packages not installed after setup: %v", status.Missing)
	}
	return status, nil
}
