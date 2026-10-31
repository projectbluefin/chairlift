package livery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestHanabiSupport(t *testing.T) {
	for _, tc := range []struct {
		name, schemas string
		desktop       deskenv.Desktop
		fail          bool
		available     bool
		wantErr       bool
	}{
		{"supported", "org.example.Other\n" + HanabiSchema + "\n", deskenv.GNOME, false, true, false},
		{"absent", "org.example.Other", deskenv.GNOME, false, false, false},
		{"uuid is not schema", "hanabi-extension@jeffshee.github.io", deskenv.GNOME, false, false, false},
		{"exact schema only", HanabiSchema + ".extra", deskenv.GNOME, false, false, false},
		{"probe failure", "", deskenv.GNOME, true, false, true},
		{"KDE with schema", HanabiSchema, deskenv.KDE, false, false, false},
		{"unknown desktop", HanabiSchema, deskenv.Unknown, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useDesktop(t, tc.desktop)
			f := newFakeCommands(t)
			f.reply["gsettings list-schemas"] = tc.schemas
			f.reply["gsettings get "+HanabiSchema+" video-path"] = "'/old/video.mp4'"
			if tc.fail {
				f.fail["gsettings list-schemas"] = errors.New("probe failed")
			}
			state, err := LoadHanabi(context.Background())
			if state.Available != tc.available || (err != nil) != tc.wantErr {
				t.Fatalf("state=%+v err=%v", state, err)
			}
			if tc.desktop != deskenv.GNOME && len(f.calls) != 0 {
				t.Fatalf("unsupported desktop ran %v", f.calls)
			}
			if !tc.available && f.sawPrefix("gsettings get") {
				t.Fatalf("read absent schema: %v", f.calls)
			}
			if f.sawPrefix("gsettings set") {
				t.Fatal("load wrote settings")
			}
		})
	}
}

func hanabiVideo(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHanabiVideoValidation(t *testing.T) {
	mp4 := []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00")
	for _, tc := range []struct {
		name  string
		path  string
		valid bool
	}{
		{"mp4", hanabiVideo(t, "clip.mp4", mp4), true},
		{"uppercase", hanabiVideo(t, "clip.MP4", mp4), true},
		{"webm", hanabiVideo(t, "clip.webm", []byte("\x1a\x45\xdf\xa3abcdefgh")), true},
		{"ogv", hanabiVideo(t, "clip.ogv", []byte("OggSabcdefgh")), true},
		{"renamed image", hanabiVideo(t, "fake.mp4", []byte("PNG image content")), false},
		{"unknown type", hanabiVideo(t, "clip.txt", mp4), false},
		{"empty", hanabiVideo(t, "clip.mp4", nil), false},
		{"missing", filepath.Join(t.TempDir(), "gone.mp4"), false},
		{"directory", t.TempDir(), false},
		{"relative", "clip.mp4", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateHanabiVideo(tc.path); (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}

func TestHanabiApplyOutcomes(t *testing.T) {
	path := hanabiVideo(t, "a 'quoted' \\ video.mp4", []byte("\x00\x00\x00\x18ftypisom1234"))
	previous := HanabiState{Available: true, Path: "/old.mp4"}
	for _, mode := range []string{"confirmed", "preview", "unsupported", "probe error", "invalid file", "write rejected", "refresh error", "readback mismatch", "malformed readback"} {
		t.Run(mode, func(t *testing.T) {
			dryrun.Set(mode == "preview")
			t.Cleanup(func() { dryrun.Set(false) })
			f := newFakeCommands(t)
			input := path
			f.reply["gsettings list-schemas"] = HanabiSchema
			f.reply["gsettings get "+HanabiSchema+" video-path"] = strconv.Quote(path)
			switch mode {
			case "unsupported":
				f.reply["gsettings list-schemas"] = ""
			case "probe error":
				f.fail["gsettings list-schemas"] = errors.New("failed")
			case "invalid file":
				input = "/missing.mp4"
			case "write rejected":
				f.fail["gsettings set"] = errors.New("not writable")
			case "readback mismatch":
				f.reply["gsettings get "+HanabiSchema+" video-path"] = "'/different.mp4'"
			case "malformed readback":
				f.reply["gsettings get "+HanabiSchema+" video-path"] = "not a string"
			}
			// A failed post-write refresh is distinct from a failed initial probe.
			if mode == "refresh error" {
				underlying := runCommand
				wrote := false
				runCommand = func(ctx context.Context, name string, args ...string) (string, error) {
					if wrote && args[0] == "list-schemas" {
						return "", errors.New("refresh failed")
					}
					out, err := underlying(ctx, name, args...)
					if args[0] == "set" {
						wrote = true
					}
					return out, err
				}
			}
			state, preview, err := ApplyHanabi(context.Background(), input, previous)
			success := mode == "confirmed"
			if (err == nil) != (success || mode == "preview") || preview != (mode == "preview") {
				t.Fatalf("state=%+v preview=%v err=%v", state, preview, err)
			}
			if success {
				if state.Path != path {
					t.Fatalf("path=%q", state.Path)
				}
			} else if state != previous {
				t.Fatalf("lost confirmed state: %+v", state)
			}
			shouldWrite := success || mode == "write rejected" || mode == "refresh error" || mode == "readback mismatch"
			if f.sawPrefix("gsettings set") != shouldWrite {
				t.Fatalf("unexpected commands: %v", f.calls)
			}
			for _, call := range f.calls {
				if strings.Contains(call, " set ") && call != "gsettings set "+HanabiSchema+" video-path "+strconv.Quote(path) {
					t.Fatalf("unrelated mutation: %s", call)
				}
			}
		})
	}
}
