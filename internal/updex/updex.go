// Package updex provides an interface to system feature management via the updex API.
// Read operations use the updex Go library directly. Write operations that require
// root are delegated to the chairlift-updex-helper binary via pkexec.
package updex

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/frostyard/std/reporter"
	updexapi "github.com/frostyard/updex/updex"
	"github.com/projectbluefin/chairlift/internal/helperexec"
	"github.com/projectbluefin/chairlift/internal/pkexec"
	"github.com/projectbluefin/chairlift/internal/updexhelper"
)

const (
	// HelperPath is the fixed, absolute installed path of the privileged
	// updex helper binary. It must match the
	// org.freedesktop.policykit.exec.path annotation on all three actions in
	// data/io.projectbluefin.chairlift.updex.policy exactly. Each action also
	// selects one supported helper command through the exec.argv1 annotation.
	// A path mismatch (e.g. a bare, $PATH-resolved name) makes pkexec fall
	// back to the generic, more restrictive
	// org.freedesktop.policykit.pkexec.run-program action instead. This
	// constant is installed at $(PREFIX)/bin/chairlift-updex-helper by the
	// Makefile, which requires PREFIX=/usr (the default) to match.
	HelperPath = "/usr/bin/chairlift-updex-helper"

	// DefaultTimeout bounds an unprivileged read (listing or checking
	// features).
	DefaultTimeout = 5 * time.Minute

	// AuthenticationAllowance is how much longer the GUI waits than the
	// helper's own budget (updexhelper.Timeout). The GUI's clock also runs
	// while the PolicyKit prompt is open, and the helper's starts only after
	// authentication; internal/ublue carries the same allowance for the
	// same reason.
	AuthenticationAllowance = 5 * time.Minute
)

// DefaultContext returns a context for an unprivileged read.
func DefaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), DefaultTimeout)
}

// MutationContext returns a context for one privileged helper command
// (updexhelper.CommandEnableFeature, CommandDisableFeature, or CommandUpdate):
// the helper's own budget for that command plus AuthenticationAllowance.
func MutationContext(command string) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), MutationTimeout(command))
}

// MutationTimeout is MutationContext's duration.
func MutationTimeout(command string) time.Duration {
	return updexhelper.Timeout(command) + AuthenticationAllowance
}

// Error represents an updex-related error. It aliases
// internal/helperexec.Error: all privileged helper invocations share one
// plumbing implementation and one failure taxonomy.
type Error = helperexec.Error

// NotFoundError is returned when updex features are not configured
type NotFoundError = helperexec.NotFoundError

// Type aliases to the updex API types
type (
	Feature      = updexapi.FeatureInfo
	CheckResult  = updexapi.CheckResult
	FeatureCheck = updexapi.CheckFeaturesResult
)

// Singleton API client
var (
	clientOnce sync.Once
	apiClient  *updexapi.Client
)

func getClient() *updexapi.Client {
	clientOnce.Do(func() {
		apiClient = newClient(updexapi.ClientConfig{})
	})
	return apiClient
}

// newClient is the single construction site for updex API clients. The
// singleton above serves every wrapper that needs no per-call state; callers
// that must supply one (featuresChecker's progress reporter) build their own
// client through this function so the two sites cannot drift.
func newClient(cfg updexapi.ClientConfig) *updexapi.Client {
	return updexapi.NewClient(cfg)
}

// featuresLister is an unexported injection seam for feature listing, allowing
// availability check behavior (empty, populated, or error) to be tested without
// relying on host system definitions.
var featuresLister = func(ctx context.Context) ([]Feature, error) {
	return getClient().Features(ctx)
}

// IsInstalled checks if updex features are configured on this system.
// It returns true only if updex features can be listed without error and at least
// one feature definition is present. An empty feature list is treated as unavailable.
func IsInstalled() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	features, err := featuresLister(ctx)
	return err == nil && len(features) > 0
}

var (
	installedOnce   sync.Once
	installedResult bool
)

// IsInstalledCached returns a cached result of IsInstalled, running the check at most once.
func IsInstalledCached() bool {
	installedOnce.Do(func() {
		installedResult = IsInstalled()
	})
	return installedResult
}

// ListFeatures returns all available features
func ListFeatures(ctx context.Context) ([]Feature, error) {
	features, err := getClient().Features(ctx)
	if err != nil {
		return nil, &Error{Message: fmt.Sprintf("failed to list features: %v", err)}
	}
	return features, nil
}

// warningCapturingReporter captures warnings emitted by updex during operations.
type warningCapturingReporter struct {
	reporter.NoopReporter
	mu       sync.Mutex
	warnings []string
}

func (r *warningCapturingReporter) Warning(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

func (r *warningCapturingReporter) Warnings() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.warnings)
}

// featuresChecker is an unexported injection seam for feature update checking,
// allowing check behavior (checks, warnings, error) to be tested without
// relying on host system definitions or network access. It cannot use the
// getClient singleton: the warning-capturing reporter is per-call state, so the
// client carrying it must be too. It still constructs through newClient.
var featuresChecker = func(ctx context.Context) ([]FeatureCheck, []string, error) {
	rep := &warningCapturingReporter{}
	client := newClient(updexapi.ClientConfig{Progress: rep})
	checks, err := client.CheckFeatures(ctx, updexapi.CheckFeaturesOptions{})
	if err != nil {
		return nil, rep.Warnings(), &Error{Message: fmt.Sprintf("failed to check features: %v", err)}
	}
	return checks, rep.Warnings(), nil
}

// CheckFeatures checks enabled features for available updates, returning the
// per-feature results, any warnings emitted during the check (such as per-component
// manifest or version failures), and any top-level error.
func CheckFeatures(ctx context.Context) ([]FeatureCheck, []string, error) {
	return featuresChecker(ctx)
}

// EnableFeature enables a feature for download
func EnableFeature(ctx context.Context, name string) error {
	_, _, err := runHelper(ctx, pkexec.Command, updexhelper.CommandEnableFeature, name)
	return err
}

// DisableFeature disables a feature
func DisableFeature(ctx context.Context, name string) error {
	_, _, err := runHelper(ctx, pkexec.Command, updexhelper.CommandDisableFeature, name)
	return err
}

// UpdateFeatures downloads enabled features
func UpdateFeatures(ctx context.Context) error {
	_, _, err := runHelper(ctx, pkexec.Command, updexhelper.CommandUpdate)
	return err
}

// runHelper executes HelperPath via pkexec for privileged operations. The
// plumbing — dry-run short-circuit, journaling, failure classification — is
// owned by internal/helperexec; this wrapper only binds the package's fixed
// HelperPath. pkexecPath stays an explicit parameter (mirroring
// internal/stageexec.Run's executable seam) so tests can substitute a fake
// pkexec stand-in without invoking the real pkexec/polkit stack or
// requiring root. HelperPath itself is never overridden: it is the fixed
// absolute path that must match the policy's exec.path annotation, so tests
// assert it by inspecting the fake pkexec's captured argv rather than by
// substituting a different one in. Every invocation — dry-run or live — is
// journal.Record'd, so a test can assert the argv ChairLift assembled
// without granting privilege; see internal/journal.
func runHelper(ctx context.Context, pkexecPath string, args ...string) (string, string, error) {
	return helperexec.Run(ctx, pkexecPath, HelperPath, args...)
}
