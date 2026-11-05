package pageview

import (
	"errors"

	"github.com/projectbluefin/chairlift/internal/devtools"
)

// Documentation URLs for WSL Mode virtual machine engines.
const (
	NSLDocumentationURL  = "https://frostyard.github.io/nsl/"
	LimaDocumentationURL = "https://lima-vm.io/docs/"
)

// WSLEngineDocumentationURL returns the documentation URL for the given WSL engine backend.
func WSLEngineDocumentationURL(backend string) string {
	if backend == "lima" {
		return LimaDocumentationURL
	}
	return NSLDocumentationURL
}

// WSLEngineDocumentationTooltip returns the tooltip and accessible label for the documentation button.
func WSLEngineDocumentationTooltip(backend string) string {
	if backend == "lima" {
		return "Lima documentation"
	}
	return "NSL documentation"
}

// WSLObservation retains the last confirmed state when the inventory read fails.
// It is display state only; mutations must read fresh inventory themselves.
func WSLObservation(previous, observed devtools.WSLState, err error) devtools.WSLState {
	if err != nil {
		return previous
	}
	return observed
}

// WSLAllowed offers Stop for a last-confirmed running machine after a failed
// read; the backend still requires fresh inventory before issuing the stop.
func WSLAllowed(active, canEnable bool, err error) bool {
	return active || (err == nil && canEnable)
}

// WSLDetails keeps setup, inventory and shell probe failures visible together.
func WSLDetails(actionErr, stateErr error, probeDiagnostic string) string {
	if errors.Is(actionErr, stateErr) {
		stateErr = nil // The setup result already carries this observation failure.
	}
	var probeErr error
	if probeDiagnostic != "" {
		probeErr = errors.New(probeDiagnostic)
	}
	if err := errors.Join(actionErr, stateErr, probeErr); err != nil {
		return err.Error()
	}
	return ""
}

// WSLSubtitle describes the observed machine without claiming a failed read succeeded.
func WSLSubtitle(backend string, state devtools.WSLState, err error) string {
	if backend == devtools.BackendLima {
		switch {
		case err != nil:
			return "Couldn't check the Ubuntu virtual machine."
		case state.Ready:
			return "Ubuntu is ready in Lima. Run limactl shell ubuntu, or connect your editor over SSH to lima-ubuntu."
		case state.Running:
			return "Ubuntu is running but not ready yet."
		case state.Exists:
			return "Ubuntu is stopped. Your files are kept."
		default:
			return "Run Ubuntu LTS in a Lima virtual machine with writable access to your home folder. Downloads about 600 MB. Open a terminal with limactl shell ubuntu."
		}
	}
	switch {
	case err != nil:
		return "Couldn't check your Linux virtual machine."
	case state.Ready:
		return "Your Linux virtual machine is ready."
	case state.Running:
		return "Your Linux virtual machine is running but not ready yet."
	case state.Exists:
		return "Your Linux virtual machine is stopped. Your files are kept."
	default:
		return "Run Linux in a virtual machine that keeps your files."
	}
}
