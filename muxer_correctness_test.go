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
				SampleFormat: SampleFormatFLTP,
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

func TestMuxerDoesNotMutateStreamConfigs(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	muxer, err := NewMuxer(filepath.Join(t.TempDir(), "configs.mp4"), "mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer muxer.Close()

	videoConfig := VideoStreamConfig{
		Codec:  avcodec.CodecIDMPEG4,
		Width:  16,
		Height: 16,
	}
	wantVideo := videoConfig
	if _, err := muxer.AddVideoStream(&videoConfig); err != nil {
		t.Fatal(err)
	}
	if videoConfig != wantVideo {
		t.Fatalf("AddVideoStream mutated config: got %+v, want %+v", videoConfig, wantVideo)
	}

	audioConfig := AudioStreamConfig{
		Codec:        avcodec.CodecIDAAC,
		SampleFormat: SampleFormatNone,
	}
	wantAudio := audioConfig
	if _, err := muxer.AddAudioStream(&audioConfig); err != nil {
		t.Fatal(err)
	}
	if audioConfig != wantAudio {
		t.Fatalf("AddAudioStream mutated config: got %+v, want %+v", audioConfig, wantAudio)
	}
}

func TestMuxerStreamsReturnsSnapshot(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	muxer, err := NewMuxer(filepath.Join(t.TempDir(), "streams.mp4"), "mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer muxer.Close()

	stream, err := muxer.AddVideoStream(testMuxerVideoConfig())
	if err != nil {
		t.Fatal(err)
	}
	streams := muxer.Streams()
	if len(streams) != 1 || streams[0] != stream {
		t.Fatalf("Streams() = %v, want [%p]", streams, stream)
	}
	streams[0] = nil
	streams = append(streams, nil)
	if len(streams) != 2 {
		t.Fatalf("caller snapshot length = %d, want 2", len(streams))
	}

	got := muxer.Streams()
	if len(got) != 1 || got[0] != stream {
		t.Fatalf("caller mutation changed muxer streams: %v", got)
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
