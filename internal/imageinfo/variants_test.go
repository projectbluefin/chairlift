package imageinfo

import (
	"reflect"
	"slices"
	"testing"
)

func TestSplitDriverRecognizesOnlyManagedImages(t *testing.T) {
	tests := []struct {
		name       string
		ref        string
		wantBase   string
		wantDriver Driver
	}{
		{name: "base bluefin", ref: "ghcr.io/ublue-os/bluefin", wantBase: "ghcr.io/ublue-os/bluefin", wantDriver: DriverStandard},
		{name: "bluefin nvidia", ref: "ghcr.io/ublue-os/bluefin-nvidia", wantBase: "ghcr.io/ublue-os/bluefin", wantDriver: DriverNVIDIA},
		// "nvidia-open" must be matched before "nvidia", or an open-modules
		// host is misread as proprietary and offered a pointless switch.
		{name: "bluefin nvidia open", ref: "ghcr.io/ublue-os/bluefin-nvidia-open", wantBase: "ghcr.io/ublue-os/bluefin", wantDriver: DriverNVIDIAOpen},
		{name: "dakota nvidia", ref: "ghcr.io/projectbluefin/dakota-nvidia", wantBase: "ghcr.io/projectbluefin/dakota", wantDriver: DriverNVIDIA},
		// The gaming flavour keeps its suffix last, so the driver suffix is
		// an infix. Splitting on the trailing "-gaming" would leave the
		// nvidia variant unrecognized, and composing the driver suffix at
		// the end would produce dakota-gaming-nvidia, which is a 404.
		{name: "dakota gaming is its own base", ref: "ghcr.io/projectbluefin/dakota-gaming", wantBase: "ghcr.io/projectbluefin/dakota-gaming", wantDriver: DriverStandard},
		{name: "dakota nvidia gaming", ref: "ghcr.io/projectbluefin/dakota-nvidia-gaming", wantBase: "ghcr.io/projectbluefin/dakota-gaming", wantDriver: DriverNVIDIA},
		// bluefin-dx-nvidia is a variant of an image ChairLift does not
		// manage. Splitting it would produce base "ghcr.io/ublue-os/
		// bluefin-dx", which is not in the table, so it must be left whole.
		{name: "developer image is left alone", ref: "ghcr.io/ublue-os/bluefin-dx-nvidia", wantBase: "ghcr.io/ublue-os/bluefin-dx-nvidia", wantDriver: DriverStandard},
		// Suffixes that are hardware-specific images, not driver flavours.
		// Treating "asus" as a driver would offer a switch that drops the
		// machine's hardware support.
		{name: "asus is not a driver", ref: "ghcr.io/ublue-os/bluefin-asus", wantBase: "ghcr.io/ublue-os/bluefin-asus", wantDriver: DriverStandard},
		{name: "unknown image", ref: "ghcr.io/someone/custom", wantBase: "ghcr.io/someone/custom", wantDriver: DriverStandard},
		{name: "empty", ref: "", wantBase: "", wantDriver: DriverStandard},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base, driver := SplitDriver(test.ref)
			if base != test.wantBase || driver != test.wantDriver {
				t.Errorf("SplitDriver(%q) = (%q, %q), want (%q, %q)",
					test.ref, base, driver, test.wantBase, test.wantDriver)
			}
		})
	}
}

// The whole point of keying on the stream: driver images are published for
// some streams and not others. Every row here is a manifest request made on
// 2026-08-17 or, for the dakota rows, re-made on 2026-09-21, and recorded in
// variants.go's table comment.
func TestAvailableDriversAreStreamDependent(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		tag  string
		want []Driver
	}{
		{
			name: "bluefin latest has both nvidia flavours",
			ref:  "ghcr.io/ublue-os/bluefin",
			tag:  "latest",
			want: []Driver{DriverStandard, DriverNVIDIA, DriverNVIDIAOpen},
		},
		{
			name: "bluefin stable has both",
			ref:  "ghcr.io/ublue-os/bluefin",
			tag:  "stable",
			want: []Driver{DriverStandard, DriverNVIDIA, DriverNVIDIAOpen},
		},
		{
			// The correction: ghcr.io/ublue-os/bluefin-nvidia:lts is a 404,
			// so an LTS host has no driver choice at all. finupdate's
			// family-level variant list would offer it.
			name: "bluefin lts has no driver variants",
			ref:  "ghcr.io/ublue-os/bluefin",
			tag:  "lts",
			want: nil,
		},
		{
			name: "bluefin lts-hwe has none either",
			ref:  "ghcr.io/ublue-os/bluefin",
			tag:  "lts-hwe",
			want: nil,
		},
		{
			// The 2026-09-21 correction: dakota-nvidia:latest is now a 404,
			// so a dakota host on latest has no driver choice left. It was
			// published on 2026-08-17, which is how the stale entry got in.
			name: "dakota latest lost its nvidia variant",
			ref:  "ghcr.io/projectbluefin/dakota",
			tag:  "latest",
			want: nil,
		},
		{
			name: "dakota stable has nvidia",
			ref:  "ghcr.io/projectbluefin/dakota",
			tag:  "stable",
			want: []Driver{DriverStandard, DriverNVIDIA},
		},
		{
			name: "dakota testing has nvidia",
			ref:  "ghcr.io/projectbluefin/dakota",
			tag:  "testing",
			want: []Driver{DriverStandard, DriverNVIDIA},
		},
		{
			name: "dakota gaming stable has nvidia",
			ref:  "ghcr.io/projectbluefin/dakota-gaming",
			tag:  "stable",
			want: []Driver{DriverStandard, DriverNVIDIA},
		},
		{
			// A gaming host already on the NVIDIA image must see the same
			// choice, which only works if the infix split found its base.
			name: "dakota nvidia gaming sees the full choice",
			ref:  "ghcr.io/projectbluefin/dakota-nvidia-gaming",
			tag:  "testing",
			want: []Driver{DriverStandard, DriverNVIDIA},
		},
		{
			// The gaming images publish no latest, so neither flavour is
			// available on it and there is nothing to offer.
			name: "dakota gaming has no latest stream",
			ref:  "ghcr.io/projectbluefin/dakota-gaming",
			tag:  "latest",
			want: nil,
		},
		{
			name: "projectbluefin lts publishes no variants",
			ref:  "ghcr.io/projectbluefin/bluefin-lts",
			tag:  "lts",
			want: nil,
		},
		{
			name: "an nvidia host still sees the full choice",
			ref:  "ghcr.io/ublue-os/bluefin-nvidia",
			tag:  "latest",
			want: []Driver{DriverStandard, DriverNVIDIA, DriverNVIDIAOpen},
		},
		{name: "unknown image", ref: "ghcr.io/someone/custom", tag: "latest", want: nil},
		{name: "no tag", ref: "ghcr.io/ublue-os/bluefin", tag: "", want: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := AvailableDrivers(test.ref, test.tag)
			if len(got) == 0 && len(test.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("AvailableDrivers(%q, %q) = %v, want %v", test.ref, test.tag, got, test.want)
			}
		})
	}
}

func TestDriverTargetBuildsPublishedReferencesOnly(t *testing.T) {
	tests := []struct {
		name   string
		ref    string
		tag    string
		driver Driver
		want   string
		wantOK bool
	}{
		{
			name: "bluefin to nvidia", ref: "ghcr.io/ublue-os/bluefin", tag: "latest", driver: DriverNVIDIA,
			want: "ghcr.io/ublue-os/bluefin-nvidia:latest", wantOK: true,
		},
		{
			name: "bluefin to open modules", ref: "ghcr.io/ublue-os/bluefin", tag: "stable", driver: DriverNVIDIAOpen,
			want: "ghcr.io/ublue-os/bluefin-nvidia-open:stable", wantOK: true,
		},
		{
			name: "nvidia back to standard", ref: "ghcr.io/ublue-os/bluefin-nvidia", tag: "latest", driver: DriverStandard,
			want: "ghcr.io/ublue-os/bluefin:latest", wantOK: true,
		},
		{
			name: "dakota to nvidia", ref: "ghcr.io/projectbluefin/dakota", tag: "testing", driver: DriverNVIDIA,
			want: "ghcr.io/projectbluefin/dakota-nvidia:testing", wantOK: true,
		},
		{
			// The published name is dakota-nvidia-gaming. Appending the
			// driver suffix instead gives dakota-gaming-nvidia, a 404 that
			// would only be discovered as a failed switch on a real host.
			name: "dakota gaming to nvidia keeps the flavour last", ref: "ghcr.io/projectbluefin/dakota-gaming", tag: "stable", driver: DriverNVIDIA,
			want: "ghcr.io/projectbluefin/dakota-nvidia-gaming:stable", wantOK: true,
		},
		{
			name: "dakota nvidia gaming back to standard", ref: "ghcr.io/projectbluefin/dakota-nvidia-gaming", tag: "testing", driver: DriverStandard,
			want: "ghcr.io/projectbluefin/dakota-gaming:testing", wantOK: true,
		},
		{name: "dakota gaming has no latest stream", ref: "ghcr.io/projectbluefin/dakota-gaming", tag: "latest", driver: DriverNVIDIA, wantOK: false},
		{name: "dakota latest lost its nvidia variant", ref: "ghcr.io/projectbluefin/dakota", tag: "latest", driver: DriverNVIDIA, wantOK: false},
		// The reference that must never be produced.
		{name: "lts cannot reach nvidia", ref: "ghcr.io/ublue-os/bluefin", tag: "lts", driver: DriverNVIDIA, wantOK: false},
		{name: "already on the requested driver", ref: "ghcr.io/ublue-os/bluefin-nvidia", tag: "latest", driver: DriverNVIDIA, wantOK: false},
		{name: "unknown image", ref: "ghcr.io/someone/custom", tag: "latest", driver: DriverNVIDIA, wantOK: false},
		{name: "no tag", ref: "ghcr.io/ublue-os/bluefin", tag: "", driver: DriverNVIDIA, wantOK: false},
		// On a stream that does publish a driver variant, so the refusal is
		// about the open modules and not about the stream.
		{name: "dakota has no open modules", ref: "ghcr.io/projectbluefin/dakota", tag: "stable", driver: DriverNVIDIAOpen, wantOK: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := DriverTarget(test.ref, test.tag, test.driver)
			if ok != test.wantOK {
				t.Fatalf("DriverTarget(%q, %q, %q) ok = %v, want %v", test.ref, test.tag, test.driver, ok, test.wantOK)
			}
			if ok && got != test.want {
				t.Errorf("DriverTarget(%q, %q, %q) = %q, want %q", test.ref, test.tag, test.driver, got, test.want)
			}
		})
	}
}

func TestRecommendedDriverIsNarrowAndOneDirectional(t *testing.T) {
	tests := []struct {
		name       string
		info       Info
		hasNVIDIA  bool
		wantDriver Driver
		wantOffer  bool
	}{
		{
			name:      "nvidia card on the standard image is offered the driver",
			info:      Info{Name: "bluefin", Tag: "latest", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			hasNVIDIA: true, wantDriver: DriverNVIDIA, wantOffer: true,
		},
		{
			name:      "dakota with an nvidia card",
			info:      Info{Name: "dakota", Tag: "stable", Ref: "docker://ghcr.io/projectbluefin/dakota"},
			hasNVIDIA: true, wantDriver: DriverNVIDIA, wantOffer: true,
		},
		{
			// dakota-nvidia:latest stopped being published, so a latest
			// host has nowhere to go even with the card.
			name:      "dakota latest has no driver image to offer",
			info:      Info{Name: "dakota", Tag: "latest", Ref: "docker://ghcr.io/projectbluefin/dakota"},
			hasNVIDIA: true, wantDriver: DriverStandard, wantOffer: false,
		},
		{
			name:      "dakota gaming with an nvidia card",
			info:      Info{Name: "dakota-gaming", Tag: "stable", Ref: "docker://ghcr.io/projectbluefin/dakota-gaming"},
			hasNVIDIA: true, wantDriver: DriverNVIDIA, wantOffer: true,
		},
		{
			name:      "no nvidia card, no offer",
			info:      Info{Name: "bluefin", Tag: "latest", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			hasNVIDIA: false, wantDriver: DriverStandard, wantOffer: false,
		},
		{
			// Already on a driver image: nothing to offer, and ChairLift
			// must not choose between proprietary and open modules on the
			// user's behalf.
			name:      "already on the nvidia image",
			info:      Info{Name: "bluefin", Tag: "latest", Ref: "docker://ghcr.io/ublue-os/bluefin-nvidia"},
			hasNVIDIA: true, wantDriver: DriverNVIDIA, wantOffer: false,
		},
		{
			name:      "already on open modules",
			info:      Info{Name: "bluefin", Tag: "latest", Ref: "docker://ghcr.io/ublue-os/bluefin-nvidia-open"},
			hasNVIDIA: true, wantDriver: DriverNVIDIAOpen, wantOffer: false,
		},
		{
			// An LTS host with an NVIDIA card genuinely has nowhere to go.
			name:      "nvidia card on lts has no published driver image",
			info:      Info{Name: "bluefin", Tag: "lts", Ref: "docker://ghcr.io/ublue-os/bluefin"},
			hasNVIDIA: true, wantDriver: DriverStandard, wantOffer: false,
		},
		{
			// ChairLift never pushes a machine off a driver image.
			name:      "nvidia image without a card is left alone",
			info:      Info{Name: "bluefin", Tag: "latest", Ref: "docker://ghcr.io/ublue-os/bluefin-nvidia"},
			hasNVIDIA: false, wantDriver: DriverStandard, wantOffer: false,
		},
		{
			name:      "unknown image",
			info:      Info{Name: "custom", Tag: "latest", Ref: "docker://ghcr.io/someone/custom"},
			hasNVIDIA: true, wantDriver: DriverStandard, wantOffer: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			driver, offer := test.info.RecommendedDriver(test.hasNVIDIA)
			if offer != test.wantOffer {
				t.Fatalf("RecommendedDriver(%v) offer = %v, want %v", test.hasNVIDIA, offer, test.wantOffer)
			}
			if driver != test.wantDriver {
				t.Errorf("RecommendedDriver(%v) driver = %q, want %q", test.hasNVIDIA, driver, test.wantDriver)
			}
		})
	}
}

func TestDriverDisplayNamesAreDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, driver := range []Driver{DriverStandard, DriverNVIDIA, DriverNVIDIAOpen} {
		name := driver.DisplayName()
		if name == "" {
			t.Errorf("%q has no display name", driver)
		}
		if seen[name] {
			t.Errorf("display name %q is used by more than one driver", name)
		}
		seen[name] = true
	}
}

// Every stream a driver claims must also be a stream the base image is
// published for, or the table promises a switch from a tag that does not
// exist on the base.
func TestDriverTableIsInternallyConsistent(t *testing.T) {
	for base, entries := range imageDriverMap {
		t.Run(base, func(t *testing.T) {
			var standard []string
			for _, entry := range entries {
				if entry.driver == DriverStandard {
					standard = entry.streams
				}
			}
			if len(standard) == 0 {
				t.Fatalf("%s declares no standard driver streams", base)
			}
			for _, entry := range entries {
				if entry.driver == DriverStandard {
					continue
				}
				for _, stream := range entry.streams {
					if !slices.Contains(standard, stream) {
						t.Errorf("driver %q claims stream %q, which the base image does not publish", entry.driver, stream)
					}
				}
			}
		})
	}
}
