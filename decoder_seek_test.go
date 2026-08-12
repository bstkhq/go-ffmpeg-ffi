//go:build !ios && (amd64 || arm64)

package ffgo

import (
	"testing"
	"time"
)

func TestDecoderSeekResetsEOFState(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	decoder, err := NewDecoder(createTestVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	if count := drainDecodedFrames(t, decoder.DecodeVideo); count != 60 {
		t.Fatalf("initial video frames = %d, want 60", count)
	}
	if err := decoder.SeekTimestamp(0); err != nil {
		t.Fatal(err)
	}
	frame, err := decoder.DecodeVideo()
	if err != nil {
		t.Fatal(err)
	}
	if frame.IsNil() || GetFrameInfo(frame).PTS != 0 {
		t.Fatalf("first frame after seek has PTS %d, want 0", GetFrameInfo(frame).PTS)
	}
}

func TestDecoderSeekPreciseOpensDecoderAndReturnsTargetFrame(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	decoder, err := NewDecoder(createTestVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	if err := decoder.SeekPrecise(time.Second); err != nil {
		t.Fatal(err)
	}
	frame, err := decoder.DecodeVideo()
	if err != nil {
		t.Fatal(err)
	}
	if frame.IsNil() {
		t.Fatal("precise seek returned no frame")
	}
	if GetFrameInfo(frame).PTS != 15360 {
		t.Fatalf("precise seek frame PTS = %d, want 15360", GetFrameInfo(frame).PTS)
	}
}
