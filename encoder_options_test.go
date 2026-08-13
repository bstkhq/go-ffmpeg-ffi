//go:build amd64 || arm64

package ffmpeg

import (
	"strings"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestCloneEncoderOptionsDeepCopiesMutableFields(t *testing.T) {
	original := &EncoderOptions{
		IOOptions:    map[string]string{"timeout": "1000"},
		MuxerOptions: map[string]string{"movflags": "+faststart"},
		Video: &VideoEncoderConfig{
			Width:        1920,
			CodecOptions: map[string]string{"x264-params": "rc-lookahead=40"},
		},
		Audio: &AudioEncoderConfig{SampleRate: 48_000},
	}

	clone := cloneEncoderOptions(original)
	clone.IOOptions["timeout"] = "2000"
	clone.MuxerOptions["movflags"] = "+frag_keyframe"
	clone.Video.Width = 1280
	clone.Video.CodecOptions["x264-params"] = "rc-lookahead=20"
	clone.Audio.SampleRate = 44_100

	if original.IOOptions["timeout"] != "1000" ||
		original.MuxerOptions["movflags"] != "+faststart" ||
		original.Video.Width != 1920 ||
		original.Video.CodecOptions["x264-params"] != "rc-lookahead=40" ||
		original.Audio.SampleRate != 48_000 {
		t.Fatalf("clone mutated caller options: %#v", original)
	}
}

func TestApplyVideoOptionsAppliesDeclaredFields(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	codec := avcodec.FindEncoder(avcodec.CodecIDMPEG4)
	if codec == nil {
		t.Fatal("MPEG-4 encoder not available")
	}
	ctx := avcodec.AllocContext3(codec)
	if ctx == nil {
		t.Fatal("failed to allocate codec context")
	}
	defer avcodec.FreeContext(&ctx)

	if err := applyVideoOptions(ctx, &VideoEncoderConfig{
		MinBitrate:     123_456,
		BFrameStrategy: 2,
	}); err != nil {
		t.Fatal(err)
	}

	if got, err := avutil.OptGetInt(ctx, "minrate", avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
		t.Fatal(err)
	} else if got != 123_456 {
		t.Fatalf("minrate = %d, want 123456", got)
	}
	if got, err := avutil.OptGetInt(ctx, "b_strategy", avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
		t.Fatal(err)
	} else if got != 2 {
		t.Fatalf("b_strategy = %d, want 2", got)
	}
}

func TestApplyVideoOptionsRejectsUnknownExplicitOption(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	codec := avcodec.FindEncoder(avcodec.CodecIDMPEG4)
	if codec == nil {
		t.Fatal("MPEG-4 encoder not available")
	}
	ctx := avcodec.AllocContext3(codec)
	if ctx == nil {
		t.Fatal("failed to allocate codec context")
	}
	defer avcodec.FreeContext(&ctx)

	err := applyVideoOptions(ctx, &VideoEncoderConfig{
		CodecOptions: map[string]string{"definitely-not-an-ffmpeg-option": "1"},
	})
	if err == nil {
		t.Fatal("unknown explicit codec option was silently accepted")
	}
	if !strings.Contains(err.Error(), "definitely-not-an-ffmpeg-option") {
		t.Fatalf("error %q does not name the rejected option", err)
	}
}
