//go:build amd64 || arm64

// Example: custom-io - Demonstrates decoding from io.Reader
//
// Usage: custom-io <input_file>
//
// This example reads a video file using custom I/O (io.Reader)
// instead of a file path, useful for reading from HTTP, S3, etc.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/bstkhq/go-ffmpeg-ffi"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input_file>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s video.mp4\n", os.Args[0])
		os.Exit(1)
	}

	inputFile := os.Args[1]

	// Initialize FFmpeg
	if err := ffmpeg.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize FFmpeg: %v\n", err)
		os.Exit(1)
	}

	// Print version info
	avutil, avcodec, avformat := ffmpeg.Version()
	fmt.Printf("FFmpeg versions: avutil=%d.%d.%d, avcodec=%d.%d.%d, avformat=%d.%d.%d\n",
		avutil>>16, (avutil>>8)&0xFF, avutil&0xFF,
		avcodec>>16, (avcodec>>8)&0xFF, avcodec&0xFF,
		avformat>>16, (avformat>>8)&0xFF, avformat&0xFF)

	// Open file as io.Reader
	file, err := os.Open(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	fmt.Printf("\nOpening input via io.Reader: %s\n", inputFile)

	// Create decoder from io.Reader
	// Since *os.File implements both io.Reader and io.Seeker,
	// the decoder will support seeking
	decoder, err := ffmpeg.NewDecoderFromReader(file, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create decoder: %v\n", err)
		os.Exit(1)
	}
	defer decoder.Close()

	// Print stream info
	if decoder.HasVideo() {
		videoInfo := decoder.VideoStream()
		fmt.Printf("Video stream: %dx%d, codec=%v\n",
			videoInfo.Width, videoInfo.Height, videoInfo.CodecID)
	}

	if decoder.HasAudio() {
		audioInfo := decoder.AudioStream()
		fmt.Printf("Audio stream: %d Hz, %d channels, codec=%v\n",
			audioInfo.SampleRate, audioInfo.Channels, audioInfo.CodecID)
	}

	fmt.Printf("Duration: %v\n", decoder.Duration())

	// Read frames
	fmt.Println("\nReading frames...")

	videoFrames := 0
	audioFrames := 0
	maxFrames := 100 // Limit for demo

	for videoFrames+audioFrames < maxFrames {
		frame, err := decoder.ReadFrame()
		if err != nil {
			if ffmpeg.IsEOF(err) {
				break
			}
			fmt.Fprintf(os.Stderr, "Read error: %v\n", err)
			os.Exit(1)
		}
		if frame == nil {
			continue
		}

		switch frame.MediaType() {
		case ffmpeg.MediaTypeVideo:
			videoFrames++
			if videoFrames <= 5 {
				fmt.Printf("  Video frame %d: %dx%d, PTS=%d\n",
					videoFrames, frame.Width(), frame.Height(), frame.PTS())
			}
		case ffmpeg.MediaTypeAudio:
			audioFrames++
			if audioFrames <= 3 {
				fmt.Printf("  Audio frame %d: %d samples, PTS=%d\n",
					audioFrames, frame.NumSamples(), frame.PTS())
			}
		}
	}

	fmt.Printf("\nRead %d video frames and %d audio frames\n", videoFrames, audioFrames)

	// Demonstrate custom I/O callbacks
	fmt.Println("\n--- Custom I/O Callbacks Example ---")
	demonstrateCustomCallbacks(inputFile)
}

// demonstrateCustomCallbacks shows how to use custom I/O callbacks
func demonstrateCustomCallbacks(inputFile string) {
	// Open file manually
	file, err := os.Open(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open file: %v\n", err)
		return
	}
	defer file.Close()

	// Track read statistics
	totalBytesRead := int64(0)
	readCalls := 0

	// Create custom callbacks
	callbacks := &ffmpeg.IOCallbacks{
		Read: func(buf []byte) (int, error) {
			n, err := file.Read(buf)
			if n > 0 {
				totalBytesRead += int64(n)
				readCalls++
			}
			if err == io.EOF {
				return n, io.EOF
			}
			return n, err
		},
		Seek: func(offset int64, whence int) (int64, error) {
			return file.Seek(offset, whence)
		},
	}

	// Create decoder with custom callbacks
	decoder, err := ffmpeg.NewDecoderFromIO(callbacks, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create decoder with callbacks: %v\n", err)
		return
	}
	defer decoder.Close()

	// Read a few frames
	frameCount := 0
	for frameCount < 10 {
		frame, err := decoder.ReadFrame()
		if err != nil {
			if ffmpeg.IsEOF(err) {
				break
			}
			fmt.Fprintf(os.Stderr, "Read error: %v\n", err)
			return
		}
		if frame == nil {
			continue
		}
		if frame.MediaType() == ffmpeg.MediaTypeVideo {
			frameCount++
		}
	}

	fmt.Printf("Custom I/O stats:\n")
	fmt.Printf("  Total bytes read: %d\n", totalBytesRead)
	fmt.Printf("  Read calls: %d\n", readCalls)
	fmt.Printf("  Frames decoded: %d\n", frameCount)
}
