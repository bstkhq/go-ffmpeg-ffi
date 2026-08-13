//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestEstimateFrameRateStopsAfterFramesWithoutPTS(t *testing.T) {
	calls := 0
	_, err := estimateFrameRateFromPTS(func() (int64, bool, error) {
		calls++
		return avutil.AV_NOPTS_VALUE, true, nil
	}, NewRational(1, 1000))
	if err == nil {
		t.Fatal("expected missing PTS error")
	}
	if calls != frameRateDetectionSampleSize {
		t.Fatalf("decoded frames = %d, want %d", calls, frameRateDetectionSampleSize)
	}
}

func TestEstimateFrameRateIgnoresMissingPTS(t *testing.T) {
	pts := []int64{avutil.AV_NOPTS_VALUE, 0, 40, 80}
	next := 0
	fps, err := estimateFrameRateFromPTS(func() (int64, bool, error) {
		if next == len(pts) {
			return 0, false, nil
		}
		value := pts[next]
		next++
		return value, true, nil
	}, NewRational(1, 1000))
	if err != nil {
		t.Fatal(err)
	}
	if fps != 25 {
		t.Fatalf("frame rate = %v, want 25", fps)
	}
}

func TestEstimateFrameRatePropagatesDecodeError(t *testing.T) {
	want := errors.New("decode failed")
	_, err := estimateFrameRateFromPTS(func() (int64, bool, error) {
		return 0, false, want
	}, NewRational(1, 1000))
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestGenerateTimestamps(t *testing.T) {
	tb := NewRational(1, 30)
	got := GenerateTimestamps(3, tb, 30)
	want := []int64{0, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("len: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("idx %d: got %d want %d", i, got[i], want[i])
		}
	}
}

func TestValidateTimestamps(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	f1 := FrameAlloc()
	f2 := FrameAlloc()
	f3 := FrameAlloc()
	defer func() { _ = FrameFree(&f1) }()
	defer func() { _ = FrameFree(&f2) }()
	defer func() { _ = FrameFree(&f3) }()

	avutil.SetFramePTS(f1.ptr, 0)
	avutil.SetFramePTS(f2.ptr, 1)
	avutil.SetFramePTS(f3.ptr, 2)

	if err := ValidateTimestamps([]*Frame{&f1, &f2, &f3}); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}

	avutil.SetFramePTS(f2.ptr, -1)
	if err := ValidateTimestamps([]*Frame{&f1, &f2, &f3}); err == nil {
		t.Fatalf("expected error for non-monotonic pts")
	}
}

func TestFrameRateDetect(t *testing.T) {
	if testing.Short() {
		t.Log("Skipping FrameRateDetect in short mode")
		return
	}
	if !requireFFmpeg(t) {
		return
	}

	in := filepath.Join("testdata", "test.mp4")
	dec, err := NewDecoder(in, nil)
	if err != nil {
		t.Fatalf("NewDecoder failed: %v", err)
	}
	defer dec.Close()

	fps, err := FrameRateDetect(dec)
	if err != nil {
		t.Fatalf("FrameRateDetect failed: %v", err)
	}
	if fps <= 0 {
		t.Fatalf("expected positive fps, got %f", fps)
	}
	if fps > 240 {
		t.Fatalf("fps too high: %f", fps)
	}
}
