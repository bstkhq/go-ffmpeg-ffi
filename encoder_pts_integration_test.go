//go:build amd64 || arm64

package ffmpeg

import (
	"path/filepath"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestEncoderVideoFramePTSContract(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	tests := []struct {
		name string
		pts  int64
		want int64
	}{
		{name: "preserves assigned PTS", pts: 37, want: 37},
		{name: "restores missing PTS", pts: avutil.AV_NOPTS_VALUE, want: avutil.AV_NOPTS_VALUE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder, err := NewEncoder(filepath.Join(t.TempDir(), "pts.mkv"), &EncoderOptions{
				Video: &VideoEncoderConfig{
					Width:       16,
					Height:      16,
					PixelFormat: PixelFormatYUV420P,
					Codec:       avcodec.CodecIDMPEG4,
					Bitrate:     100_000,
					FrameRate:   NewRational(10, 1),
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()

			frame := FrameAlloc()
			if frame.IsNil() {
				t.Fatal("failed to allocate frame")
			}
			defer func() { _ = FrameFree(&frame) }()
			avutil.SetFrameWidth(frame.ptr, 16)
			avutil.SetFrameHeight(frame.ptr, 16)
			avutil.SetFrameFormat(frame.ptr, int32(PixelFormatYUV420P))
			if err := avutil.FrameGetBufferErr(frame.ptr, 0); err != nil {
				t.Fatal(err)
			}
			fillTestFrame(frame, 0, 16, 16)
			avutil.SetFramePTS(frame.ptr, tt.pts)

			if err := encoder.WriteFrame(frame); err != nil {
				t.Fatal(err)
			}
			if got := avutil.GetFramePTS(frame.ptr); got != tt.want {
				t.Fatalf("frame PTS = %d, want %d", got, tt.want)
			}
			if encoder.FrameCount() != 1 {
				t.Fatalf("encoded frame count = %d, want 1", encoder.FrameCount())
			}
		})
	}
}

func TestEncoderAudioFramePTSContract(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	tests := []struct {
		name string
		pts  int64
		want int64
	}{
		{name: "preserves assigned PTS", pts: 4800, want: 4800},
		{name: "restores missing PTS", pts: avutil.AV_NOPTS_VALUE, want: avutil.AV_NOPTS_VALUE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoder, err := NewEncoder(filepath.Join(t.TempDir(), "pts.mkv"), &EncoderOptions{
				Video: &VideoEncoderConfig{
					Codec:       avcodec.CodecIDMPEG4,
					Width:       16,
					Height:      16,
					FrameRate:   NewRational(10, 1),
					Bitrate:     100_000,
					PixelFormat: PixelFormatYUV420P,
				},
				Audio: &AudioEncoderConfig{
					Codec:      avcodec.CodecIDAAC,
					SampleRate: 48_000,
					Channels:   2,
					Bitrate:    128_000,
				},
			})

			if err != nil {
				t.Fatal(err)
			}
			defer encoder.Close()

			frame := FrameAlloc()
			if frame.IsNil() {
				t.Fatal("failed to allocate frame")
			}
			defer func() { _ = FrameFree(&frame) }()
			avutil.SetFrameFormat(frame.ptr, int32(encoder.AudioSampleFormat()))
			avutil.SetFrameNbSamples(frame.ptr, int32(encoder.AudioFrameSize()))
			avutil.SetFrameSampleRate(frame.ptr, int32(encoder.SampleRate()))
			avutil.FrameSetChannels(frame.ptr, int32(encoder.Channels()))
			if err := avutil.FrameGetBufferErr(frame.ptr, 0); err != nil {
				t.Fatal(err)
			}
			fillSilentPlanarAudio(frame, encoder.Channels())
			avutil.SetFramePTS(frame.ptr, tt.pts)

			if err := encoder.WriteAudioFrame(frame); err != nil {
				t.Fatal(err)
			}
			if got := avutil.GetFramePTS(frame.ptr); got != tt.want {
				t.Fatalf("frame PTS = %d, want %d", got, tt.want)
			}
		})
	}
}
