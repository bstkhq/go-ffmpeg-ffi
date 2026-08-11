//go:build !ios && !android && (amd64 || arm64)

package ffgo

import "testing"

func TestDecoderReadFrameDrainsAllSelectedStreams(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	decoder, err := NewDecoder(createTestVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	counts := map[MediaType]int{}
	lastPTS := map[MediaType]int64{}
	seenPTS := map[MediaType]bool{}
	for {
		frame, err := decoder.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		if frame == nil {
			break
		}

		mediaType := frame.MediaType()
		pts := frame.PTS()
		if seenPTS[mediaType] && pts <= lastPTS[mediaType] {
			t.Fatalf("%v PTS did not increase: previous=%d current=%d", mediaType, lastPTS[mediaType], pts)
		}
		seenPTS[mediaType] = true
		lastPTS[mediaType] = pts
		counts[mediaType]++
	}

	if counts[MediaTypeVideo] != 60 {
		t.Fatalf("video frames = %d, want 60", counts[MediaTypeVideo])
	}
	if counts[MediaTypeAudio] != 87 {
		t.Fatalf("audio frames = %d, want 87", counts[MediaTypeAudio])
	}
}

func TestDecoderSingleStreamCallsPreserveInterleavedPackets(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	decoder, err := NewDecoder(createTestVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	videoFrames := drainDecodedFrames(t, decoder.DecodeVideo)
	audioFrames := drainDecodedFrames(t, decoder.DecodeAudio)
	if videoFrames != 60 {
		t.Fatalf("video frames = %d, want 60", videoFrames)
	}
	if audioFrames != 87 {
		t.Fatalf("audio frames = %d, want 87", audioFrames)
	}
}

func drainDecodedFrames(t *testing.T, next func() (Frame, error)) int {
	t.Helper()
	count := 0
	for {
		frame, err := next()
		if err != nil {
			t.Fatal(err)
		}
		if frame.IsNil() {
			return count
		}
		count++
	}
}
