//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestNewConcatDecoder_Errors(t *testing.T) {
	if _, err := NewConcatDecoder(nil, nil); err == nil {
		t.Fatalf("expected error for empty file list")
	}
	if _, err := NewConcatDecoder([]string{""}, nil); err == nil {
		t.Fatalf("expected error for empty path")
	}
	if _, err := NewConcatDecoder([]string{"does-not-exist.mp4"}, nil); err == nil {
		t.Fatalf("expected error for missing input file")
	}
	if _, err := NewConcatDecoderFromFFConcat(nil, nil); err == nil {
		t.Fatalf("expected error for empty ffconcat script")
	}
	if _, err := NewConcatDecoderFromFile("", nil); err == nil {
		t.Fatalf("expected error for empty ffconcat list path")
	}
}

func TestNewConcatDecoder_ConcatsVideo(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	in := filepath.Join("testdata", "test.mp4")

	// Baseline single-file duration + frame count.
	d1, err := NewDecoder(in, nil)
	if err != nil {
		t.Fatalf("NewDecoder failed: %v", err)
	}
	defer d1.Close()

	dur1 := d1.DurationMicroseconds()
	_ = dur1 // duration may be unknown depending on demuxer/container

	count1, err := countVideoFrames(d1)
	if err != nil {
		t.Fatalf("countVideoFrames (single) failed: %v", err)
	}
	if count1 <= 0 {
		t.Fatalf("expected at least 1 video frame, got %d", count1)
	}

	// Concat same file twice.
	d2, err := NewConcatDecoder([]string{in, in}, nil)
	if err != nil {
		t.Fatalf("NewConcatDecoder failed: %v", err)
	}
	defer d2.Close()

	dur2 := d2.DurationMicroseconds()
	_ = dur2 // duration is often unknown for concat demuxer; rely on frame count instead.

	// Decode all video frames, ensure monotonic PTS where present and that we got >1 file worth.
	var lastPTS int64 = avutil.AV_NOPTS_VALUE
	var gotPTS bool
	count2 := 0
	for {
		f, err := d2.DecodeVideo()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("DecodeVideo failed: %v", err)
		}
		count2++

		pts := avutil.GetFramePTS(f.ptr)
		if pts != avutil.AV_NOPTS_VALUE {
			if gotPTS && lastPTS != avutil.AV_NOPTS_VALUE && pts < lastPTS {
				t.Fatalf("non-monotonic PTS: prev=%d curr=%d", lastPTS, pts)
			}
			lastPTS = pts
			gotPTS = true
		}
	}

	if count2 < count1*2-1 {
		t.Fatalf("expected ~2x frames (single=%d, concat=%d)", count1, count2)
	}
	if !gotPTS {
		t.Fatalf("expected at least some frames to have PTS")
	}
}

func countVideoFrames(d *Decoder) (int, error) {
	n := 0
	for {
		_, err := d.DecodeVideo()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return n, nil
			}
			return 0, err
		}
		n++
	}
}
