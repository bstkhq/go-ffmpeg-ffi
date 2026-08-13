//go:build amd64 || arm64

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
	output, muxer := muxDelayedFrames(t)

	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
	if err := muxer.WriteTrailer(); err == nil {
		t.Fatal("second trailer write succeeded")
	}
	if err := muxer.Close(); err != nil {
		t.Fatal(err)
	}

	assertDecodedVideoFrames(t, output, 20)
}

func TestMuxerCloseWritesTrailerAndDrainsDelayedFrames(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	output, muxer := muxDelayedFrames(t)

	if err := muxer.Close(); err != nil {
		t.Fatal(err)
	}
	if !muxer.trailerWritten {
		t.Fatal("Close did not write the trailer")
	}

	assertDecodedVideoFrames(t, output, 20)
}

func muxDelayedFrames(t *testing.T) (string, *Muxer) {
	t.Helper()
	const frameCount = 20
	output := filepath.Join(t.TempDir(), "muxed-delayed.mp4")
	muxer, err := NewMuxer(output, "mp4")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = muxer.Close() })
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
	frame.SetWidth(160)
	frame.SetHeight(120)
	frame.SetPixelFormat(PixelFormatYUV420P)
	if err := frame.GetBuffer(0); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < frameCount; i++ {
		if err := frame.MakeWritable(); err != nil {
			t.Fatal(err)
		}
		fillTestFrame(frame, i, 160, 120)
		avutil.SetFramePTS(frame.ptr, int64(i))
		if err := muxer.WriteFrame(stream, frame); err != nil {
			t.Fatalf("write frame %d: %v", i, err)
		}
	}
	return output, muxer
}

func assertDecodedVideoFrames(t *testing.T, output string, want int) {
	t.Helper()
	decoder, err := NewDecoder(output, &DecoderOptions{Streams: []MediaType{MediaTypeVideo}})
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	if got := drainDecodedFrames(t, decoder.DecodeVideo); got != want {
		t.Fatalf("decoded frames = %d, want %d", got, want)
	}
}
