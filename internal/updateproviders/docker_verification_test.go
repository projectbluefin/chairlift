package updateproviders

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dockerbrew"
)

func TestDockerUpdateRejectsInconclusiveVerification(t *testing.T) {
	readErr := errors.New("verification read failed")
	for _, tt := range []struct {
		name   string
		change func(*HomebrewDeps)
		cause  error
	}{
		{"API failure", func(d *HomebrewDeps) {
			d.DockerLatest = func(context.Context) ([]dockerbrew.Bottle, error) { return nil, readErr }
		}, readErr},
		{"manifest unreadable", func(d *HomebrewDeps) {
			d.DockerManifest = func() (dockerbrew.Manifest, error) { return dockerbrew.Manifest{}, readErr }
		}, readErr},
		{"manifest absent", func(d *HomebrewDeps) {
			d.DockerManifest = func() (dockerbrew.Manifest, error) { return dockerbrew.Manifest{}, os.ErrNotExist }
		}, os.ErrNotExist},
		{"empty API answer", func(d *HomebrewDeps) {
			d.DockerLatest = func(context.Context) ([]dockerbrew.Bottle, error) { return nil, nil }
		}, nil},
		{"partial API answer", func(d *HomebrewDeps) {
			d.DockerLatest = func(context.Context) ([]dockerbrew.Bottle, error) { return installedDocker.Bottles[:1], nil }
		}, nil},
		{"missing digest", func(d *HomebrewDeps) {
			d.DockerLatest = func(context.Context) ([]dockerbrew.Bottle, error) {
				return []dockerbrew.Bottle{{Name: "docker-engine", Version: "29.9.2"}}, nil
			}
		}, nil},
		{"duplicate bottle", func(d *HomebrewDeps) {
			d.DockerLatest = func(context.Context) ([]dockerbrew.Bottle, error) {
				return []dockerbrew.Bottle{installedDocker.Bottles[0], installedDocker.Bottles[0]}, nil
			}
		}, nil},
		{"empty manifest", func(d *HomebrewDeps) {
			d.DockerManifest = func() (dockerbrew.Manifest, error) { return dockerbrew.Manifest{}, nil }
		}, nil},
		{"missing read dependency", func(d *HomebrewDeps) { d.DockerLatest = nil }, errDockerRuntimeUnavailable},
		{"helper no longer supports update", func(d *HomebrewDeps) { d.DockerUpdateSupported = func() bool { return false } }, errDockerRuntimeUnavailable},
		{"image units appeared", func(d *HomebrewDeps) { d.DockerImageUnits = func() bool { return true } }, errDockerRuntimeUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			deps := newDockerDeps().deps()
			tt.change(&deps)
			result, err := updateDockerRuntime(context.Background(), deps)
			if err == nil || result.Changed || result.Preview {
				t.Fatalf("unverified update = %+v, %v", result, err)
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Fatalf("lost cause %v: %v", tt.cause, err)
			}
		})
	}
}

func TestDockerUpdateCancelledAfterHelperDoesNotClaimSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	deps := newDockerDeps().deps()
	deps.UpdateDocker = func(context.Context) error { cancel(); return nil }
	result, err := updateDockerRuntime(ctx, deps)
	if !errors.Is(err, context.Canceled) || result.Changed {
		t.Fatalf("cancelled verification = %+v, %v", result, err)
	}
}
