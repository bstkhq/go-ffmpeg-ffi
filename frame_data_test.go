//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestFrameWrapperVideoDataUsesPlaneGeometry(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	const width, height = 17, 9
	frame := FrameAlloc()
	if frame.IsNil() {
		t.Fatal("failed to allocate frame")
	}
	defer func() { _ = FrameFree(&frame) }()

	avutil.SetFrameWidth(frame.ptr, width)
	avutil.SetFrameHeight(frame.ptr, height)
	avutil.SetFrameFormat(frame.ptr, int32(PixelFormatYUV420P))
	if err := avutil.FrameGetBufferErr(frame.ptr, 32); err != nil {
		t.Fatal(err)
	}

	wrapper := WrapFrame(frame, MediaTypeVideo)
	linesizes := avutil.GetFrameLinesize(frame.ptr)
	if got, want := len(wrapper.Data(0)), int(linesizes[0])*height; got != want {
		t.Fatalf("luma plane length = %d, want %d", got, want)
	}
	if got, want := len(wrapper.Data(1)), int(linesizes[1])*((height+1)/2); got != want {
		t.Fatalf("chroma plane length = %d, want %d", got, want)
	}
	if data := wrapper.Data(3); data != nil {
		t.Fatalf("unexpected fourth YUV420P plane with %d bytes", len(data))
	}
}

func TestFrameWrapperAudioDataUsesExtendedPlanes(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	const channels, samples = 9, 32
	frame := FrameAlloc()
	if frame.IsNil() {
		t.Fatal("failed to allocate frame")
	}
	defer func() { _ = FrameFree(&frame) }()

	avutil.SetFrameFormat(frame.ptr, int32(SampleFormatFLTP))
	avutil.SetFrameNbSamples(frame.ptr, samples)
	avutil.SetFrameSampleRate(frame.ptr, 48000)
	avutil.FrameSetChannels(frame.ptr, channels)
	if err := avutil.FrameGetBufferErr(frame.ptr, 0); err != nil {
		t.Fatal(err)
	}

	wrapper := WrapFrame(frame, MediaTypeAudio)
	for plane := 0; plane < channels; plane++ {
		if got, want := len(wrapper.Data(plane)), samples*4; got != want {
			t.Fatalf("audio plane %d length = %d, want %d", plane, got, want)
		}
		if got, want := wrapper.Linesize(plane), int(avutil.GetFrameLinesizePlane(frame.ptr, 0)); got != want {
			t.Fatalf("audio plane %d linesize = %d, want %d", plane, got, want)
		}
	}
	if data := wrapper.Data(channels); data != nil {
		t.Fatalf("unexpected audio plane %d with %d bytes", channels, len(data))
	}
}
