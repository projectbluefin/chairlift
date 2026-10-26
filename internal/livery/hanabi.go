package livery

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// HanabiSchema is the extension's actual schema, not its extension UUID.
const HanabiSchema = "io.github.jeffshee.hanabi-extension"

// HanabiState is an observation of support and the configured video path.
// A path is not evidence that the extension is enabled or rendering a video.
type HanabiState struct {
	Available bool
	Path      string
}

// LoadHanabi must run on a worker. A failed probe is an error, never absence.
// No in-process GSettings object is constructed for this optional schema.
func LoadHanabi(ctx context.Context) (HanabiState, error) {
	if detectDesktop() != deskenv.GNOME {
		return HanabiState{}, nil
	}
	out, err := runCommand(ctx, "gsettings", "list-schemas")
	if err != nil {
		return HanabiState{}, fmt.Errorf("checking video wallpaper support: %w: %s", err, strings.TrimSpace(out))
	}
	found := false
	for _, schema := range strings.Fields(out) {
		if schema == HanabiSchema {
			found = true
			break
		}
	}
	if !found {
		return HanabiState{}, nil
	}
	out, err = runCommand(ctx, "gsettings", "get", HanabiSchema, "video-path")
	if err != nil {
		return HanabiState{}, fmt.Errorf("reading video wallpaper: %w: %s", err, strings.TrimSpace(out))
	}
	value := strings.TrimSpace(out)
	if len(value) < 2 || (value[0] != '\'' && value[0] != '"') || value[len(value)-1] != value[0] {
		return HanabiState{}, fmt.Errorf("reading video wallpaper: invalid string response")
	}
	return HanabiState{Available: true, Path: unquote(value)}, nil
}

// ValidateHanabiVideo accepts local, readable, nonempty regular video files.
// Container signatures reject renamed images; decoding remains Hanabi's job.
func ValidateHanabiVideo(path string) error {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return fmt.Errorf("choose a local video file with an absolute path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking video: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("choose a regular video file")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening video: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("choose a regular video file")
	}
	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("reading video: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(path))
	valid := (ext == ".mp4" || ext == ".mov" || ext == ".m4v") && string(header[4:8]) == "ftyp" || ext == ".webm" && string(header[:4]) == "\x1a\x45\xdf\xa3" || ext == ".ogv" && string(header[:4]) == "OggS"
	if !valid {
		return fmt.Errorf("choose an MP4, MOV, M4V, WebM or OGV video file")
	}
	return nil
}

// ApplyHanabi rechecks support and validates at the write boundary. The caller
// invokes it only after Apply, never on chooser selection. Failed writes or
// readbacks preserve previous confirmed state. Preview never writes.
func ApplyHanabi(ctx context.Context, path string, previous HanabiState) (state HanabiState, preview bool, err error) {
	if err = ValidateHanabiVideo(path); err != nil {
		return previous, false, err
	}
	observed, err := LoadHanabi(ctx)
	if err != nil {
		return previous, false, err
	}
	if !observed.Available {
		return previous, false, fmt.Errorf("video wallpaper support is no longer available")
	}
	if dryrun.Enabled() {
		return previous, true, nil
	}
	out, err := runCommand(ctx, "gsettings", "set", HanabiSchema, "video-path", strconv.Quote(path))
	if err != nil {
		return previous, false, fmt.Errorf("saving video path: %w: %s", err, strings.TrimSpace(out))
	}
	observed, err = LoadHanabi(ctx)
	if err != nil {
		return previous, false, fmt.Errorf("video path was written but could not be verified: %w", err)
	}
	if !observed.Available || observed.Path != path {
		return previous, false, fmt.Errorf("video path was written but the requested value was not confirmed")
	}
	return observed, false, nil
}
