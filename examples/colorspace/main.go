//go:build amd64 || arm64

// Example: colorspace - Demonstrates explicit colorspace matrix control for swscale.
//
// Usage: colorspace <input_file>
package main

import (
	"fmt"
	"os"

	"github.com/bstkhq/go-ffmpeg-ffi"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input_file>\n", os.Args[0])
		os.Exit(1)
	}
	in := os.Args[1]

	if err := ffmpeg.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize FFmpeg: %v\n", err)
		os.Exit(1)
	}

	dec, err := ffmpeg.NewDecoder(in, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open input: %v\n", err)
		os.Exit(1)
	}
	defer dec.Close()

	if !dec.HasVideo() {
		fmt.Fprintln(os.Stderr, "No video stream.")
		os.Exit(1)
	}
	v := dec.VideoStream()
	if err := dec.OpenVideoDecoder(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open video decoder: %v\n", err)
		os.Exit(1)
	}

	f, err := dec.DecodeVideo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "DecodeVideo failed: %v\n", err)
		os.Exit(1)
	}
	if f.IsNil() {
		fmt.Fprintln(os.Stderr, "No frame decoded.")
		os.Exit(1)
	}

	// Best-effort: set source color metadata (use BT.709).
	f.SetColorSpec(ffmpeg.ColorSpec{
		Range:     ffmpeg.ColorRangeMPEG,
		Space:     ffmpeg.ColorSpaceBT709,
		Primaries: ffmpeg.ColorPrimariesBT709,
		Transfer:  ffmpeg.ColorTransferBT709,
	})

	// Create a scaler (no resize, same pixel format) and force conversion matrix to BT.2020.
	sc, err := ffmpeg.NewScaler(v.Width, v.Height, v.PixelFmt, v.Width, v.Height, v.PixelFmt, ffmpeg.ScaleBilinear)
	if err != nil {
		fmt.Fprintf(os.Stderr, "NewScaler failed: %v\n", err)
		os.Exit(1)
	}
	defer sc.Close()

	if err := sc.SetColorspace(ffmpeg.ColorSpaceBT709, ffmpeg.ColorSpaceBT2020NCL); err != nil {
		fmt.Fprintf(os.Stderr, "SetColorspace failed: %v\n", err)
		os.Exit(1)
	}

	out, err := sc.Scale(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Scale failed: %v\n", err)
		os.Exit(1)
	}

	// Attach output metadata describing BT.2020/PQ (BT.2100 PQ) as an example.
	out.SetColorSpec(ffmpeg.ColorSpec{
		Range:     ffmpeg.ColorRangeMPEG,
		Space:     ffmpeg.ColorSpaceBT2020NCL,
		Primaries: ffmpeg.ColorPrimariesBT2020,
		Transfer:  ffmpeg.ColorTransferSMPTE2084,
	})

	fmt.Printf("Input color:  %+v\n", f.ColorSpec())
	fmt.Printf("Output color: %+v\n", out.ColorSpec())
}
