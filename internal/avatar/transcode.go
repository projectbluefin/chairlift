package avatar

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// AvatarSize is the edge length, in pixels, of the square canvas every
// transcoded avatar PNG is scaled to.
const AvatarSize = 512

// MaxPNGBytes is the byte ceiling on one encoded avatar PNG: 1 MiB
// (1,048,576 bytes).
const MaxPNGBytes = 1 << 20

// MaxWebPBytes is the byte ceiling on the encoded WebP handed to
// TranscodeWebPToPNG: 4 MiB. The catalog's own artwork is two orders of
// magnitude below it (the largest character illustration is ~120 KiB), so
// the bound only ever fires on input the avatar pipeline was not meant to
// carry.
const MaxWebPBytes = 4 << 20

// MaxSourcePixels is the ceiling on the decoded frame's pixel count:
// 4096*4096 = 16,777,216 pixels, at most 64 MiB of NRGBA. WebP itself allows
// 16383x16383, which would be a ~1 GiB allocation plus a CatmullRom pass over
// it, so the header is checked before the frame is decoded.
const MaxSourcePixels = 4096 * 4096

// ErrAvatarTooLarge reports that downsampling ran out of room: every edge
// length in the reduction ladder still encoded a PNG above the byte ceiling.
var ErrAvatarTooLarge = errors.New("avatar: transcoded PNG exceeds the byte ceiling")

// ErrSourceTooLarge reports that the input was refused before its frame was
// decoded: either the encoded WebP ran past MaxWebPBytes or its header
// declared more than MaxSourcePixels pixels. Both are rejected without
// allocating the frame, so a WebP that decompresses to far more memory than
// it occupies on the wire cannot be used to exhaust the process.
var ErrSourceTooLarge = errors.New("avatar: source WebP exceeds the input bounds")

// TranscodeWebPToPNG reads one WebP image from r (lossy VP8, lossless VP8L,
// or extended VP8X — whatever golang.org/x/image/webp decodes), takes the
// largest centred square of the decoded frame, scales that square to
// AvatarSize x AvatarSize, and encodes it as PNG. It returns the encoded
// bytes when the PNG fits under MaxPNGBytes, ErrSourceTooLarge when r runs
// past MaxWebPBytes or declares more than MaxSourcePixels pixels, a wrapped
// decode error when r does not hold a decodable WebP image, and
// ErrAvatarTooLarge when every downsampling attempt still exceeds the
// ceiling. The 1 MiB bound is what lets an avatar be handed to
// AccountsService as one bounded write instead of a streamed unknown.
//
// Cropping to a centred square is what keeps a non-square source — the
// catalog's header artwork is wider than it is tall — from reaching the
// square canvas anisotropically stretched. Content outside the square is
// dropped rather than squashed, which is the usual reading of a 512x512
// avatar.
func TranscodeWebPToPNG(r io.Reader) ([]byte, error) {
	return transcodeWebPToPNG(r, MaxPNGBytes)
}

// transcodeWebPToPNG is the testable core of TranscodeWebPToPNG: ceiling is
// the byte budget every attempt must fit, the seam through which tests reach
// the ErrAvatarTooLarge path with a budget no PNG can meet.
//
// The reduction ladder is load-bearing, not defensive. A 512x512 RGBA scanline
// stream is 512*(2048+1) = 1,049,088 bytes of PNG-filtered data before deflate
// even runs — already above the 1,048,576-byte ceiling — so content that does
// not compress can never fit at full size. Each rung below the first scales
// the decoded image to the next edge length and re-encodes; at AvatarSize/2
// the raw stream is 256*(1024+1) = 262,400 bytes, a quarter of the ceiling, so
// reduction essentially always succeeds, and ErrAvatarTooLarge stays as the
// explicit contract for an injected ceiling or a future ladder change.
func transcodeWebPToPNG(r io.Reader, ceiling int) ([]byte, error) {
	encoded, err := readBoundedWebP(r)
	if err != nil {
		return nil, err
	}
	config, err := webp.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("avatar: decoding WebP header: %w", err)
	}
	if pixels := int64(config.Width) * int64(config.Height); pixels > MaxSourcePixels {
		return nil, fmt.Errorf("%w: %dx%d is %d pixels, over the %d-pixel bound", ErrSourceTooLarge, config.Width, config.Height, pixels, MaxSourcePixels)
	}
	src, err := webp.Decode(bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("avatar: decoding WebP: %w", err)
	}
	square := centredSquare(src.Bounds())
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	for _, edge := range [3]int{AvatarSize, AvatarSize * 3 / 4, AvatarSize / 2} {
		dst := image.NewNRGBA(image.Rect(0, 0, edge, edge))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, square, draw.Src, nil)
		var buf bytes.Buffer
		if err := enc.Encode(&buf, dst); err != nil {
			return nil, fmt.Errorf("avatar: encoding PNG: %w", err)
		}
		if buf.Len() <= ceiling {
			return buf.Bytes(), nil
		}
	}
	return nil, ErrAvatarTooLarge
}

// readBoundedWebP drains r into memory under MaxWebPBytes. One extra byte is
// requested so a payload sitting exactly on the bound is distinguishable from
// one that runs past it, and nothing downstream sees a reader at all — both
// the header check and the full decode read the same buffered bytes, so the
// dimension guard cannot be bypassed by a stream that reports one size in its
// header and another in its body.
func readBoundedWebP(r io.Reader) ([]byte, error) {
	encoded, err := io.ReadAll(io.LimitReader(r, MaxWebPBytes+1))
	if err != nil {
		return nil, fmt.Errorf("avatar: reading WebP input: %w", err)
	}
	if len(encoded) > MaxWebPBytes {
		return nil, fmt.Errorf("%w: input is over the %d-byte bound", ErrSourceTooLarge, MaxWebPBytes)
	}
	return encoded, nil
}

// centredSquare returns the largest square inside bounds, centred on it. When
// one edge exceeds the other by an odd number the leftover pixel falls on the
// high side: a 441x412 frame keeps columns 14..425, leaving 14 columns on the
// left and 15 on the right.
func centredSquare(bounds image.Rectangle) image.Rectangle {
	edge := bounds.Dx()
	if bounds.Dy() < edge {
		edge = bounds.Dy()
	}
	x := bounds.Min.X + (bounds.Dx()-edge)/2
	y := bounds.Min.Y + (bounds.Dy()-edge)/2
	return image.Rect(x, y, x+edge, y+edge)
}
