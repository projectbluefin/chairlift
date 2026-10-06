package avatar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/webp"
)

// readWebPSample loads one of the package's frozen WebP payloads from
// testdata/, relative to the package directory the test binary runs in.
func readWebPSample(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading testdata sample %s: %v", name, err)
	}
	return payload
}

// TestTranscodeWebPToPNGScalesSamplePayloadsToSquarePNGWithinCeiling pins the
// happy path across the three sample WebP payloads: each decodes, comes back
// as a valid 512x512 PNG, and fits under the issue's 1 MiB ceiling. The
// fourcc assertion proves each case still carries the format it claims —
// VP8L (the issue's named codec), extended VP8X with alpha, and bare lossy
// VP8 — so a fixture swap cannot silently turn the table into three copies
// of one format.
func TestTranscodeWebPToPNGScalesSamplePayloadsToSquarePNGWithinCeiling(t *testing.T) {
	const oneMebibyte = 1_048_576 // the issue's ceiling, pinned independently of MaxPNGBytes
	if MaxPNGBytes != oneMebibyte {
		t.Fatalf("MaxPNGBytes = %d, want %d", MaxPNGBytes, oneMebibyte)
	}
	cases := []struct {
		file   string
		fourcc string
	}{
		{"katharina.webp", "VP8L"},      // real catalog artwork (characters/header/katharina.webp), lossless
		{"dakota.webp", "VP8X"},         // real catalog artwork (characters/dakota.webp), extended with alpha
		{"gradient-lossy.webp", "VP8 "}, // synthetic lossy sample
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			payload := readWebPSample(t, tc.file)
			if len(payload) < 16 {
				t.Fatalf("sample is %d bytes, too short to carry a RIFF fourcc", len(payload))
			}
			if got := string(payload[12:16]); got != tc.fourcc {
				t.Fatalf("fourcc at byte 12 = %q, want %q; the sample no longer exercises the format this case names", got, tc.fourcc)
			}
			out, err := TranscodeWebPToPNG(bytes.NewReader(payload))
			if err != nil {
				t.Fatalf("TranscodeWebPToPNG() error = %v", err)
			}
			if len(out) > oneMebibyte {
				t.Errorf("encoded PNG is %d bytes, over the %d-byte ceiling", len(out), oneMebibyte)
			}
			img, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatalf("decoding the result as PNG: %v", err)
			}
			if want := image.Rect(0, 0, AvatarSize, AvatarSize); img.Bounds() != want {
				t.Errorf("bounds = %v, want %v", img.Bounds(), want)
			}
		})
	}
}

// TestTranscodeWebPToPNGRejectsPayloadsThatDoNotDecode covers the decode
// failure outcome: no bytes come back, the error is a decode failure rather
// than the ceiling sentinel, and each shape is one a fetch of the catalog's
// artwork could actually produce (empty body, non-WebP content, a RIFF
// wrapper around garbage, a truncated read).
func TestTranscodeWebPToPNGRejectsPayloadsThatDoNotDecode(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"empty", nil},
		{"not a webp", []byte("this is not a WebP image")},
		{"riff header with junk body", []byte("RIFF\x10\x00\x00\x00WEBPVP8L junkjunkjunkjunk")},
		{"truncated sample", readWebPSample(t, "katharina.webp")[:64]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := TranscodeWebPToPNG(bytes.NewReader(tc.payload))
			if err == nil {
				t.Fatal("TranscodeWebPToPNG() error = nil, want a decode failure")
			}
			if errors.Is(err, ErrAvatarTooLarge) {
				t.Errorf("error = %v; a payload that does not decode is a decode failure, not a ceiling failure", err)
			}
			if out != nil {
				t.Errorf("bytes = %d long, want nil on failure", len(out))
			}
		})
	}
}

// TestTranscodeWebPToPNGReportsTooLargeWhenNoRungFitsTheCeiling drives the
// explicit ErrAvatarTooLarge outcome. A ceiling of zero can never be met, so
// the whole 512 -> 384 -> 256 ladder is attempted before the sentinel comes
// back — this is the reduction-failed path the issue requires, and the seam
// that keeps it testable without a multi-megabyte fixture.
func TestTranscodeWebPToPNGReportsTooLargeWhenNoRungFitsTheCeiling(t *testing.T) {
	payload := readWebPSample(t, "gradient-lossy.webp")
	out, err := transcodeWebPToPNG(bytes.NewReader(payload), 0)
	if !errors.Is(err, ErrAvatarTooLarge) {
		t.Fatalf("error = %v, want ErrAvatarTooLarge", err)
	}
	if out != nil {
		t.Errorf("bytes = %d long, want nil when every rung exceeds the ceiling", len(out))
	}
}

// TestTranscodeWebPToPNGDownsamplesWhenTheFullSizeEncodeExceedsTheCeiling
// proves the ladder actually downsamples instead of failing at full size: a
// ceiling below the full-size attempt but above the 384 rung must come back
// as a valid 384x384 PNG within budget.
//
// downsampleCeiling was measured through transcodeWebPToPNG itself with
// png.BestCompression, and remeasured once the circle fit landed: the
// katharina.webp full-size attempt encodes to 132,282 bytes and the 384
// rung to 79,753, so 105,000 sits 21% below the former and 32% above the
// latter — ordinary deflate drift between Go releases cannot flip either
// comparison unnoticed, and a change large enough to do so fails this test
// loudly and the constant gets remeasured.
func TestTranscodeWebPToPNGDownsamplesWhenTheFullSizeEncodeExceedsTheCeiling(t *testing.T) {
	const downsampleCeiling = 105_000 // straddles katharina's full-size (132,282) and 384-rung (79,753) attempts
	payload := readWebPSample(t, "katharina.webp")
	out, err := transcodeWebPToPNG(bytes.NewReader(payload), downsampleCeiling)
	if err != nil {
		t.Fatalf("transcodeWebPToPNG() error = %v", err)
	}
	if len(out) > downsampleCeiling {
		t.Errorf("encoded PNG is %d bytes, over the %d-byte test ceiling", len(out), downsampleCeiling)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decoding the downsampled result as PNG: %v", err)
	}
	if want := image.Rect(0, 0, AvatarSize*3/4, AvatarSize*3/4); img.Bounds() != want {
		t.Errorf("bounds = %v, want %v — the full-size rung should have been rejected and the 384 rung used", img.Bounds(), want)
	}
}

// TestFitInCircleKeepsEveryCornerInsideTheCircle pins the geometry: the
// destination keeps the content's aspect ratio, is centred, and its diagonal
// never exceeds the canvas edge, so a circular clip cannot cut a corner.
func TestFitInCircleKeepsEveryCornerInsideTheCircle(t *testing.T) {
	for _, content := range []image.Rectangle{
		image.Rect(0, 0, 512, 512),
		image.Rect(0, 0, 1392, 1070),
		image.Rect(0, 0, 64, 96),
		image.Rect(10, 20, 110, 80),
		image.Rect(0, 0, 1000, 10),
		image.Rect(0, 0, 1000, 1), // a sliver must not fit to an empty rectangle
	} {
		for _, edge := range []int{AvatarSize, AvatarSize / 2} {
			got := fitInCircle(content, edge)
			if got.Empty() {
				t.Errorf("fitInCircle(%v, %d) = %v is empty; the avatar would be blank", content, edge, got)
			}
			if d := math.Hypot(float64(got.Dx()), float64(got.Dy())); d > float64(edge) {
				t.Errorf("fitInCircle(%v, %d) = %v, diagonal %.1f exceeds the %d circle", content, edge, got, d, edge)
			}
			want := float64(content.Dx()) / float64(content.Dy())
			if ratio := float64(got.Dx()) / float64(got.Dy()); math.Abs(ratio-want)/want > 0.02 && got.Dy() > 50 {
				t.Errorf("fitInCircle(%v, %d) = %v, aspect %.3f, want %.3f", content, edge, got, ratio, want)
			}
			if l, r := got.Min.X, edge-got.Max.X; l-r > 1 || r-l > 1 {
				t.Errorf("fitInCircle(%v, %d) = %v is not horizontally centred", content, edge, got)
			}
			if top, bottom := got.Min.Y, edge-got.Max.Y; top-bottom > 1 || bottom-top > 1 {
				t.Errorf("fitInCircle(%v, %d) = %v is not vertically centred", content, edge, got)
			}
		}
	}
}

// TestTranscodeWebPToPNGKeepsTheWholeCharacterInsideTheAvatarCircle is the
// regression for a profile picture whose head and tail were cut off: GNOME
// clips avatars to a circle, so no visible pixel of a transcoded catalog
// character may fall outside it, and the character must not be stretched.
func TestTranscodeWebPToPNGKeepsTheWholeCharacterInsideTheAvatarCircle(t *testing.T) {
	payload := readWebPSample(t, "dakota.webp")
	src, err := webp.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decoding the sample directly: %v", err)
	}
	out, err := TranscodeWebPToPNG(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("TranscodeWebPToPNG() error = %v", err)
	}
	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decoding the result as PNG: %v", err)
	}
	edge := got.Bounds().Dx()
	centre, radius := float64(edge)/2, float64(edge)/2+1
	for y := range edge {
		for x := range edge {
			if _, _, _, a := got.At(x, y).RGBA(); a != 0 && math.Hypot(float64(x)+0.5-centre, float64(y)+0.5-centre) > radius {
				t.Fatalf("visible pixel at (%d,%d) lies outside the avatar circle", x, y)
			}
		}
	}
	in, outBox := visibleBounds(src), visibleBounds(got)
	want := float64(in.Dx()) / float64(in.Dy())
	if ratio := float64(outBox.Dx()) / float64(outBox.Dy()); math.Abs(ratio-want)/want > 0.03 {
		t.Errorf("visible aspect %.3f, source %.3f: the character was stretched", ratio, want)
	}
}

// vp8lHeaderOnly builds a RIFF/WEBP container holding one VP8L chunk whose
// 5-byte header declares w x h and whose image data is absent. It is the
// decompression-bomb shape in miniature: a few dozen bytes on the wire that
// webp.DecodeConfig reads as a frame of w*h pixels, which is why the bound
// has to be enforced from the header rather than after webp.Decode has
// already allocated the frame.
func vp8lHeaderOnly(w, h int) []byte {
	chunk := []byte{'V', 'P', '8', 'L', 0, 0, 0, 0, 0x2f, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(chunk[4:], 5)
	binary.LittleEndian.PutUint32(chunk[9:], uint32(w-1)|uint32(h-1)<<14)
	payload := []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}
	binary.LittleEndian.PutUint32(payload[4:], uint32(4+len(chunk)))
	return append(payload, chunk...)
}

// TestTranscodeWebPToPNGRejectsSourcesOverTheInputBounds covers the two
// pre-decode refusals: an encoded payload past MaxWebPBytes, and a header
// declaring more pixels than MaxSourcePixels. Both must come back as
// ErrSourceTooLarge and no bytes — neither is a ceiling failure, and neither
// may be reported only after the frame has been allocated.
func TestTranscodeWebPToPNGRejectsSourcesOverTheInputBounds(t *testing.T) {
	oversizedHeader := vp8lHeaderOnly(16383, 16383)
	if config, err := webp.DecodeConfig(bytes.NewReader(oversizedHeader)); err != nil || int64(config.Width)*int64(config.Height) <= MaxSourcePixels {
		t.Fatalf("crafted header decoded as %+v (err %v); it no longer declares a frame over the %d-pixel bound", config, err, MaxSourcePixels)
	}
	cases := []struct {
		name    string
		payload []byte
	}{
		{"payload over the byte bound", append(readWebPSample(t, "katharina.webp"), make([]byte, MaxWebPBytes)...)},
		{"header over the pixel bound", oversizedHeader},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := TranscodeWebPToPNG(bytes.NewReader(tc.payload))
			if !errors.Is(err, ErrSourceTooLarge) {
				t.Fatalf("error = %v, want ErrSourceTooLarge", err)
			}
			if out != nil {
				t.Errorf("bytes = %d long, want nil when the source is refused", len(out))
			}
		})
	}
}

// TestTranscodeWebPToPNGAcceptsAPayloadExactlyOnTheByteBound keeps the input
// bound inclusive: MaxWebPBytes is the largest accepted size, not the first
// rejected one. The sample is padded to exactly the bound with trailing bytes
// the RIFF container ignores.
func TestTranscodeWebPToPNGAcceptsAPayloadExactlyOnTheByteBound(t *testing.T) {
	payload := readWebPSample(t, "gradient-lossy.webp")
	payload = append(payload, make([]byte, MaxWebPBytes-len(payload))...)
	if len(payload) != MaxWebPBytes {
		t.Fatalf("padded payload is %d bytes, want exactly %d", len(payload), MaxWebPBytes)
	}
	if _, err := TranscodeWebPToPNG(bytes.NewReader(payload)); err != nil {
		t.Fatalf("TranscodeWebPToPNG() error = %v, want a payload sitting on the bound to be accepted", err)
	}
}
