// Package bundleview owns the pure presentation and action-state decisions
// used by the Applications page's Brew bundle group.
package bundleview

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/projectbluefin/chairlift/internal/homebrew"
)

const (
	gateIdle uint32 = iota
	gateRunning
	gateComplete
)

// Presentation describes the group-level text after bundle discovery.
// Placeholder fields are non-empty only when there are no bundle rows.
type Presentation struct {
	Description         string
	PlaceholderTitle    string
	PlaceholderSubtitle string
}

// Present derives the bundle group's complete loaded state. warning is empty
// when every existing configured path was read successfully.
func Present(count int, warning string, homebrewAvailable bool) Presentation {
	var result Presentation
	switch {
	case count == 0 && warning == "":
		result = Presentation{
			Description:         "No Brew bundles found",
			PlaceholderTitle:    "No bundles available",
			PlaceholderSubtitle: "Check the configured bundles_paths directories",
		}
	case count == 0:
		result = Presentation{
			Description:         "Brew bundles could not be loaded",
			PlaceholderTitle:    "Bundles unavailable",
			PlaceholderSubtitle: warning,
		}
	default:
		noun := "bundles"
		if count == 1 {
			noun = "bundle"
		}
		result.Description = fmt.Sprintf("%d Brew %s available", count, noun)
		if warning != "" {
			result.Description += "; some configured paths could not be read: " + warning
		}
	}

	if !homebrewAvailable {
		result.Description = strings.TrimSuffix(result.Description, ".") +
			". Homebrew is not installed; install actions are disabled."
	}
	return result
}

// InstallGate prevents overlapping invocations of one bundle row's install
// action. Its zero value is ready for use.
type InstallGate struct {
	state atomic.Uint32
}

// TryStart starts an idle action and reports whether this caller acquired it.
func (g *InstallGate) TryStart() bool {
	return g.state.CompareAndSwap(gateIdle, gateRunning)
}

// Reset makes a failed or dry-run action available again.
func (g *InstallGate) Reset() {
	g.state.CompareAndSwap(gateRunning, gateIdle)
}

// Complete permanently closes a bundle action, whether it was started by this
// process or the bundle was already installed when its row was built.
func (g *InstallGate) Complete() {
	g.state.Store(gateComplete)
}

// IsIdle reports whether no action has been started and none has completed,
// meaning the row's action control is safe to re-derive from a fresh status.
func (g *InstallGate) IsIdle() bool {
	return g.state.Load() == gateIdle
}

// RowState represents the UI presentation and state of a bundle row's action control.
type RowState struct {
	Label     string
	Sensitive bool
	Completed bool
}

// RowAction derives button label, sensitivity, and completion state from bundle status
// and Homebrew availability.
func RowAction(status homebrew.BundleStatus, homebrewAvailable bool) RowState {
	if !homebrewAvailable {
		return RowState{
			Label:     "Install",
			Sensitive: false,
			Completed: false,
		}
	}

	switch status {
	case homebrew.BundleInstalled:
		return RowState{
			Label:     "Installed",
			Sensitive: false,
			Completed: true,
		}
	case homebrew.BundleUpdateAvailable:
		return RowState{
			Label:     "Update",
			Sensitive: true,
			Completed: false,
		}
	case homebrew.BundleNotInstalled:
		return RowState{
			Label:     "Install",
			Sensitive: true,
			Completed: false,
		}
	default:
		return RowState{
			Label:     "Install",
			Sensitive: true,
			Completed: false,
		}
	}
}
