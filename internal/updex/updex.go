// Package updex provides an interface to system feature management via the updex API.
// Read operations use the updex Go library directly. Write operations that require
// root are delegated to the chairlift-updex-helper binary via pkexec.
package updex

import (
	"context"
	"fmt"
	"sync"
	"time"

	updexapi "github.com/frostyard/updex/updex"
	"github.com/projectbluefin/chairlift/internal/helperexec"
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

	pkexecCommand  = "pkexec"
	DefaultTimeout = 5 * time.Minute
)

// DefaultContext returns a context with the default timeout
func DefaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), DefaultTimeout)
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
		apiClient = updexapi.NewClient(updexapi.ClientConfig{})
	})
	return apiClient
}

// IsInstalled checks if updex features are configured on this system
func IsInstalled() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := getClient().Features(ctx)
	return err == nil
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

// CheckFeatures checks enabled features for available updates
func CheckFeatures(ctx context.Context) ([]FeatureCheck, error) {
	checks, err := getClient().CheckFeatures(ctx, updexapi.CheckFeaturesOptions{})
	if err != nil {
		return nil, &Error{Message: fmt.Sprintf("failed to check features: %v", err)}
	}
	return checks, nil
}

// EnableFeature enables a feature for download
func EnableFeature(ctx context.Context, name string) error {
	_, _, err := runHelper(ctx, pkexecCommand, updexhelper.CommandEnableFeature, name)
	return err
}

// DisableFeature disables a feature
func DisableFeature(ctx context.Context, name string) error {
	_, _, err := runHelper(ctx, pkexecCommand, updexhelper.CommandDisableFeature, name)
	return err
}

// UpdateFeatures downloads enabled features
func UpdateFeatures(ctx context.Context) error {
	_, _, err := runHelper(ctx, pkexecCommand, updexhelper.CommandUpdate)
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
