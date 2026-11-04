package avatar

import (
	"embed"
	"errors"
	"fmt"
	"path"
	"strings"
)

// embeddedPreviewFS holds one precomputed preview PNG per catalog entry, keyed
// by the artwork's base name (bluefin.webp -> bluefin.png).
//
// The PNGs are committed data, not generated build output. Regenerate affected
// previews when catalog artwork or preview transcoding changes.
//
//go:embed assets/preview/*.png
var embeddedPreviewFS embed.FS

// ErrPreviewNotFound reports a catalog entry the binary carries no preview PNG
// for.
var ErrPreviewNotFound = errors.New("avatar: no embedded preview for that id")

// PreviewPNG returns the precomputed preview-sized PNG for one catalog entry,
// ready to hand to a texture with no transcode.
func PreviewPNG(id string) ([]byte, error) {
	entry, ok := Lookup(id)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrAvatarNotFound, id)
	}
	data, ok := embeddedPreview(entry.Path)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrPreviewNotFound, id)
	}
	return data, nil
}

// embeddedPreview returns the preview PNG for one catalog Path, keyed by the
// artwork's base name with its extension swapped for .png.
func embeddedPreview(artworkPath string) ([]byte, bool) {
	name := strings.TrimSuffix(path.Base(artworkPath), ".webp") + ".png"
	data, err := embeddedPreviewFS.ReadFile("assets/preview/" + name)
	if err != nil {
		return nil, false
	}
	return data, true
}
