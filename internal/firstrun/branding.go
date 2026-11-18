package firstrun

import (
	"path"
	"strings"

	"github.com/projectbluefin/chairlift/internal/imageinfo"
)

// ImageBrand names the image's embedded wordmarks and their accessible label.
// This display choice grants no support for privileged image operations.
type ImageBrand struct {
	Name       string
	LightAsset string
	DarkAsset  string
}

// BrandForImage selects branding from the descriptor's image name, or the
// reference's repository name when the descriptor omits it. Hyphenated variants
// share their family's artwork; unknown images retain the Bluefin wordmark.
func BrandForImage(info imageinfo.Info) ImageBrand {
	name := info.Name
	if name == "" {
		name = path.Base(info.CleanRef())
		name, _, _ = strings.Cut(name, "@")
	}
	name = strings.ToLower(name)
	switch {
	case name == "bazzite" || strings.HasPrefix(name, "bazzite-"):
		return ImageBrand{
			Name:       "Bazzite",
			LightAsset: "assets/bazzite-wordmark-light.svg",
			DarkAsset:  "assets/bazzite-wordmark-dark.svg",
		}
	case name == "aurora" || strings.HasPrefix(name, "aurora-"):
		return ImageBrand{
			Name:       "Aurora",
			LightAsset: "assets/aurora-wordmark.svg",
			DarkAsset:  "assets/aurora-wordmark.svg",
		}
	default:
		return ImageBrand{
			Name:       "Bluefin",
			LightAsset: AssetWordmarkLight,
			DarkAsset:  AssetWordmarkDark,
		}
	}
}
