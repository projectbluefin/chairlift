package livery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// NativeBackgroundSchema is the GNOME schema that owns the desktop picture.
// ChairLift does not declare it — it is GNOME's — so writing it requires the
// schema to be installed on the host. That is exactly why selection is
// offered only where NativeBackgroundAvailable reports true: a schema that is
// absent would turn a selection into a silent no-op.
const NativeBackgroundSchema = "org.gnome.desktop.background"

const (
	// pictureURIKey and pictureURIDarkKey are GNOME's light/dark pairs of
	// the same key. Both must move together, or a user whose session flips
	// between colour schemes would see one state updated and the other not.
	pictureURIKey     = "picture-uri"
	pictureURIDarkKey = "picture-uri-dark"
)

// NativeBackgroundAvailable reports whether the session is GNOME and GNOME owns
// the desktop-background schema on this host. Both are required: other
// desktops (Plasma on Fedora-based images, for one) commonly ship
// gsettings-desktop-schemas, so the schema alone would offer a write that
// changes nothing on screen. Either missing returns false, so the page offers
// wallpaper selection only where it can actually apply the picture. A failure
// to probe — a hung or broken settings tool — is returned, not swallowed as a
// false, so callers can report "unknown" rather than hiding the feature as
// unsupported (per #230: detection failures are errors, not "unsupported").
func NativeBackgroundAvailable(ctx context.Context) (bool, error) {
	if detectDesktop() != deskenv.GNOME {
		return false, nil
	}
	_, err := gsettingsGet(ctx, NativeBackgroundSchema, pictureURIKey)
	if errors.Is(err, ErrExtensionMissing) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("livery: probing %s: %w", NativeBackgroundSchema, err)
	}
	return true, nil
}

// ErrBackgroundSchemaMissing reports a failed wallpaper write because GNOME's
// desktop-background schema is absent. gsettingsSet maps a missing schema to
// ErrExtensionMissing, whose text blames a GNOME Shell extension; wrapping it
// here keeps the journal naming the wallpaper feature instead.
var ErrBackgroundSchemaMissing = errors.New("livery: GNOME desktop-background schema is not installed")

// wrapBackgroundSchemaError renames gsettingsSet's schema-missing error — which
// blames an unrelated GNOME Shell extension — to a wallpaper-specific message
// so the journal stays truthful. A nil error passes through; any other failure
// (a hung or otherwise broken tool) is returned unchanged.
func wrapBackgroundSchemaError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrExtensionMissing) {
		return fmt.Errorf("%w", ErrBackgroundSchemaMissing)
	}
	return err
}

// ApplyNativeBackground writes file to GNOME's picture-uri and picture-uri-dark
// so the selected collection image becomes the desktop picture in both colour
// schemes. It writes neither key when dry-run is enabled. A missing schema
// surfaces as ErrBackgroundSchemaMissing: gsettingsSet maps a missing schema to
// ErrExtensionMissing, whose text blames a GNOME Shell extension, so the write
// path renames it rather than logging a failure against an unrelated feature.
//
// Both keys hold URIs, not paths: GNOME resolves them with
// g_filename_from_uri, which rejects a bare path, so the absolute path is
// written as a file:// URI quoted as a GVariant string.
func ApplyNativeBackground(ctx context.Context, file string) error {
	value := strconv.Quote(BackgroundURI(file))
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set %s %s and %s %s to %s",
			NativeBackgroundSchema, pictureURIKey,
			NativeBackgroundSchema, pictureURIDarkKey, value)
		return nil
	}
	if err := wrapBackgroundSchemaError(gsettingsSet(ctx, NativeBackgroundSchema, pictureURIKey, value)); err != nil {
		return err
	}
	return wrapBackgroundSchemaError(gsettingsSet(ctx, NativeBackgroundSchema, pictureURIDarkKey, value))
}

// BackgroundURI returns the file:// URI GNOME's background keys expect for an
// absolute path, percent-encoding any character a URI cannot carry verbatim.
func BackgroundURI(file string) string {
	return (&url.URL{Scheme: "file", Path: filepath.Clean(file)}).String()
}

// SelectWallpaper applies one channel image to the desktop and records the
// channel and image, so the headless rotation pass (RotateWallpaper) knows
// which channel to advance and where it left off. Under dry-run nothing is
// written: ApplyNativeBackground only logs, and the choice is not persisted.
func SelectWallpaper(ctx context.Context, channelID, imageID, file string) error {
	if err := ApplyNativeBackground(ctx, file); err != nil {
		return err
	}
	if dryrun.Enabled() {
		return nil
	}
	if err := SetString(ctx, KeyWallpaperChannel, channelID); err != nil {
		return err
	}
	return SetString(ctx, KeyWallpaperImage, imageID)
}
