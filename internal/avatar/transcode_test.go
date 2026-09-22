package avatar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/draw"
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
// png.BestCompression, and remeasured once the centred-square crop landed:
// the katharina.webp full-size attempt encodes to 240,217 bytes and the 384
// rung to 147,466, so 184,000 sits 23% below the former and 25% above the
// latter — ordinary deflate drift between Go releases cannot flip either
// comparison unnoticed, and a change large enough to do so fails this test
// loudly and the constant gets remeasured.
func TestTranscodeWebPToPNGDownsamplesWhenTheFullSizeEncodeExceedsTheCeiling(t *testing.T) {
	const downsampleCeiling = 184_000 // straddles katharina's full-size (240,217) and 384-rung (147,466) attempts
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

// TestCentredSquareTakesTheLargestCentredSquare pins the crop geometry on its
// own: the square is always the shorter edge, it stays inside the source, and
// the leftover from an odd difference falls on the high side so the result
// cannot drift off centre by a whole pixel in either direction.
func TestCentredSquareTakesTheLargestCentredSquare(t *testing.T) {
	cases := []struct {
		name   string
		bounds image.Rectangle
		want   image.Rectangle
	}{
		{"already square", image.Rect(0, 0, 512, 512), image.Rect(0, 0, 512, 512)},
		{"wide even difference", image.Rect(0, 0, 1392, 1070), image.Rect(161, 0, 1231, 1070)},
		{"wide even difference, sample dimensions", image.Rect(0, 0, 440, 412), image.Rect(14, 0, 426, 412)},
		{"wide odd difference", image.Rect(0, 0, 441, 412), image.Rect(14, 0, 426, 412)},
		{"tall", image.Rect(0, 0, 64, 96), image.Rect(0, 16, 64, 80)},
		{"non-zero origin", image.Rect(10, 20, 110, 80), image.Rect(30, 20, 90, 80)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := centredSquare(tc.bounds)
			if got != tc.want {
				t.Fatalf("centredSquare(%v) = %v, want %v", tc.bounds, got, tc.want)
			}
			if got.Dx() != got.Dy() {
				t.Errorf("result %v is not square", got)
			}
			if !got.In(tc.bounds) {
				t.Errorf("result %v escapes the source bounds %v", got, tc.bounds)
			}
		})
	}
}

// TestTranscodeWebPToPNGCropsNonSquareSourcesInsteadOfStretching is the
// anti-distortion pin. dakota.webp is 1392x1070, so scaling its full bounds
// into a square would compress it horizontally by 23%. The result must match
// a scale of the centred square computed independently here, and must not
// match a scale of the full bounds — the second half is what actually fails
// if the crop is dropped and the stretch comes back.
func TestTranscodeWebPToPNGCropsNonSquareSourcesInsteadOfStretching(t *testing.T) {
	payload := readWebPSample(t, "dakota.webp")
	src, err := webp.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decoding the sample directly: %v", err)
	}
	if src.Bounds().Dx() == src.Bounds().Dy() {
		t.Fatalf("sample bounds %v are square; this case can no longer tell a crop from a stretch", src.Bounds())
	}
	out, err := TranscodeWebPToPNG(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("TranscodeWebPToPNG() error = %v", err)
	}
	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decoding the result as PNG: %v", err)
	}
	scaled := func(from image.Rectangle) *image.NRGBA {
		dst := image.NewNRGBA(image.Rect(0, 0, AvatarSize, AvatarSize))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, from, draw.Src, nil)
		return dst
	}
	b := src.Bounds()
	cropped := scaled(image.Rect(b.Min.X+(b.Dx()-b.Dy())/2, b.Min.Y, b.Min.X+(b.Dx()-b.Dy())/2+b.Dy(), b.Max.Y))
	if !sameNRGBA(got, cropped) {
		t.Error("result does not match a scale of the centred square of the source")
	}
	if sameNRGBA(got, scaled(b)) {
		t.Error("result matches a scale of the full source bounds; the non-square source was stretched, not cropped")
	}
}

// sameNRGBA reports whether img covers exactly reference's bounds and carries
// the same colour at every pixel.
func sameNRGBA(img image.Image, reference *image.NRGBA) bool {
	if img.Bounds() != reference.Bounds() {
		return false
	}
	for y := reference.Bounds().Min.Y; y < reference.Bounds().Max.Y; y++ {
		for x := reference.Bounds().Min.X; x < reference.Bounds().Max.X; x++ {
			r1, g1, b1, a1 := img.At(x, y).RGBA()
			r2, g2, b2, a2 := reference.At(x, y).RGBA()
			if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
				return false
			}
		}
	}
	return true
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
