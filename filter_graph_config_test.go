//go:build !ios && (amd64 || arm64)

package ffgo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestVideoBufferSourceArgsIncludesFrameRate(t *testing.T) {
	args := videoBufferSourceArgs(FilterGraphConfig{
		Width:     1920,
		Height:    1080,
		PixelFmt:  PixelFormatYUV420P,
		FrameRate: Rational{Num: 30000, Den: 1001},
	}, Rational{Num: 1, Den: 90000}, Rational{Num: 1, Den: 1})

	if !strings.Contains(args, "frame_rate=30000/1001") {
		t.Fatalf("buffer source args omit frame rate: %q", args)
	}
}

func TestVideoFilterGraphAcceptsFrameRate(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	graph, err := NewFilterGraph(FilterGraphConfig{
		Width:     320,
		Height:    240,
		PixelFmt:  PixelFormatYUV420P,
		TimeBase:  Rational{Num: 1001, Den: 30000},
		FrameRate: Rational{Num: 30000, Den: 1001},
		Filters:   "null",
	})
	if err != nil {
		t.Fatalf("create graph: %v", err)
	}
	defer graph.Close()
}

func TestAudioBufferSourceArgsUsesDeclaredLayout(t *testing.T) {
	tests := []struct {
		name string
		cfg  FilterGraphConfig
		want string
	}{
		{
			name: "default layout for channel count",
			cfg: FilterGraphConfig{
				SampleRate: 48000,
				Channels:   3,
				SampleFmt:  SampleFormatFLTP,
			},
			want: "sample_rate=48000:sample_fmt=fltp:channel_layout=3c",
		},
		{
			name: "explicit channel layout",
			cfg: FilterGraphConfig{
				SampleRate:    44100,
				Channels:      5,
				ChannelLayout: ChannelLayout5Point0,
				SampleFmt:     SampleFormatS16,
			},
			want: "sample_rate=44100:sample_fmt=s16:channel_layout=0x607",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audioBufferSourceArgs(tt.cfg); got != tt.want {
				t.Fatalf("audio buffer source args = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAudioFilterGraphAcceptsDefaultLayouts(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	for _, channels := range []int{3, 4, 5, 7} {
		t.Run(fmt.Sprintf("%d_channels", channels), func(t *testing.T) {
			graph, err := NewFilterGraph(FilterGraphConfig{
				SampleRate: 48000,
				Channels:   channels,
				SampleFmt:  SampleFormatFLTP,
				Filters:    "anull",
			})
			if err != nil {
				t.Fatalf("create graph: %v", err)
			}
			defer graph.Close()

			frame := FrameAlloc()
			if frame.IsNil() {
				t.Fatal("allocate frame")
			}
			defer func() { _ = FrameFree(&frame) }()

			avutil.SetFrameFormat(frame.ptr, int32(SampleFormatFLTP))
			avutil.SetFrameSampleRate(frame.ptr, 48000)
			avutil.SetFrameNbSamples(frame.ptr, 32)
			avutil.FrameSetChannels(frame.ptr, int32(channels))
			if err := avutil.FrameGetBufferErr(frame.ptr, 0); err != nil {
				t.Fatalf("allocate frame buffer: %v", err)
			}

			frames, err := graph.Filter(&frame)
			if err != nil {
				t.Fatalf("filter frame: %v", err)
			}
			for _, filtered := range frames {
				if err := FrameFree(filtered); err != nil {
					t.Fatalf("free filtered frame: %v", err)
				}
			}
		})
	}
}

func TestAudioFilterGraphAcceptsExplicitLayout(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	graph, err := NewFilterGraph(FilterGraphConfig{
		SampleRate:    48000,
		Channels:      5,
		ChannelLayout: ChannelLayout5Point0,
		SampleFmt:     SampleFormatFLTP,
		Filters:       "anull",
	})
	if err != nil {
		t.Fatalf("create graph: %v", err)
	}
	defer graph.Close()
}
