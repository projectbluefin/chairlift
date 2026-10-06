package avatar

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"

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
// or extended VP8X — whatever golang.org/x/image/webp decodes), fits the
// artwork inside the circle inscribed in an AvatarSize x AvatarSize
// transparent canvas, and encodes it as PNG. It returns the encoded
// bytes when the PNG fits under MaxPNGBytes, ErrSourceTooLarge when r runs
// past MaxWebPBytes or declares more than MaxSourcePixels pixels, a wrapped
// decode error when r does not hold a decodable WebP image, and
// ErrAvatarTooLarge when every downsampling attempt still exceeds the
// ceiling. The 1 MiB bound is what lets an avatar be handed to
// AccountsService as one bounded write instead of a streamed unknown.
//
// GNOME draws every avatar — login, lock screen, Quick Settings, this
// page's AdwAvatar — clipped to a circle. A centred-square crop cut the
// catalog's characters at the head and tail, and the circle then cut them
// again, so the artwork's visible content (its non-transparent bounding box)
// is scaled, without stretching, until its diagonal fits the circle.
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
	content := visibleBounds(src)
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	for _, edge := range [3]int{AvatarSize, AvatarSize * 3 / 4, AvatarSize / 2} {
		dst := image.NewNRGBA(image.Rect(0, 0, edge, edge))
		draw.CatmullRom.Scale(dst, fitInCircle(content, edge), src, content, draw.Src, nil)
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

// visibleBounds is the bounding box of src's non-transparent pixels, so
// transparent margins in the artwork do not shrink the character. A fully
// transparent frame keeps its whole bounds.
func visibleBounds(src image.Image) image.Rectangle {
	b := src.Bounds()
	box := image.Rectangle{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := src.At(x, y).RGBA(); a != 0 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if box.Empty() {
		return b
	}
	return box
}

// fitInCircle returns the destination rectangle, centred on an edge x edge
// canvas, that holds content at its own aspect ratio with its diagonal no
// longer than the canvas's inscribed circle, so no corner of the artwork
// falls outside a circular clip.
func fitInCircle(content image.Rectangle, edge int) image.Rectangle {
	w, h := float64(content.Dx()), float64(content.Dy())
	scale := float64(edge) / math.Hypot(w, h)
	// A sliver of visible content still gets one pixel each way rather than
	// an empty rectangle that would paint a blank avatar.
	dw, dh := max(1, int(w*scale)), max(1, int(h*scale))
	x, y := (edge-dw)/2, (edge-dh)/2
	return image.Rect(x, y, x+dw, y+dh)
}
