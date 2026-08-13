//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
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

	frame.SetWidth(width)
	frame.SetHeight(height)
	frame.SetPixelFormat(PixelFormatYUV420P)
	if err := frame.GetBuffer(32); err != nil {
		t.Fatal(err)
	}

	wrapper := WrapFrame(frame, MediaTypeVideo)
	if got := frame.MediaType(); got != MediaTypeVideo {
		t.Fatalf("media type = %v, want video", got)
	}
	linesizes := avutil.GetFrameLinesize(frame.ptr)
	if got, want := len(frame.Data(0)), int(linesizes[0])*height; got != want {
		t.Fatalf("luma plane length = %d, want %d", got, want)
	}
	if got, want := len(frame.Data(1)), int(linesizes[1])*((height+1)/2); got != want {
		t.Fatalf("chroma plane length = %d, want %d", got, want)
	}
	if data := frame.Data(3); data != nil {
		t.Fatalf("unexpected fourth YUV420P plane with %d bytes", len(data))
	}
	for plane := 0; plane < 4; plane++ {
		if got, want := len(wrapper.Data(plane)), len(frame.Data(plane)); got != want {
			t.Fatalf("wrapper plane %d length = %d, Frame length = %d", plane, got, want)
		}
		if got, want := wrapper.Linesize(plane), frame.Linesize(plane); got != want {
			t.Fatalf("wrapper plane %d linesize = %d, Frame linesize = %d", plane, got, want)
		}
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

	frame.SetSampleFormat(SampleFormatFLTP)
	frame.SetNbSamples(samples)
	frame.SetSampleRate(48000)
	frame.SetChannels(channels)
	if err := frame.GetBuffer(0); err != nil {
		t.Fatal(err)
	}

	wrapper := WrapFrame(frame, MediaTypeAudio)
	if got := frame.MediaType(); got != MediaTypeAudio {
		t.Fatalf("media type = %v, want audio", got)
	}
	for plane := 0; plane < channels; plane++ {
		if got, want := len(frame.Data(plane)), samples*4; got != want {
			t.Fatalf("audio plane %d length = %d, want %d", plane, got, want)
		}
		if got, want := frame.Linesize(plane), int(avutil.GetFrameLinesizePlane(frame.ptr, 0)); got != want {
			t.Fatalf("audio plane %d linesize = %d, want %d", plane, got, want)
		}
		if got, want := len(wrapper.Data(plane)), len(frame.Data(plane)); got != want {
			t.Fatalf("wrapper plane %d length = %d, Frame length = %d", plane, got, want)
		}
		if got, want := wrapper.Linesize(plane), frame.Linesize(plane); got != want {
			t.Fatalf("wrapper plane %d linesize = %d, Frame linesize = %d", plane, got, want)
		}
	}
	if data := frame.Data(channels); data != nil {
		t.Fatalf("unexpected audio plane %d with %d bytes", channels, len(data))
	}
}

func TestFramePackedAudioDataUsesSinglePlane(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	const channels, samples = 2, 32
	frame := FrameAlloc()
	if frame.IsNil() {
		t.Fatal("failed to allocate frame")
	}
	defer func() { _ = frame.Free() }()

	frame.SetSampleFormat(SampleFormatS16)
	frame.SetNbSamples(samples)
	frame.SetSampleRate(48000)
	frame.SetChannels(channels)
	if err := frame.GetBuffer(0); err != nil {
		t.Fatal(err)
	}

	if got, want := len(frame.Data(0)), samples*channels*2; got != want {
		t.Fatalf("packed audio length = %d, want %d", got, want)
	}
	if data := frame.Data(1); data != nil {
		t.Fatalf("unexpected second packed audio plane with %d bytes", len(data))
	}
	if got := frame.Linesize(1); got != 0 {
		t.Fatalf("second packed audio linesize = %d, want 0", got)
	}
}

func TestFrameCanonicalAccessors(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	frame := FrameAlloc()
	if frame.IsNil() {
		t.Fatal("failed to allocate frame")
	}
	defer func() { _ = frame.Free() }()

	frame.SetWidth(320)
	frame.SetHeight(180)
	frame.SetPixelFormat(PixelFormatRGBA)
	frame.SetPTS(42)
	if err := frame.GetBuffer(32); err != nil {
		t.Fatal(err)
	}

	if frame.Raw() == nil {
		t.Fatal("Raw returned nil for allocated frame")
	}
	if got := avutil.GetFrameWidth(frame.Raw()); got != 320 {
		t.Fatalf("raw AVFrame width = %d, want 320", got)
	}
	if got := frame.Width(); got != 320 {
		t.Fatalf("width = %d, want 320", got)
	}
	if got := frame.Height(); got != 180 {
		t.Fatalf("height = %d, want 180", got)
	}
	if got := frame.PixelFormat(); got != PixelFormatRGBA {
		t.Fatalf("pixel format = %v, want RGBA", got)
	}
	if got := frame.PTS(); got != 42 {
		t.Fatalf("PTS = %d, want 42", got)
	}

	clone, err := frame.Clone()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = clone.Free() }()
	if got := clone.PTS(); got != 42 {
		t.Fatalf("clone PTS = %d, want 42", got)
	}
}

func TestFrameCanonicalMethodsRejectReturnedPoolLease(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	pool := NewFramePool(1)
	defer func() { _ = pool.Close() }()
	frame, err := pool.Get()
	if err != nil {
		t.Fatal(err)
	}
	copyOfLease := frame
	if err := pool.Put(&frame); err != nil {
		t.Fatal(err)
	}

	if copyOfLease.Raw() != nil {
		t.Fatal("Raw exposed a frame after its pool lease was returned")
	}
	if err := copyOfLease.GetBuffer(0); !errors.Is(err, ErrFrameLeaseReturned) {
		t.Fatalf("GetBuffer error = %v, want ErrFrameLeaseReturned", err)
	}
	if err := copyOfLease.MakeWritable(); !errors.Is(err, ErrFrameLeaseReturned) {
		t.Fatalf("MakeWritable error = %v, want ErrFrameLeaseReturned", err)
	}
}
