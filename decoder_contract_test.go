//go:build amd64 || arm64

package ffgo

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestDecoderFrameMethodsReturnStableEOF(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	input := createTestVideo(t)

	t.Run("video", func(t *testing.T) {
		decoder, err := NewDecoder(input)
		if err != nil {
			t.Fatal(err)
		}
		defer decoder.Close()

		frames := 0
		for {
			frame, err := decoder.DecodeVideo()
			if errors.Is(err, io.EOF) {
				if !frame.IsNil() {
					t.Fatal("DecodeVideo returned a frame with io.EOF")
				}
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if frame.IsNil() {
				t.Fatal("DecodeVideo returned an empty frame without io.EOF")
			}
			frames++
		}
		if frames != 60 {
			t.Fatalf("video frames = %d, want 60", frames)
		}
		if frame, err := decoder.DecodeVideoCopy(); !errors.Is(err, io.EOF) || !frame.IsNil() {
			t.Fatalf("DecodeVideoCopy after drain = (nil=%v, err=%v), want io.EOF", frame.IsNil(), err)
		}
	})

	t.Run("audio", func(t *testing.T) {
		decoder, err := NewDecoder(input)
		if err != nil {
			t.Fatal(err)
		}
		defer decoder.Close()

		frames := 0
		for {
			frame, err := decoder.DecodeAudioContext(context.Background())
			if errors.Is(err, io.EOF) {
				if !frame.IsNil() {
					t.Fatal("DecodeAudioContext returned a frame with io.EOF")
				}
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if frame.IsNil() {
				t.Fatal("DecodeAudioContext returned an empty frame without io.EOF")
			}
			frames++
		}
		if frames != 87 {
			t.Fatalf("audio frames = %d, want 87", frames)
		}
		if frame, err := decoder.DecodeAudioPacketCopy(nil); !errors.Is(err, io.EOF) || !frame.IsNil() {
			t.Fatalf("DecodeAudioPacketCopy after drain = (nil=%v, err=%v), want io.EOF", frame.IsNil(), err)
		}
	})

	t.Run("interleaved", func(t *testing.T) {
		decoder, err := NewDecoder(input)
		if err != nil {
			t.Fatal(err)
		}
		defer decoder.Close()

		frames := 0
		for {
			frame, err := decoder.ReadFrameContext(context.Background())
			if errors.Is(err, io.EOF) {
				if frame != nil {
					t.Fatal("ReadFrameContext returned a frame with io.EOF")
				}
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if frame == nil {
				t.Fatal("ReadFrameContext returned nil without io.EOF")
			}
			frames++
		}
		if frames != 147 {
			t.Fatalf("interleaved frames = %d, want 147", frames)
		}
		if frame, err := decoder.ReadFrameCopy(); !errors.Is(err, io.EOF) || frame != nil {
			t.Fatalf("ReadFrameCopy after drain = (frame=%v, err=%v), want io.EOF", frame, err)
		}
	})
}

func TestDecodeVideoPacketReturnsStableEOF(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	decoder, err := NewDecoder(createTestVideo(t))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	if err := decoder.OpenVideoDecoder(); err != nil {
		t.Fatal(err)
	}

	frames := 0
	for {
		packet, err := decoder.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if packet == nil {
			break
		}
		if packet.StreamIndex() != decoder.videoStreamIdx {
			continue
		}
		frame, err := decoder.DecodeVideoPacket(packet)
		if err != nil {
			t.Fatal(err)
		}
		if !frame.IsNil() {
			frames++
		}
	}
	for {
		frame, err := decoder.DecodeVideoPacket(nil)
		if errors.Is(err, io.EOF) {
			if !frame.IsNil() {
				t.Fatal("DecodeVideoPacket returned a frame with io.EOF")
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !frame.IsNil() {
			frames++
		}
	}
	if frames != 60 {
		t.Fatalf("packet-decoded frames = %d, want 60", frames)
	}
	if frame, err := decoder.DecodeVideoPacket(nil); !errors.Is(err, io.EOF) || !frame.IsNil() {
		t.Fatalf("repeated packet EOF = (nil=%v, err=%v), want io.EOF", frame.IsNil(), err)
	}
}
