// Package updex provides an interface to system feature management via the updex API.
// Read operations use the updex Go library directly. Write operations that require
// root are delegated to the chairlift-updex-helper binary via pkexec.
package updex

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	updexapi "github.com/frostyard/updex/updex"
	"github.com/projectbluefin/chairlift/internal/journal"
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

var dryRun = false

// SetDryRun enables/disables dry-run mode
func SetDryRun(mode bool) {
	dryRun = mode
	log.Printf("updex dry-run mode: %v", mode)
}

// IsDryRun returns whether dry-run mode is enabled
func IsDryRun() bool {
	return dryRun
}

// DefaultContext returns a context with the default timeout
func DefaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), DefaultTimeout)
}

// Error represents an updex-related error
type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

// NotFoundError is returned when updex features are not configured
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

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

// journalArgs turns a privileged helper's argv (minus the leading command
// word, which becomes journal.Entry.Action) into the journal's args map.
func journalArgs(args []string) map[string]string {
	if len(args) <= 1 {
		return nil
	}
	return map[string]string{"args": strings.Join(args[1:], " ")}
}

// runHelper executes HelperPath via pkexec for privileged operations. pkexecPath
// is the pkexec binary to invoke — always pkexecCommand in production, but an
// explicit parameter (mirroring internal/stageexec.Run's executable seam) so
// tests can substitute a fake pkexec stand-in without invoking the real
// pkexec/polkit stack or requiring root. HelperPath itself is never overridden: it is the fixed
// absolute path that must match the policy's exec.path annotation, so tests
// assert it by inspecting the fake pkexec's captured argv rather than by
// substituting a different one in. Every invocation — dry-run or live — is
// also journal.Record'd, so a test can assert the argv ChairLift assembled
// without granting privilege; see internal/journal.
func runHelper(ctx context.Context, pkexecPath string, args ...string) (string, string, error) {
	action := ""
	if len(args) > 0 {
		action = args[0]
	}

	if dryRun {
		args = append(args, "--dry-run")
		wouldRun := append([]string{pkexecPath, HelperPath}, args...)
		journal.Record(action, journalArgs(args), wouldRun, journal.SuppressedDryRun)
		log.Printf("[DRY-RUN] would execute: %s %s %v", pkexecPath, HelperPath, args)
		return "", "", nil
	}

	fullArgs := append([]string{HelperPath}, args...)
	journal.Record(action, journalArgs(args), append([]string{pkexecPath}, fullArgs...), journal.SuppressedNone)
	cmd := exec.CommandContext(ctx, pkexecPath, fullArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if stderr.Len() > 0 {
		log.Printf("updex helper stderr: %s", stderr.String())
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", stderr.String(), &Error{Message: "command timed out"}
		}
		if execErr, ok := err.(*exec.Error); ok && execErr.Err == exec.ErrNotFound {
			return "", stderr.String(), &NotFoundError{Message: "pkexec or chairlift-updex-helper not found"}
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", stderr.String(), &Error{Message: fmt.Sprintf("command failed (exit %d): %s", exitErr.ExitCode(), stderr.String())}
		}
		return "", stderr.String(), &Error{Message: err.Error()}
	}

	return stdout.String(), stderr.String(), nil
}
