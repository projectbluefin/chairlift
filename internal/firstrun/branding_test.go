package firstrun

import (
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
)

func TestBrandForImageSelection(t *testing.T) {
	tests := []struct {
		name      string
		info      imageinfo.Info
		wantName  string
		wantLight string
		wantDark  string
	}{
		{
			name:      "bazzite by name",
			info:      imageinfo.Info{Name: "bazzite"},
			wantName:  "Bazzite",
			wantLight: "assets/bazzite-wordmark-light.svg",
			wantDark:  "assets/bazzite-wordmark-dark.svg",
		},
		{
			name:      "bazzite-deck by name",
			info:      imageinfo.Info{Name: "bazzite-deck"},
			wantName:  "Bazzite",
			wantLight: "assets/bazzite-wordmark-light.svg",
			wantDark:  "assets/bazzite-wordmark-dark.svg",
		},
		{
			name:      "bazzite by ref fallback",
			info:      imageinfo.Info{Ref: "ostree-image-signed:docker://ghcr.io/ublue-os/bazzite-nvidia:stable"},
			wantName:  "Bazzite",
			wantLight: "assets/bazzite-wordmark-light.svg",
			wantDark:  "assets/bazzite-wordmark-dark.svg",
		},
		{
			name:      "aurora by name",
			info:      imageinfo.Info{Name: "aurora"},
			wantName:  "Aurora",
			wantLight: "assets/aurora-wordmark.svg",
			wantDark:  "assets/aurora-wordmark.svg",
		},
		{
			name:      "aurora-dx by name",
			info:      imageinfo.Info{Name: "aurora-dx"},
			wantName:  "Aurora",
			wantLight: "assets/aurora-wordmark.svg",
			wantDark:  "assets/aurora-wordmark.svg",
		},
		{
			name:      "aurora by ref fallback",
			info:      imageinfo.Info{Ref: "docker://ghcr.io/ublue-os/aurora:stable"},
			wantName:  "Aurora",
			wantLight: "assets/aurora-wordmark.svg",
			wantDark:  "assets/aurora-wordmark.svg",
		},
		{
			name:      "bluefin default",
			info:      imageinfo.Info{Name: "bluefin"},
			wantName:  "Bluefin",
			wantLight: AssetWordmarkLight,
			wantDark:  AssetWordmarkDark,
		},
		{
			name:      "dakota default",
			info:      imageinfo.Info{Name: "dakota"},
			wantName:  "Bluefin",
			wantLight: AssetWordmarkLight,
			wantDark:  AssetWordmarkDark,
		},
		{
			name:      "empty descriptor default",
			info:      imageinfo.Info{},
			wantName:  "Bluefin",
			wantLight: AssetWordmarkLight,
			wantDark:  AssetWordmarkDark,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BrandForImage(tt.info)
			if got.Name != tt.wantName {
				t.Errorf("BrandForImage().Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.LightAsset != tt.wantLight {
				t.Errorf("BrandForImage().LightAsset = %q, want %q", got.LightAsset, tt.wantLight)
			}
			if got.DarkAsset != tt.wantDark {
				t.Errorf("BrandForImage().DarkAsset = %q, want %q", got.DarkAsset, tt.wantDark)
			}
			lightData, err := Asset(got.LightAsset)
			if err != nil || len(lightData) == 0 || !strings.Contains(string(lightData), "<svg") {
				t.Fatalf("Asset(%q) unusable SVG: err=%v, len=%d", got.LightAsset, err, len(lightData))
			}
			darkData, err := Asset(got.DarkAsset)
			if err != nil || len(darkData) == 0 || !strings.Contains(string(darkData), "<svg") {
				t.Fatalf("Asset(%q) unusable SVG: err=%v, len=%d", got.DarkAsset, err, len(darkData))
			}
		})
	}
}
