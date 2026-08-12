//go:build !ios && (amd64 || arm64)

package ffgo

import (
	"path/filepath"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestMuxerTrailerDrainsDelayedFrames(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	const frameCount = 20
	output := filepath.Join(t.TempDir(), "muxed-delayed.mp4")
	muxer, err := NewMuxer(output, "mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer muxer.Close()
	stream, err := muxer.AddVideoStream(&VideoStreamConfig{
		Codec:       avcodec.CodecIDMPEG4,
		Width:       160,
		Height:      120,
		PixelFormat: PixelFormatYUV420P,
		FrameRate:   15,
		BitRate:     300000,
		GOPSize:     12,
		MaxBFrames:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatal(err)
	}

	frame := FrameAlloc()
	if frame.IsNil() {
		t.Fatal("failed to allocate frame")
	}
	defer func() { _ = FrameFree(&frame) }()
	AVUtil.SetFrameWidth(frame, 160)
	AVUtil.SetFrameHeight(frame, 120)
	AVUtil.SetFrameFormat(frame, int32(PixelFormatYUV420P))
	if err := AVUtil.FrameGetBuffer(frame, 0); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < frameCount; i++ {
		if err := AVUtil.FrameMakeWritable(frame); err != nil {
			t.Fatal(err)
		}
		fillTestFrame(frame, i, 160, 120)
		avutil.SetFramePTS(frame.ptr, int64(i))
		if err := muxer.WriteFrame(stream, frame); err != nil {
			t.Fatalf("write frame %d: %v", i, err)
		}
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err == nil {
		t.Fatal("second trailer write succeeded")
	}
	if err := muxer.Close(); err != nil {
		t.Fatal(err)
	}

	decoder, err := NewDecoder(output, WithStreams(MediaTypeVideo))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	if got := drainDecodedFrames(t, decoder.DecodeVideo); got != frameCount {
		t.Fatalf("decoded frames = %d, want %d", got, frameCount)
	}
}
