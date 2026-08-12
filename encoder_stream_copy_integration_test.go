//go:build !ios && (amd64 || arm64)

package ffgo

import (
	"path/filepath"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestEncoderStreamCopyMapsNonzeroVideoIndex(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	sourcePath := filepath.Join(t.TempDir(), "audio-first.mkv")
	writeAudioFirstVideoFixture(t, sourcePath)

	for _, tt := range []struct {
		name      string
		copyAudio bool
	}{
		{name: "video and audio", copyAudio: true},
		{name: "video only", copyAudio: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewDecoder(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()

			video := source.VideoStream()
			audio := source.AudioStream()
			if video == nil || audio == nil {
				t.Fatalf("source streams = video %v, audio %v; want both", video, audio)
			}
			if video.Index != 1 || audio.Index != 0 {
				t.Fatalf("source indices = video %d, audio %d; want video 1, audio 0", video.Index, audio.Index)
			}

			var copiedAudio *StreamInfo
			if tt.copyAudio {
				copiedAudio = audio
			}
			outputPath := filepath.Join(t.TempDir(), "copy.mkv")
			output, err := NewEncoderWithOptions(outputPath, &EncoderOptions{
				CopyVideo:     true,
				CopyAudio:     tt.copyAudio,
				SourceStreams: NewStreamCopySource(video, copiedAudio),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()

			writtenVideoPackets := copySourcePackets(t, source, output, video.Index)
			if writtenVideoPackets == 0 {
				_ = output.Close()
				t.Fatal("source contained no video packets")
			}
			if err := output.Close(); err != nil {
				t.Fatal(err)
			}

			decoded, err := NewDecoder(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()

			outputVideo := decoded.VideoStream()
			outputAudio := decoded.AudioStream()
			if outputVideo == nil {
				t.Fatal("output has no video stream")
			}
			if tt.copyAudio != (outputAudio != nil) {
				t.Fatalf("output audio stream present = %t, want %t", outputAudio != nil, tt.copyAudio)
			}

			var videoPackets, audioPackets int
			for {
				packet, err := decoded.ReadPacket()
				if err != nil {
					t.Fatal(err)
				}
				if packet == nil {
					break
				}
				if packet.StreamIndex() == outputVideo.Index {
					videoPackets++
				} else if outputAudio != nil && packet.StreamIndex() == outputAudio.Index {
					audioPackets++
				}
			}
			if videoPackets != writtenVideoPackets {
				t.Fatalf("copied video packets = %d, want %d", videoPackets, writtenVideoPackets)
			}
			if audioPackets != 0 {
				t.Fatalf("copied audio packets = %d, want 0", audioPackets)
			}
		})
	}
}

func copySourcePackets(t *testing.T, source *Decoder, output *Encoder, videoStreamIndex int) int {
	t.Helper()

	videoPackets := 0
	for {
		packet, err := source.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if packet == nil {
			return videoPackets
		}

		streamIndex := packet.StreamIndex()
		pts := packet.PTS()
		dts := packet.DTS()
		duration := avcodec.GetPacketDuration(packet.ptr)
		size := packet.Size()
		if streamIndex == videoStreamIndex {
			videoPackets++
		}

		if err := output.WritePacket(packet); err != nil {
			t.Fatal(err)
		}
		if packet.StreamIndex() != streamIndex || packet.PTS() != pts || packet.DTS() != dts ||
			avcodec.GetPacketDuration(packet.ptr) != duration || packet.Size() != size {
			t.Fatal("WritePacket mutated a borrowed source packet")
		}
	}
}

func writeAudioFirstVideoFixture(t *testing.T, path string) {
	t.Helper()

	muxer, err := NewMuxer(path, "matroska")
	if err != nil {
		t.Fatal(err)
	}
	defer muxer.Close()

	audio, err := muxer.AddAudioStream(&AudioStreamConfig{
		Codec:        CodecIDAAC,
		SampleRate:   48_000,
		Channels:     2,
		SampleFormat: SampleFormatFltP,
		BitRate:      64_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	video, err := muxer.AddVideoStream(&VideoStreamConfig{
		Codec:       avcodec.CodecIDMPEG4,
		Width:       16,
		Height:      16,
		PixelFormat: PixelFormatYUV420P,
		FrameRate:   10,
		BitRate:     100_000,
		GOPSize:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if audio.Index() != 0 || video.Index() != 1 {
		t.Fatalf("fixture indices = audio %d, video %d; want audio 0, video 1", audio.Index(), video.Index())
	}
	if err := muxer.WriteHeader(); err != nil {
		t.Fatal(err)
	}

	frame := FrameAlloc()
	if frame.IsNil() {
		t.Fatal("failed to allocate fixture frame")
	}
	defer func() { _ = FrameFree(&frame) }()
	avutil.SetFrameWidth(frame.ptr, 16)
	avutil.SetFrameHeight(frame.ptr, 16)
	avutil.SetFrameFormat(frame.ptr, int32(PixelFormatYUV420P))
	if err := avutil.FrameGetBufferErr(frame.ptr, 0); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if err := avutil.FrameMakeWritable(frame.ptr); err != nil {
			t.Fatal(err)
		}
		fillTestFrame(frame, i, 16, 16)
		avutil.SetFramePTS(frame.ptr, int64(i))
		if err := muxer.WriteFrame(video, frame); err != nil {
			t.Fatal(err)
		}
	}
	if err := muxer.WriteTrailer(); err != nil {
		t.Fatal(err)
	}
}
