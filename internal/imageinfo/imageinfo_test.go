package imageinfo

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Descriptors captured from the three supported images. They are the fixture
// every variant/channel/target case below is built from, so a change in what
// ChairLift expects to find on disk shows up in exactly one place.
const (
	dakotaDescriptor = `{
	  "image-name": "dakota",
	  "image-tag": "latest",
	  "image-ref": "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota",
	  "image-vendor": "projectbluefin",
	  "image-flavor": "main"
	}`

	bluefinDescriptor = `{
	  "image-name": "bluefin",
	  "image-tag": "stable",
	  "image-ref": "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin",
	  "image-vendor": "ublue-os",
	  "image-flavor": "main"
	}`

	// projectbluefin/bluefin-lts, not ublue-os/bluefin-lts: the latter is
	// not a published package (GHCR returns no pull token for it).
	bluefinLTSDescriptor = `{
	  "image-name": "bluefin-lts",
	  "image-tag": "lts",
	  "image-ref": "ostree-image-signed:docker://ghcr.io/projectbluefin/bluefin-lts",
	  "image-vendor": "projectbluefin",
	  "image-flavor": "lts"
	}`
)

func parseOrFatal(t *testing.T, descriptor string) Info {
	t.Helper()
	info, err := Parse(strings.NewReader(descriptor))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	return info
}

func TestParseReadsSupportedDescriptors(t *testing.T) {
	tests := []struct {
		name       string
		descriptor string
		want       Info
	}{
		{
			name:       "dakota",
			descriptor: dakotaDescriptor,
			want: Info{
				Name:   "dakota",
				Tag:    "latest",
				Ref:    "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota",
				Vendor: "projectbluefin",
				Flavor: "main",
			},
		},
		{
			name:       "bluefin",
			descriptor: bluefinDescriptor,
			want: Info{
				Name:   "bluefin",
				Tag:    "stable",
				Ref:    "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin",
				Vendor: "ublue-os",
				Flavor: "main",
			},
		},
		{
			name:       "bluefin lts",
			descriptor: bluefinLTSDescriptor,
			want: Info{
				Name:   "bluefin-lts",
				Tag:    "lts",
				Ref:    "ostree-image-signed:docker://ghcr.io/projectbluefin/bluefin-lts",
				Vendor: "projectbluefin",
				Flavor: "lts",
			},
		},
		{
			name:       "unknown keys are ignored",
			descriptor: `{"image-name":"dakota","image-ref":"docker://ghcr.io/x/dakota","future-key":{"a":1}}`,
			want:       Info{Name: "dakota", Ref: "docker://ghcr.io/x/dakota"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parseOrFatal(t, test.descriptor)
			if got != test.want {
				t.Errorf("Parse() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestParseRejectsUnusableDescriptors(t *testing.T) {
	tests := []struct {
		name       string
		descriptor string
	}{
		{name: "malformed json", descriptor: `{"image-name":`},
		{name: "not an object", descriptor: `["dakota"]`},
		{name: "empty object", descriptor: `{}`},
		{name: "tag only", descriptor: `{"image-tag":"latest"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(test.descriptor)); err == nil {
				t.Fatal("Parse() error = nil, want non-nil")
			}
		})
	}
}

func TestLoadReportsMissingDescriptor(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load() error = %v, want one wrapping fs.ErrNotExist", err)
	}
}

func TestVariantCoversEverySupportedImage(t *testing.T) {
	tests := []struct {
		name        string
		info        Info
		want        Variant
		wantDisplay string
		wantSupport bool
	}{
		{
			name:        "dakota",
			info:        parseOrFatal(t, dakotaDescriptor),
			want:        VariantDakota,
			wantDisplay: "Dakota",
			wantSupport: true,
		},
		{
			name:        "bluefin",
			info:        parseOrFatal(t, bluefinDescriptor),
			want:        VariantBluefin,
			wantDisplay: "Bluefin",
			wantSupport: true,
		},
		{
			// The ordering trap: "bluefin-lts" contains "bluefin", so a
			// Bluefin-first match would classify every LTS host as Bluefin
			// and offer it the wrong channel tags.
			name:        "bluefin lts is not bluefin",
			info:        parseOrFatal(t, bluefinLTSDescriptor),
			want:        VariantBluefinLTS,
			wantDisplay: "Bluefin LTS",
			wantSupport: true,
		},
		{
			name:        "lts detected from ref when name is generic",
			info:        Info{Name: "os", Ref: "docker://ghcr.io/ublue-os/bluefin-lts:lts"},
			want:        VariantBluefinLTS,
			wantDisplay: "Bluefin LTS",
			wantSupport: true,
		},
		{
			name:        "aurora is unsupported",
			info:        Info{Name: "aurora", Ref: "docker://ghcr.io/ublue-os/aurora"},
			want:        VariantUnknown,
			wantDisplay: "Unknown",
			wantSupport: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.info.Variant()
			if got != test.want {
				t.Errorf("Variant() = %q, want %q", got, test.want)
			}
			if display := got.DisplayName(); display != test.wantDisplay {
				t.Errorf("DisplayName() = %q, want %q", display, test.wantDisplay)
			}
			if supported := got.Supported(); supported != test.wantSupport {
				t.Errorf("Supported() = %v, want %v", supported, test.wantSupport)
			}
		})
	}
}

// The gaming images ship the stack as system packages, so ChairLift's
// Flatpak-based gaming mode must not be offered on them: it would install a
// second, shadowing copy of Steam and friends. Reported against
// ghcr.io/projectbluefin/dakota-gaming:next and
// ghcr.io/projectbluefin/dakota-nvidia-gaming:next.
func TestGamingImagesAreRecognizedFromTheDescriptor(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want bool
	}{
		{
			name: "dakota-gaming by flavor and name",
			info: parseOrFatal(t, `{
			  "image-name": "dakota-gaming",
			  "image-tag": "next",
			  "image-ref": "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-gaming:next",
			  "image-vendor": "projectbluefin",
			  "image-flavor": "gaming"
			}`),
			want: true,
		},
		{
			// The driver suffix sits *before* the gaming suffix, so a
			// match on "-gaming" has to be a suffix test on the whole
			// name rather than an equality test against a known list.
			name: "dakota-nvidia-gaming",
			info: parseOrFatal(t, `{
			  "image-name": "dakota-nvidia-gaming",
			  "image-tag": "next",
			  "image-ref": "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-nvidia-gaming:next",
			  "image-vendor": "projectbluefin",
			  "image-flavor": "gaming"
			}`),
			want: true,
		},
		{
			// image-flavor alone is enough: it is the authoritative
			// signal, and a descriptor may carry a generic image-name.
			name: "flavor only",
			info: Info{Name: "os", Ref: "docker://ghcr.io/projectbluefin/dakota:next", Flavor: "gaming"},
			want: true,
		},
		{
			// And the name alone is enough too, for a descriptor that
			// omits image-flavor entirely.
			name: "name suffix only, no flavor",
			info: Info{Name: "dakota-gaming", Ref: "docker://ghcr.io/projectbluefin/dakota-gaming:next"},
			want: true,
		},
		{
			name: "ref suffix only, generic name and no flavor",
			info: Info{Name: "os", Ref: "docker://ghcr.io/projectbluefin/dakota-gaming:next"},
			want: true,
		},
		{
			name: "flavor is compared case-insensitively",
			info: Info{Name: "os", Ref: "docker://ghcr.io/projectbluefin/dakota:next", Flavor: "Gaming"},
			want: true,
		},
		{
			// The three supported non-gaming descriptors must keep the
			// toggle, or this fix would remove the feature everywhere.
			name: "dakota is not a gaming image",
			info: parseOrFatal(t, dakotaDescriptor),
			want: false,
		},
		{
			name: "bluefin is not a gaming image",
			info: parseOrFatal(t, bluefinDescriptor),
			want: false,
		},
		{
			name: "bluefin lts is not a gaming image",
			info: parseOrFatal(t, bluefinLTSDescriptor),
			want: false,
		},
		{
			// "gaming" appearing anywhere other than the flavor or the
			// end of the image name is not a gaming image. A substring
			// match would strip the toggle from an unrelated image.
			name: "gaming in the middle of the name is not a suffix",
			info: Info{Name: "gaming-tools-demo", Ref: "docker://ghcr.io/example/gaming-tools-demo:latest"},
			want: false,
		},
		{
			name: "empty descriptor fields offer the toggle",
			info: Info{Name: "dakota", Ref: "docker://ghcr.io/projectbluefin/dakota:latest"},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.info.IsGaming(); got != test.want {
				t.Errorf("IsGaming() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCleanRefStripsTransportAndTag(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{
			name: "signed docker transport",
			ref:  "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota",
			want: "ghcr.io/projectbluefin/dakota",
		},
		{
			name: "unverified transport with tag",
			ref:  "ostree-unverified-image:docker://ghcr.io/ublue-os/bluefin:stable",
			want: "ghcr.io/ublue-os/bluefin",
		},
		{
			name: "scheme without transport",
			ref:  "ostree-image-signed:ghcr.io/ublue-os/bluefin-lts:lts",
			want: "ghcr.io/ublue-os/bluefin-lts",
		},
		{
			name: "bare docker prefix",
			ref:  "docker://ghcr.io/projectbluefin/dakota:testing",
			want: "ghcr.io/projectbluefin/dakota",
		},
		{
			name: "no prefix at all",
			ref:  "ghcr.io/projectbluefin/dakota",
			want: "ghcr.io/projectbluefin/dakota",
		},
		{
			name: "registry port is not a transport or a tag",
			ref:  "registry.example:5000/team/dakota:latest",
			want: "registry.example:5000/team/dakota",
		},
		{
			name: "empty",
			ref:  "",
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := (Info{Ref: test.ref}).CleanRef(); got != test.want {
				t.Errorf("CleanRef() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEffectiveTagPrefersTheBootedStream(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{name: "descriptor tag wins", info: Info{Tag: "lts", Ref: "docker://ghcr.io/x/y:stale"}, want: "lts"},
		{name: "falls back to ref tag", info: Info{Ref: "docker://ghcr.io/x/y:testing"}, want: "testing"},
		{name: "no tag anywhere", info: Info{Ref: "docker://ghcr.io/x/y"}, want: ""},
		{name: "registry port is not a tag", info: Info{Ref: "registry.example:5000/x/y"}, want: ""},

		// The boot-time marker outranks the baked tag, because Dakota bakes
		// "latest" on every stream. This is the case observed on a real
		// dakota-gaming host on 2026-09-21: the descriptor said "latest",
		// which that image does not publish, and the marker said "testing".
		{
			name: "boot marker outranks a baked tag",
			info: Info{
				Tag:       "latest",
				Ref:       "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-gaming",
				BootedRef: "ghcr.io/projectbluefin/dakota-gaming:testing",
			},
			want: "testing",
		},
		{
			name: "boot marker keeps its transport prefix out of the tag",
			info: Info{
				Tag:       "latest",
				Ref:       "docker://ghcr.io/projectbluefin/dakota",
				BootedRef: "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota:stable",
			},
			want: "stable",
		},
		{
			// A marker left by a deployment of a different image must not
			// move this host onto that image's streams.
			name: "marker for another image is ignored",
			info: Info{
				Tag:       "stable",
				Ref:       "docker://ghcr.io/projectbluefin/dakota-gaming",
				BootedRef: "ghcr.io/projectbluefin/dakota:testing",
			},
			want: "stable",
		},
		{
			// A pinned host is on no stream at all, and the baked tag must
			// not be allowed to invent one for it.
			name: "digest-pinned marker names no stream",
			info: Info{
				Tag:       "latest",
				Ref:       "docker://ghcr.io/projectbluefin/dakota-gaming",
				BootedRef: "ghcr.io/projectbluefin/dakota-gaming@sha256:0123456789abcdef",
			},
			want: "",
		},
		{
			name: "marker without a tag names no stream",
			info: Info{
				Tag:       "latest",
				Ref:       "docker://ghcr.io/projectbluefin/dakota-gaming",
				BootedRef: "ghcr.io/projectbluefin/dakota-gaming",
			},
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.info.EffectiveTag(); got != test.want {
				t.Errorf("EffectiveTag() = %q, want %q", got, test.want)
			}
		})
	}
}

// The marker is a file on tmpfs that may be absent, empty, or larger than
// one reference. Reading it must never be the reason startup fails.
func TestBootedRefReadsTheMarkerDefensively(t *testing.T) {
	dir := t.TempDir()

	if got := readBootedRef(filepath.Join(dir, "absent")); got != "" {
		t.Errorf("readBootedRef(absent) = %q, want \"\"", got)
	}

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "one reference", content: "ghcr.io/projectbluefin/dakota-gaming:testing\n", want: "ghcr.io/projectbluefin/dakota-gaming:testing"},
		{name: "no trailing newline", content: "ghcr.io/projectbluefin/dakota:stable", want: "ghcr.io/projectbluefin/dakota:stable"},
		{name: "surrounding whitespace", content: "  ghcr.io/projectbluefin/dakota:stable  \n", want: "ghcr.io/projectbluefin/dakota:stable"},
		{name: "only the first line", content: "ghcr.io/projectbluefin/dakota:stable\nghcr.io/someone/else:latest\n", want: "ghcr.io/projectbluefin/dakota:stable"},
		{name: "empty", content: "", want: ""},
		// A corrupt marker must not be read without bound, and must not be
		// mistaken for a reference.
		{name: "oversized junk", content: strings.Repeat("x", 4096), want: strings.Repeat("x", 1024)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(dir, "booted-image")
			if err := os.WriteFile(path, []byte(test.content), 0o644); err != nil {
				t.Fatalf("writing marker: %v", err)
			}
			if got := readBootedRef(path); got != test.want {
				t.Errorf("readBootedRef() = %q, want %q", got, test.want)
			}
		})
	}
}

// The shape of the machine this alpha was cut on, observed 2026-09-21: the
// descriptor bakes image-tag "latest", which dakota-gaming does not
// publish, while the boot-time marker records the stream it really booted.
// Without the marker this host is on no stream and is offered no switch,
// which is the whole reason the release-channel row looked dead on it.
func TestAGamingHostResolvesItsRealStream(t *testing.T) {
	descriptor := Info{
		Name:   "dakota-gaming",
		Vendor: "projectbluefin",
		Flavor: "gaming",
		Tag:    "latest",
		Ref:    "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-gaming",
	}

	if got := descriptor.Channel(); got != ChannelUnknown {
		t.Errorf("without the marker, Channel() = %q, want %q", got, ChannelUnknown)
	}

	booted := descriptor
	booted.BootedRef = "ghcr.io/projectbluefin/dakota-gaming:testing"

	if got := booted.Channel(); got != ChannelTesting {
		t.Fatalf("Channel() = %q, want %q", got, ChannelTesting)
	}
	target, ok := booted.SwitchTarget(ChannelStable)
	if !ok || target != "ghcr.io/projectbluefin/dakota-gaming:stable" {
		t.Errorf("SwitchTarget(stable) = (%q, %v), want (%q, true)", target, ok, "ghcr.io/projectbluefin/dakota-gaming:stable")
	}
	if _, ok := booted.SwitchTarget(ChannelTesting); ok {
		t.Error("SwitchTarget(testing) offered a switch to the stream the host is already on")
	}
}

// The published tag surface of each image, confirmed by GHCR manifest
// request on 2026-08-17. Every case below is one of those observations, so
// the table in imageinfo.go cannot drift from the registry without a test
// failure to explain it.
func TestChannelClassifiesRunningTagPerImage(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		tag  string
		want Channel
	}{
		{name: "bluefin latest", ref: "ghcr.io/ublue-os/bluefin", tag: "latest", want: ChannelStable},
		{name: "bluefin stable", ref: "ghcr.io/ublue-os/bluefin", tag: "stable", want: ChannelStable},
		{name: "bluefin gts", ref: "ghcr.io/ublue-os/bluefin", tag: "gts", want: ChannelStable},
		{name: "bluefin stable-daily", ref: "ghcr.io/ublue-os/bluefin", tag: "stable-daily", want: ChannelStable},
		// Retired upstream: ghcr.io/ublue-os/bluefin:beta was 200 on
		// 2026-08-17 and 404 on 2026-09-21, so it names no stream now.
		{name: "bluefin beta is retired", ref: "ghcr.io/ublue-os/bluefin", tag: "beta", want: ChannelUnknown},
		{name: "bluefin lts", ref: "ghcr.io/ublue-os/bluefin", tag: "lts", want: ChannelStable},
		{name: "bluefin lts-testing", ref: "ghcr.io/ublue-os/bluefin", tag: "lts-testing", want: ChannelTesting},
		{name: "bluefin lts-hwe-testing", ref: "ghcr.io/ublue-os/bluefin", tag: "lts-hwe-testing", want: ChannelTesting},

		{name: "projectbluefin lts", ref: "ghcr.io/projectbluefin/bluefin-lts", tag: "lts", want: ChannelStable},
		{name: "projectbluefin lts stable", ref: "ghcr.io/projectbluefin/bluefin-lts", tag: "stable", want: ChannelStable},
		{name: "projectbluefin lts testing", ref: "ghcr.io/projectbluefin/bluefin-lts", tag: "testing", want: ChannelTesting},
		// The tag ublue-os/bluefin uses for its LTS testing stream is not
		// published for this image at all.
		{name: "projectbluefin lts has no lts-testing", ref: "ghcr.io/projectbluefin/bluefin-lts", tag: "lts-testing", want: ChannelUnknown},

		{name: "dakota latest", ref: "ghcr.io/projectbluefin/dakota", tag: "latest", want: ChannelStable},
		{name: "dakota stable", ref: "ghcr.io/projectbluefin/dakota", tag: "stable", want: ChannelStable},
		{name: "dakota testing", ref: "ghcr.io/projectbluefin/dakota", tag: "testing", want: ChannelTesting},
		{name: "dakota pinned build", ref: "ghcr.io/projectbluefin/dakota", tag: "latest.20260212", want: ChannelUnknown},

		{name: "dakota gaming stable", ref: "ghcr.io/projectbluefin/dakota-gaming", tag: "stable", want: ChannelStable},
		{name: "dakota gaming testing", ref: "ghcr.io/projectbluefin/dakota-gaming", tag: "testing", want: ChannelTesting},
		// The gaming image publishes no "latest", even though its own
		// descriptor declares image-tag "latest". Classifying that tag onto
		// a stream would claim a reference GHCR answers 404 for.
		{name: "dakota gaming has no latest stream", ref: "ghcr.io/projectbluefin/dakota-gaming", tag: "latest", want: ChannelUnknown},
		// "next" and "btw" are published but unclassified, so they are not
		// silently folded into either stream.
		{name: "dakota gaming next is unclassified", ref: "ghcr.io/projectbluefin/dakota-gaming", tag: "next", want: ChannelUnknown},

		{name: "unknown image", ref: "ghcr.io/someone/custom-bluefin", tag: "latest", want: ChannelUnknown},
		{name: "no tag", ref: "ghcr.io/projectbluefin/dakota", tag: "", want: ChannelUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := Info{Name: "bluefin", Tag: test.tag, Ref: "docker://" + test.ref}
			if got := info.Channel(); got != test.want {
				t.Errorf("Channel() for %s:%s = %q, want %q", test.ref, test.tag, got, test.want)
			}
		})
	}
}

func TestTargetTagUsesThePerImageTable(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		tag     string
		channel Channel
		want    string
		wantOK  bool
	}{
		// ── ghcr.io/ublue-os/bluefin ──────────────────────────────────────
		// The correction that motivates this whole table: bluefinctl maps
		// latest/stable to a bare "testing" tag, and ghcr.io/ublue-os/
		// bluefin:testing does not exist (404). The switch must stay inert
		// rather than stage a failing bootc transaction.
		{name: "bluefin latest has no testing counterpart", ref: "ghcr.io/ublue-os/bluefin", tag: "latest", channel: ChannelTesting, wantOK: false},
		{name: "bluefin stable has no testing counterpart", ref: "ghcr.io/ublue-os/bluefin", tag: "stable", channel: ChannelTesting, wantOK: false},
		{name: "bluefin gts has no testing counterpart", ref: "ghcr.io/ublue-os/bluefin", tag: "gts", channel: ChannelTesting, wantOK: false},
		{name: "bluefin lts to testing", ref: "ghcr.io/ublue-os/bluefin", tag: "lts", channel: ChannelTesting, want: "lts-testing", wantOK: true},
		{name: "bluefin lts-hwe to testing", ref: "ghcr.io/ublue-os/bluefin", tag: "lts-hwe", channel: ChannelTesting, want: "lts-hwe-testing", wantOK: true},
		{name: "bluefin lts-testing back", ref: "ghcr.io/ublue-os/bluefin", tag: "lts-testing", channel: ChannelStable, want: "lts", wantOK: true},
		{name: "bluefin lts-hwe-testing back", ref: "ghcr.io/ublue-os/bluefin", tag: "lts-hwe-testing", channel: ChannelStable, want: "lts-hwe", wantOK: true},

		// ── ghcr.io/projectbluefin/bluefin-lts ────────────────────────────
		// The second correction: this image's testing stream is the bare
		// "testing" tag; :lts-testing is a 404 here.
		{name: "projectbluefin lts to testing", ref: "ghcr.io/projectbluefin/bluefin-lts", tag: "lts", channel: ChannelTesting, want: "testing", wantOK: true},
		{name: "projectbluefin stable to testing", ref: "ghcr.io/projectbluefin/bluefin-lts", tag: "stable", channel: ChannelTesting, want: "testing", wantOK: true},
		{name: "projectbluefin testing back to stable", ref: "ghcr.io/projectbluefin/bluefin-lts", tag: "testing", channel: ChannelStable, want: "stable", wantOK: true},

		// ── ghcr.io/projectbluefin/dakota ─────────────────────────────────
		{name: "dakota latest to testing", ref: "ghcr.io/projectbluefin/dakota", tag: "latest", channel: ChannelTesting, want: "testing", wantOK: true},
		{name: "dakota stable to testing", ref: "ghcr.io/projectbluefin/dakota", tag: "stable", channel: ChannelTesting, want: "testing", wantOK: true},
		{name: "dakota testing back to stable", ref: "ghcr.io/projectbluefin/dakota", tag: "testing", channel: ChannelStable, want: "stable", wantOK: true},

		// ── refusals ──────────────────────────────────────────────────────
		{name: "already on testing", ref: "ghcr.io/projectbluefin/dakota", tag: "testing", channel: ChannelTesting, wantOK: false},
		{name: "already on stable", ref: "ghcr.io/projectbluefin/dakota", tag: "latest", channel: ChannelStable, wantOK: false},
		{name: "pinned build", ref: "ghcr.io/projectbluefin/dakota", tag: "latest.20260212", channel: ChannelTesting, wantOK: false},
		{name: "unknown image is never guessed", ref: "ghcr.io/someone/custom-bluefin", tag: "stable", channel: ChannelTesting, wantOK: false},
		{name: "empty tag", ref: "ghcr.io/projectbluefin/dakota", tag: "", channel: ChannelTesting, wantOK: false},
		{name: "unknown channel", ref: "ghcr.io/projectbluefin/dakota", tag: "latest", channel: ChannelUnknown, wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := TargetTag(test.ref, test.tag, test.channel)
			if ok != test.wantOK {
				t.Fatalf("TargetTag(%q, %q, %q) ok = %v, want %v", test.ref, test.tag, test.channel, ok, test.wantOK)
			}
			if ok && got != test.want {
				t.Errorf("TargetTag(%q, %q, %q) = %q, want %q", test.ref, test.tag, test.channel, got, test.want)
			}
		})
	}
}

func TestSwitchTargetBuildsFullReferences(t *testing.T) {
	tests := []struct {
		name    string
		info    Info
		channel Channel
		want    string
		wantOK  bool
	}{
		{
			name:    "dakota to testing",
			info:    parseOrFatal(t, dakotaDescriptor),
			channel: ChannelTesting,
			want:    "ghcr.io/projectbluefin/dakota:testing",
			wantOK:  true,
		},
		{
			// The shipping gaming image. Before it had a table entry of its
			// own, a gaming host matched nothing and the Updates page
			// offered no release-channel switch at all.
			name: "dakota gaming to testing",
			info: Info{
				Name:   "dakota-gaming",
				Flavor: "gaming",
				Tag:    "stable",
				Ref:    "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-gaming",
			},
			channel: ChannelTesting,
			want:    "ghcr.io/projectbluefin/dakota-gaming:testing",
			wantOK:  true,
		},
		{
			name: "dakota gaming back to stable",
			info: Info{
				Name:   "dakota-gaming",
				Flavor: "gaming",
				Tag:    "testing",
				Ref:    "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-gaming",
			},
			channel: ChannelStable,
			want:    "ghcr.io/projectbluefin/dakota-gaming:stable",
			wantOK:  true,
		},
		{
			// The descriptor the image actually ships declares image-tag
			// "latest", which GHCR does not publish for it. Recognizing it
			// would put the host on a stream that does not exist.
			name: "dakota gaming on the unpublished latest tag",
			info: Info{
				Name:   "dakota-gaming",
				Flavor: "gaming",
				Tag:    "latest",
				Ref:    "ostree-image-signed:docker://ghcr.io/projectbluefin/dakota-gaming",
			},
			channel: ChannelTesting,
			wantOK:  false,
		},
		{
			name:    "bluefin lts to testing",
			info:    parseOrFatal(t, bluefinLTSDescriptor),
			channel: ChannelTesting,
			want:    "ghcr.io/projectbluefin/bluefin-lts:testing",
			wantOK:  true,
		},
		{
			name:    "bluefin lts back to stable",
			info:    Info{Name: "bluefin-lts", Tag: "testing", Ref: "docker://ghcr.io/projectbluefin/bluefin-lts"},
			channel: ChannelStable,
			want:    "ghcr.io/projectbluefin/bluefin-lts:stable",
			wantOK:  true,
		},
		{
			name:    "bluefin on the lts stream",
			info:    Info{Name: "bluefin", Tag: "lts", Ref: "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin"},
			channel: ChannelTesting,
			want:    "ghcr.io/ublue-os/bluefin:lts-testing",
			wantOK:  true,
		},
		{
			// Bluefin Stable — the most common Bluefin host — genuinely has
			// nowhere to switch to.
			name:    "bluefin stable offers no switch",
			info:    parseOrFatal(t, bluefinDescriptor),
			channel: ChannelTesting,
			wantOK:  false,
		},
		{
			name:    "ref without a registry path",
			info:    Info{Name: "dakota", Tag: "latest", Ref: "dakota"},
			channel: ChannelTesting,
			wantOK:  false,
		},
		{
			name:    "no ref at all",
			info:    Info{Name: "dakota", Tag: "latest"},
			channel: ChannelTesting,
			wantOK:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := test.info.SwitchTarget(test.channel)
			if ok != test.wantOK {
				t.Fatalf("SwitchTarget(%q) ok = %v, want %v", test.channel, ok, test.wantOK)
			}
			if ok && got != test.want {
				t.Errorf("SwitchTarget(%q) = %q, want %q", test.channel, got, test.want)
			}
		})
	}
}

// Every mapping in the table must land on a tag the same image also
// recognizes, in both directions. A typo that pointed lts at "lts-testng"
// would otherwise only surface as a failed bootc switch on a real host.
func TestChannelTableIsInternallyConsistent(t *testing.T) {
	for ref, channels := range imageChannelMap {
		t.Run(ref, func(t *testing.T) {
			for from, to := range channels.toTesting {
				if !slices.Contains(channels.stableTags, from) {
					t.Errorf("toTesting source %q is not listed in stableTags", from)
				}
				if !slices.Contains(channels.testingTags, to) {
					t.Errorf("toTesting target %q is not listed in testingTags", to)
				}
			}
			for from, to := range channels.toStable {
				if !slices.Contains(channels.testingTags, from) {
					t.Errorf("toStable source %q is not listed in testingTags", from)
				}
				if !slices.Contains(channels.stableTags, to) {
					t.Errorf("toStable target %q is not listed in stableTags", to)
				}
			}
			for _, tag := range channels.testingTags {
				if _, ok := channels.toStable[tag]; !ok {
					t.Errorf("testing tag %q has no way back to a stable tag", tag)
				}
			}
		})
	}
}
