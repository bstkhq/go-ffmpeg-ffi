//go:build amd64 || arm64

package ffgo

import (
	"path/filepath"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
)

func TestMuxerFailedEncoderDoesNotRegisterStream(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	muxer, err := NewMuxer(filepath.Join(t.TempDir(), "atomic-stream.mp4"), "mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer muxer.Close()

	if _, err := muxer.AddVideoStream(&VideoStreamConfig{
		Codec:       avcodec.CodecIDMPEG4,
		PixelFormat: PixelFormatYUV420P,
		FrameRate:   25,
	}); err == nil {
		t.Fatal("opening a video encoder without dimensions succeeded")
	}
	if got := avformat.GetNumStreams(muxer.formatCtx); got != 0 {
		t.Fatalf("native streams after failed encoder setup = %d, want 0", got)
	}

	stream, err := muxer.AddVideoStream(testMuxerVideoConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stream.Index(), int(avformat.GetStreamIndex(stream.stream)); got != want {
		t.Fatalf("MuxerStream index = %d, native index = %d", got, want)
	}
	if stream.Index() != 0 {
		t.Fatalf("first registered stream index = %d, want 0", stream.Index())
	}
}

func TestMuxerGlobalHeaderFollowsContainer(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	for _, tt := range []struct {
		name   string
		format string
		want   bool
	}{
		{name: "mp4 requires global headers", format: "mp4", want: true},
		{name: "mpegts keeps headers in packets", format: "mpegts", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			muxer, err := NewMuxer(filepath.Join(t.TempDir(), "output"), tt.format)
			if err != nil {
				t.Fatal(err)
			}
			defer muxer.Close()

			if got := avformat.NeedsGlobalHeader(muxer.formatCtx); got != tt.want {
				t.Fatalf("NeedsGlobalHeader(%s) = %v, want %v", tt.format, got, tt.want)
			}
			stream, err := muxer.AddVideoStream(testMuxerVideoConfig())
			if err != nil {
				t.Fatal(err)
			}
			got := avcodec.GetCtxFlags(stream.codecCtx)&avcodec.CodecFlagGlobalHeader != 0
			if got != tt.want {
				t.Fatalf("video global-header flag for %s = %v, want %v", tt.format, got, tt.want)
			}

			audio, err := muxer.AddAudioStream(&AudioStreamConfig{
				Codec:        avcodec.CodecIDAAC,
				SampleRate:   48000,
				Channels:     2,
				SampleFormat: SampleFormatFltP,
				BitRate:      128000,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := audio.Index(), int(avformat.GetStreamIndex(audio.stream)); got != want {
				t.Fatalf("audio MuxerStream index = %d, native index = %d", got, want)
			}
			got = avcodec.GetCtxFlags(audio.codecCtx)&avcodec.CodecFlagGlobalHeader != 0
			if got != tt.want {
				t.Fatalf("audio global-header flag for %s = %v, want %v", tt.format, got, tt.want)
			}
		})
	}
}

func testMuxerVideoConfig() *VideoStreamConfig {
	return &VideoStreamConfig{
		Codec:       avcodec.CodecIDMPEG4,
		Width:       16,
		Height:      16,
		PixelFormat: PixelFormatYUV420P,
		FrameRate:   25,
		BitRate:     100000,
		GOPSize:     12,
	}
}
