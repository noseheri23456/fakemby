package testutil

import (
	"bytes"
	"image"
	"image/png"
)

// ImageBytes returns a decodable, deterministic image rather than mock JPEG text.
func ImageBytes() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	var x uint32 = 123456789
	for i := range img.Pix {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		img.Pix[i] = byte(x)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}
