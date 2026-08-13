//go:build amd64 || arm64

package ffmpeg

import (
	"testing"
)

func TestNewStreamingEncoder_URLMappingAndLazyIO(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	opts := &EncoderOptions{
		Video: &VideoEncoderConfig{
			Codec:       CodecIDH264,
			Width:       160,
			Height:      120,
			FrameRate:   NewRational(30, 1),
			PixelFormat: PixelFormatYUV420P,
			RateControl: RateControlCRF,
			CRF:         28,
		},
		IOOptions: map[string]string{
			"timeout":    "5000000",
			"rw_timeout": "5000000",
			"max_delay":  "250000",
		},
	}
	enc, err := NewStreamingEncoder("rtmp://example.com/live/stream", opts)
	if err != nil {
		t.Fatalf("NewStreamingEncoder failed: %v", err)
	}
	defer enc.Close()

	// Must not connect/open IO eagerly for URL outputs.
	if enc.ioCtx != nil {
		t.Fatalf("expected ioCtx to be nil (lazy open), got %v", enc.ioCtx)
	}
	if enc.formatCtx == nil {
		t.Fatalf("expected formatCtx to be initialized")
	}
	if opts.Format != "" {
		t.Fatalf("constructor mutated caller format: %q", opts.Format)
	}
	opts.IOOptions["timeout"] = "changed"
	if enc.ioOptions["timeout"] != "5000000" {
		t.Fatalf("encoder retained caller IOOptions map")
	}
}

func TestNewStreamingEncoder_UnsupportedScheme(t *testing.T) {
	_, err := NewStreamingEncoder("ftp://example.com/out", &EncoderOptions{})
	if err == nil {
		t.Fatalf("expected error for unsupported scheme")
	}
}

func TestNewStreamingEncoderRequiresOptions(t *testing.T) {
	_, err := NewStreamingEncoder("rtmp://example.com/live/stream", nil)
	if err == nil {
		t.Fatal("expected nil options to fail")
	}
}
