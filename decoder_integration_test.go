//go:build amd64 || arm64

package ffgo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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

func TestDecoderPropagatesTruncatedInputError(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	dir := t.TempDir()
	fragmented := filepath.Join(dir, "fragmented.mp4")
	command := exec.Command("ffmpeg", "-loglevel", "error", "-y",
		"-i", createTestVideo(t), "-c", "copy",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof", fragmented)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create fragmented fixture: %v\n%s", err, output)
	}
	data, err := os.ReadFile(fragmented)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 5000 {
		t.Fatalf("fragmented fixture is unexpectedly small: %d bytes", len(data))
	}
	truncated := filepath.Join(dir, "truncated.mp4")
	if err := os.WriteFile(truncated, data[:len(data)-5000], 0o600); err != nil {
		t.Fatal(err)
	}

	decoder, err := NewDecoder(truncated)
	if err != nil {
		t.Fatalf("open truncated fixture: %v", err)
	}
	defer decoder.Close()
	frames := 0
	for frames < 500 {
		frame, decodeErr := decoder.ReadFrame()
		if decodeErr != nil {
			if frames == 0 {
				t.Fatal("truncated fixture failed before producing any frame")
			}
			return
		}
		if frame == nil {
			t.Fatalf("truncated input became clean EOF after %d frames", frames)
		}
		frames++
	}
	t.Fatal("truncated input did not terminate within 500 frames")
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
