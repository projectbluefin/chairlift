//go:build chairlift_e2e

package app

import (
	"context"
	"os"
	"strings"

	"github.com/projectbluefin/chairlift/internal/autoupdate"
	"github.com/projectbluefin/chairlift/internal/gpu"
	"github.com/projectbluefin/chairlift/internal/ublue"
)

// applyImageInfoOverride points the unprivileged image-descriptor read at
// $CHAIRLIFT_IMAGE_INFO. It is compiled only into the chairlift_e2e build
// that `make e2e` produces, never into a released binary — see the
// no-op counterpart in imageinfo_override.go for why.
//
// It is called only from the --dry-run branch, so the walkthrough's session
// additionally cannot execute any mutation. The privileged helper is a
// separate binary built without this tag and resolves its own descriptor
// from imageinfo.DescriptorPath regardless.
func applyImageInfoOverride() {
	if path := os.Getenv("CHAIRLIFT_IMAGE_INFO"); path != "" {
		ublue.SetDescriptorOverride(path)
	}

	// $CHAIRLIFT_GPU_VENDORS is a comma-separated list of PCI vendor IDs, so
	// the walkthrough can capture the graphics-driver row for hardware the
	// capture host does not have.
	if spec := os.Getenv("CHAIRLIFT_GPU_VENDORS"); spec != "" {
		_ = gpu.SetVendorIDs(strings.Split(spec, ","))
	}

	// $CHAIRLIFT_AUTO_UPDATES is "<is-enabled>,<is-active>" — the two
	// systemctl answers autoupdate.Classify consumes — so the walkthrough can
	// capture the automatic-updates switch on a runner with no uupd.timer.
	if spec := os.Getenv("CHAIRLIFT_AUTO_UPDATES"); spec != "" {
		isEnabled, isActive, _ := strings.Cut(spec, ",")
		autoupdate.SetProbe(func(context.Context) (string, string) {
			return isEnabled, isActive
		})
	}
}
