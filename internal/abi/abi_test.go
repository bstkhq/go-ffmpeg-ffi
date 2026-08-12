//go:build !ios && !android && (amd64 || arm64)

package abi

import (
	"errors"
	"testing"
)

func version(major, minor, patch uint32) uint32 {
	return major<<16 | minor<<8 | patch
}

func TestDetectSupportedLayouts(t *testing.T) {
	tests := []struct {
		name                                                             string
		versions                                                         [3]uint32
		ffmpeg, duration, interruptCallback, frameSampleRate, frameFlags uintptr
		frameLegacyKeyFrame                                              uintptr
		codecParFormat, codecParSampleRate, channelLayout                uintptr
		codecWidth, codecPixelFormat, subtitleType, subtitleFlags        uintptr
	}{
		{
			name: "FFmpeg 6", versions: [3]uint32{version(58, 29, 100), version(60, 31, 102), version(60, 16, 100)},
			ffmpeg: 6, duration: 72, interruptCallback: 200, frameSampleRate: 208, frameFlags: 316,
			frameLegacyKeyFrame: 120,
			codecParFormat:      28, codecParSampleRate: 116, channelLayout: 912,
			codecWidth: 116, codecPixelFormat: 136, subtitleType: 72, subtitleFlags: 96,
		},
		{
			name: "FFmpeg 7", versions: [3]uint32{version(59, 39, 100), version(61, 19, 100), version(61, 7, 100)},
			ffmpeg: 7, duration: 104, interruptCallback: 216, frameSampleRate: 192, frameFlags: 292,
			codecParFormat: 44, codecParSampleRate: 152, channelLayout: 352,
			codecWidth: 116, codecPixelFormat: 140, subtitleType: 76, subtitleFlags: 72,
		},
		{
			name: "FFmpeg 8", versions: [3]uint32{version(60, 8, 100), version(62, 11, 100), version(62, 3, 100)},
			ffmpeg: 8, duration: 104, interruptCallback: 216, frameSampleRate: 180, frameFlags: 276,
			codecParFormat: 44, codecParSampleRate: 152, channelLayout: 352,
			codecWidth: 112, codecPixelFormat: 136, subtitleType: 76, subtitleFlags: 72,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout, err := Detect(tt.versions[0], tt.versions[1], tt.versions[2])
			if err != nil {
				t.Fatal(err)
			}
			if uintptr(layout.FFmpegMajor) != tt.ffmpeg {
				t.Fatalf("FFmpeg major = %d, want %d", layout.FFmpegMajor, tt.ffmpeg)
			}
			if layout.FormatContext.Duration != tt.duration {
				t.Fatalf("duration offset = %d, want %d", layout.FormatContext.Duration, tt.duration)
			}
			if layout.FormatContext.InterruptCallback != tt.interruptCallback {
				t.Fatalf("interrupt_callback offset = %d, want %d", layout.FormatContext.InterruptCallback, tt.interruptCallback)
			}
			if layout.IOContext.Buffer != 8 {
				t.Fatalf("AVIOContext buffer offset = %d, want 8", layout.IOContext.Buffer)
			}
			if layout.Frame.SampleRate != tt.frameSampleRate {
				t.Fatalf("frame sample_rate offset = %d, want %d", layout.Frame.SampleRate, tt.frameSampleRate)
			}
			if layout.Frame.Flags != tt.frameFlags {
				t.Fatalf("frame flags offset = %d, want %d", layout.Frame.Flags, tt.frameFlags)
			}
			if layout.Frame.LegacyKeyFrame != tt.frameLegacyKeyFrame {
				t.Fatalf("frame key_frame offset = %d, want %d", layout.Frame.LegacyKeyFrame, tt.frameLegacyKeyFrame)
			}
			if layout.CodecParameters.Format != tt.codecParFormat {
				t.Fatalf("codecpar format offset = %d, want %d", layout.CodecParameters.Format, tt.codecParFormat)
			}
			if layout.CodecParameters.SampleRate != tt.codecParSampleRate {
				t.Fatalf("codecpar sample_rate offset = %d, want %d", layout.CodecParameters.SampleRate, tt.codecParSampleRate)
			}
			if layout.CodecContext.ChannelLayout != tt.channelLayout {
				t.Fatalf("codec ch_layout offset = %d, want %d", layout.CodecContext.ChannelLayout, tt.channelLayout)
			}
			if layout.CodecContext.Width != tt.codecWidth {
				t.Fatalf("codec width offset = %d, want %d", layout.CodecContext.Width, tt.codecWidth)
			}
			if layout.CodecContext.PixelFormat != tt.codecPixelFormat {
				t.Fatalf("codec pix_fmt offset = %d, want %d", layout.CodecContext.PixelFormat, tt.codecPixelFormat)
			}
			if layout.SubtitleRect.Type != tt.subtitleType {
				t.Fatalf("subtitle type offset = %d, want %d", layout.SubtitleRect.Type, tt.subtitleType)
			}
			if layout.SubtitleRect.Flags != tt.subtitleFlags {
				t.Fatalf("subtitle flags offset = %d, want %d", layout.SubtitleRect.Flags, tt.subtitleFlags)
			}
			if layout.Codec.Name != 0 {
				t.Fatalf("codec name offset = %d, want 0", layout.Codec.Name)
			}
		})
	}
}

func TestDetectRejectsMixedLibraries(t *testing.T) {
	_, err := Detect(version(59, 39, 100), version(60, 31, 102), version(61, 7, 100))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Detect() error = %v, want ErrUnsupported", err)
	}
}

func TestDetectRejectsUnknownMajor(t *testing.T) {
	_, err := Detect(version(61, 1, 0), version(63, 1, 0), version(63, 1, 0))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Detect() error = %v, want ErrUnsupported", err)
	}
}

func TestSupportedPreferenceOrder(t *testing.T) {
	got := Supported()
	if len(got) != 3 {
		t.Fatalf("Supported() returned %d layouts, want 3", len(got))
	}
	for i, want := range []int{8, 7, 6} {
		if got[i].FFmpegMajor != want {
			t.Fatalf("Supported()[%d] = FFmpeg %d, want %d", i, got[i].FFmpegMajor, want)
		}
	}
}

func TestLibraryMajor(t *testing.T) {
	layout, err := Detect(version(59, 39, 100), version(61, 19, 100), version(61, 7, 100))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := layout.LibraryMajor("swresample"); !ok || got != 5 {
		t.Fatalf("LibraryMajor(swresample) = %d, %v; want 5, true", got, ok)
	}
	if _, ok := layout.LibraryMajor("unknown"); ok {
		t.Fatal("LibraryMajor(unknown) unexpectedly succeeded")
	}
}
