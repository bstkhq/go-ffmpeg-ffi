//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
)

func TestEncoderPreservesFractionalFrameRate(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	encoder, err := NewEncoderWithOptions(filepath.Join(t.TempDir(), "ntsc.mkv"), &EncoderOptions{
		Video: &VideoEncoderConfig{
			Codec:       avcodec.CodecIDMPEG4,
			Width:       16,
			Height:      16,
			PixelFormat: PixelFormatYUV420P,
			FrameRate:   NewRational(30_000, 1_001),
			Bitrate:     100_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()

	want := NewRational(1_001, 30_000)
	if got := avcodec.GetCtxTimeBase(encoder.videoCodecCtx); got != want {
		t.Fatalf("codec time base = %d/%d, want %d/%d", got.Num, got.Den, want.Num, want.Den)
	}
	streamNum, streamDen := avformat.GetStreamTimeBase(encoder.videoStream)
	if streamNum != want.Num || streamDen != want.Den {
		t.Fatalf("stream time base = %d/%d, want %d/%d", streamNum, streamDen, want.Num, want.Den)
	}
	if encoder.timeBaseNum != want.Num || encoder.timeBaseDen != want.Den {
		t.Fatalf("encoder time base = %d/%d, want %d/%d", encoder.timeBaseNum, encoder.timeBaseDen, want.Num, want.Den)
	}
}

func TestEncoderFlushPreservesDelayedFrames(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	const frameCount = 30
	output := filepath.Join(t.TempDir(), "delayed.mp4")
	encoder, err := NewEncoder(output, EncoderConfig{
		Width:       160,
		Height:      120,
		PixelFormat: PixelFormatYUV420P,
		CodecID:     avcodec.CodecIDMPEG4,
		BitRate:     300000,
		FrameRate:   15,
		GOPSize:     12,
		MaxBFrames:  2,
	})
	if err != nil {
		t.Fatal(err)
	}

	frame := FrameAlloc()
	if frame.IsNil() {
		encoder.Close()
		t.Fatal("failed to allocate frame")
	}
	defer func() { _ = FrameFree(&frame) }()
	AVUtil.SetFrameWidth(frame, 160)
	AVUtil.SetFrameHeight(frame, 120)
	AVUtil.SetFrameFormat(frame, int32(PixelFormatYUV420P))
	if err := AVUtil.FrameGetBuffer(frame, 0); err != nil {
		encoder.Close()
		t.Fatal(err)
	}

	for i := 0; i < frameCount; i++ {
		if err := AVUtil.FrameMakeWritable(frame); err != nil {
			encoder.Close()
			t.Fatal(err)
		}
		fillTestFrame(frame, i, 160, 120)
		if err := encoder.WriteVideoFrame(frame); err != nil {
			encoder.Close()
			t.Fatalf("write frame %d: %v", i, err)
		}
	}
	if err := encoder.Flush(); err != nil {
		encoder.Close()
		t.Fatal(err)
	}
	if err := encoder.Flush(); err != nil {
		encoder.Close()
		t.Fatalf("second flush: %v", err)
	}
	if err := encoder.WriteVideoFrame(frame); !errors.Is(err, errEncoderFlushed) {
		encoder.Close()
		t.Fatalf("write after flush = %v, want %v", err, errEncoderFlushed)
	}
	if err := encoder.Close(); err != nil {
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
