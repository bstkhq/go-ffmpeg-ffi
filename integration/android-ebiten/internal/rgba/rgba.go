// Package rgba prepares decoded RGBA frames for Ebitengine texture upload.
package rgba

import "fmt"

// Pack removes row padding and normalizes bottom-up frames into a tightly
// packed, top-down RGBA buffer.
func Pack(data []byte, stride, width, height int) ([]byte, error) {
	rowBytes := width * 4
	absStride := stride
	if absStride < 0 {
		absStride = -absStride
	}
	if width <= 0 || height <= 0 || absStride < rowBytes || len(data) < absStride*height {
		return nil, fmt.Errorf(
			"invalid RGBA buffer: %dx%d stride=%d bytes=%d",
			width, height, stride, len(data),
		)
	}

	pixels := make([]byte, rowBytes*height)
	for y := 0; y < height; y++ {
		sourceY := y
		if stride < 0 {
			sourceY = height - 1 - y
		}
		copy(pixels[y*rowBytes:(y+1)*rowBytes], data[sourceY*absStride:sourceY*absStride+rowBytes])
	}
	return pixels, nil
}
