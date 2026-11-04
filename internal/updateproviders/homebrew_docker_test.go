package updateproviders

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dockerbrew"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/updateflow"
)

var (
	installedDocker = dockerbrew.Manifest{Arch: "x86_64_linux", Bottles: []dockerbrew.Bottle{
		{Name: "docker-engine", Version: "29.9.2", SHA256: "aaaa"},
		{Name: "runc", Version: "1.5.2", SHA256: "bbbb"},
	}}
	newerDocker = []dockerbrew.Bottle{
		{Name: "docker-engine", Version: "29.10.0", Revision: 1, SHA256: "cccc"},
		{Name: "runc", Version: "1.5.2", SHA256: "bbbb"},
	}
)

// dockerDeps is a Homebrew provider with nothing outdated in brew and a
// Homebrew Docker runtime that has newer bottles; tests change one fact.
type dockerDeps struct {
	manifestErr error
	imageUnits  bool
	supported   bool
	latest      []dockerbrew.Bottle
	latestErr   error
	dryRun      bool
	updateErr   error
	calls       []string
}

func (d *dockerDeps) deps() HomebrewDeps {
	return HomebrewDeps{
		Installed:    func() bool { return true },
		ListOutdated: func() ([]homebrew.Package, error) { return []homebrew.Package{{Name: "jq", Version: "1.7"}}, nil },
		Update: func(context.Context) error {
			d.calls = append(d.calls, "brew update")
			return nil
		},
		Upgrade: func(_ context.Context, name string) error {
			d.calls = append(d.calls, "brew upgrade"+name)
			return nil
		},
		DryRun: func() bool { return d.dryRun },
		DockerManifest: func() (dockerbrew.Manifest, error) {
			d.calls = append(d.calls, "manifest")
			if d.manifestErr != nil {
				return dockerbrew.Manifest{}, d.manifestErr
			}
			return installedDocker, nil
		},
		DockerImageUnits:      func() bool { return d.imageUnits },
		DockerUpdateSupported: func() bool { return d.supported },
		DockerLatest: func(context.Context) ([]dockerbrew.Bottle, error) {
			d.calls = append(d.calls, "latest")
			return d.latest, d.latestErr
		},
		UpdateDocker: func(context.Context) error {
			d.calls = append(d.calls, "helper docker-update")
			if d.updateErr == nil {
				d.latest = installedDocker.Bottles // The helper installed what Homebrew publishes.
			}
			return d.updateErr
		},
	}
}

func newDockerDeps() *dockerDeps {
	return &dockerDeps{supported: true, latest: newerDocker}
}

var dockerItem = updateflow.Item{ID: DockerRuntimeItemID, Name: "Docker engine", CurrentVersion: "29.9.2", AvailableVersion: "29.10.0_1"}

func TestHomebrewCheckOffersTheDockerRuntimeOnlyWhenTheHelperCanUpdateIt(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*dockerDeps)
		offer  bool
	}{
		{"newer bottles", func(*dockerDeps) {}, true},
		{"current bottles", func(d *dockerDeps) { d.latest = installedDocker.Bottles }, false},
		{"no runtime installed", func(d *dockerDeps) { d.manifestErr = fmt.Errorf("read: %w", os.ErrNotExist) }, false},
		{"unreadable manifest", func(d *dockerDeps) { d.manifestErr = errors.New("corrupt manifest") }, false},
		// The image's own Docker units make docker-update a no-op.
		{"image ships Docker", func(d *dockerDeps) { d.imageUnits = true }, false},
		{"helper lacks docker-update", func(d *dockerDeps) { d.supported = false }, false},
		{"Homebrew API unreachable", func(d *dockerDeps) { d.latestErr = errors.New("dial tcp: no route") }, false},
		{"empty answer", func(d *dockerDeps) { d.latest = nil }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := newDockerDeps()
			test.change(d)
			got, err := newHomebrew(d.deps()).Check(context.Background())
			if err != nil {
				t.Fatalf("Check() error = %v; an advisory Docker read must not fail the tools check", err)
			}
			want := []updateflow.Item{{Name: "jq", CurrentVersion: "1.7"}}
			if test.offer {
				want = append(want, dockerItem)
			}
			if !reflect.DeepEqual(got.Items, want) {
				t.Fatalf("Check() items = %#v, want %#v", got.Items, want)
			}
		})
	}
}

func TestHomebrewCheckSkipsTheNetworkWithoutAnUpdatableRuntime(t *testing.T) {
	for _, change := range []func(*dockerDeps){
		func(d *dockerDeps) { d.manifestErr = os.ErrNotExist },
		func(d *dockerDeps) { d.imageUnits = true },
		func(d *dockerDeps) { d.supported = false },
	} {
		d := newDockerDeps()
		change(d)
		if _, err := newHomebrew(d.deps()).Check(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, call := range d.calls {
			if call == "latest" {
				t.Fatalf("calls = %q: queried Homebrew for a runtime the helper would not update", d.calls)
			}
		}
	}
}

func TestHomebrewApplyUpdatesTheDockerRuntimeOnlyWhenPending(t *testing.T) {
	for _, test := range []struct {
		name    string
		items   []updateflow.Item
		dryRun  bool
		want    []string
		preview bool
	}{
		{"pending", []updateflow.Item{{Name: "jq"}, dockerItem}, false, []string{"brew update", "brew upgrade", "helper docker-update"}, false},
		{"not pending", []updateflow.Item{{Name: "jq"}}, false, []string{"brew update", "brew upgrade"}, false},
		// A brew formula that happens to be called docker is not the runtime.
		{"brew docker formula", []updateflow.Item{{Name: "docker"}}, false, []string{"brew update", "brew upgrade"}, false},
		{"preview still reaches the journaled helper", []updateflow.Item{dockerItem}, true, []string{"brew update", "brew upgrade", "helper docker-update"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := newDockerDeps()
			d.dryRun = test.dryRun
			var messages []string
			result, err := newHomebrew(d.deps()).Apply(context.Background(), test.items, func(p updateflow.Progress) { messages = append(messages, p.Message) })
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if !reflect.DeepEqual(d.calls, test.want) {
				t.Fatalf("calls = %q, want %q", d.calls, test.want)
			}
			if result.Preview != test.preview || result.Changed == test.preview {
				t.Fatalf("Apply() = %#v", result)
			}
			updating := messages[len(messages)-1] == "Updating Docker engine…"
			if updating != (len(test.want) == 3) {
				t.Fatalf("progress = %q", messages)
			}
		})
	}
}

func TestHomebrewApplyReportsADockerRuntimeFailure(t *testing.T) {
	d := newDockerDeps()
	d.updateErr = errors.New("authentication dismissed")
	result, err := newHomebrew(d.deps()).Apply(context.Background(), []updateflow.Item{dockerItem}, nil)
	if !errors.Is(err, d.updateErr) || result.Changed || result.Preview {
		t.Fatalf("Apply() = %#v, %v", result, err)
	}
}

func TestDockerRuntimeIndividualUpdateConfirmsTheNewBottles(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*dockerDeps)
		want   updateflow.ApplyResult
		err    bool
	}{
		{"installed", func(*dockerDeps) {}, updateflow.ApplyResult{Changed: true}, false},
		{"preview", func(d *dockerDeps) { d.dryRun = true }, updateflow.ApplyResult{Preview: true}, false},
		{"helper failed", func(d *dockerDeps) { d.updateErr = errors.New("helper refused") }, updateflow.ApplyResult{}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := newDockerDeps()
			test.change(d)
			result, err := updateDockerRuntime(context.Background(), d.deps())
			if (err != nil) != test.err || !reflect.DeepEqual(result, test.want) {
				t.Fatalf("updateDockerRuntime() = %#v, %v; want %#v", result, err, test.want)
			}
		})
	}
	// A helper that exited 0 while Homebrew still publishes other bottles
	// did not update anything the row can claim.
	d := newDockerDeps()
	deps := d.deps()
	deps.UpdateDocker = func(context.Context) error { return nil }
	if result, err := updateDockerRuntime(context.Background(), deps); err != nil || result.Changed {
		t.Fatalf("unverified update = %#v, %v", result, err)
	}
}

// A stalled Homebrew API omits the Docker item within the bound instead of
// holding the whole check.
func TestHomebrewCheckBoundsTheDockerAPIRead(t *testing.T) {
	old := dockerLatestTimeout
	dockerLatestTimeout = 20 * time.Millisecond
	t.Cleanup(func() { dockerLatestTimeout = old })
	deps := newDockerDeps().deps()
	deps.DockerLatest = func(ctx context.Context) ([]dockerbrew.Bottle, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	done := make(chan updateflow.CheckResult, 1)
	go func() {
		got, _ := newHomebrew(deps).Check(context.Background())
		done <- got
	}()
	select {
	case got := <-done:
		if want := []updateflow.Item{{Name: "jq", CurrentVersion: "1.7"}}; !reflect.DeepEqual(got.Items, want) {
			t.Fatalf("Check() items = %#v, want %#v", got.Items, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Check() blocked on a stalled Docker API read")
	}
}
