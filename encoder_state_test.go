//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestEncoderCodecStateDrainsAndRetriesInput(t *testing.T) {
	frameStorage := byte(0)
	sendCalls := 0
	receiveResults := []error{
		nil,
		avutil.NewError(avutil.AVERROR_EAGAIN, "receive"),
		nil,
		avutil.NewError(avutil.AVERROR_EAGAIN, "receive"),
	}
	writes := 0
	state := encoderCodecState{
		sendFrame: func(avcodec.Context, avutil.Frame) error {
			sendCalls++
			if sendCalls == 1 {
				return avutil.NewError(avutil.AVERROR_EAGAIN, "send")
			}
			return nil
		},
		receivePacket: func(avcodec.Context, avcodec.Packet) error {
			result := receiveResults[0]
			receiveResults = receiveResults[1:]
			return result
		},
	}

	err := state.encode(nil, avutil.Frame(unsafe.Pointer(&frameStorage)), nil, func(avcodec.Packet) error {
		writes++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sendCalls != 2 || writes != 2 {
		t.Fatalf("sends=%d writes=%d, want 2 and 2", sendCalls, writes)
	}
}

func TestEncoderCodecStateFlushesOnceToEOF(t *testing.T) {
	sendCalls := 0
	receiveResults := []error{
		nil,
		nil,
		avutil.NewError(avutil.AVERROR_EOF, "receive"),
	}
	writes := 0
	state := encoderCodecState{
		sendFrame: func(_ avcodec.Context, frame avutil.Frame) error {
			sendCalls++
			if frame != nil {
				t.Fatalf("flush sent non-nil frame %p", frame)
			}
			return nil
		},
		receivePacket: func(avcodec.Context, avcodec.Packet) error {
			result := receiveResults[0]
			receiveResults = receiveResults[1:]
			return result
		},
	}
	write := func(avcodec.Packet) error {
		writes++
		return nil
	}

	if err := state.encode(nil, nil, nil, write); err != nil {
		t.Fatal(err)
	}
	if err := state.encode(nil, nil, nil, write); err != nil {
		t.Fatal(err)
	}
	if sendCalls != 1 || writes != 2 || !state.drained {
		t.Fatalf("sends=%d writes=%d drained=%v", sendCalls, writes, state.drained)
	}
}

func TestEncoderCodecStateRejectsInputAfterFlush(t *testing.T) {
	frameStorage := byte(0)
	state := encoderCodecState{flushSent: true}
	err := state.encode(nil, avutil.Frame(unsafe.Pointer(&frameStorage)), nil, func(avcodec.Packet) error { return nil })
	if !errors.Is(err, errEncoderFlushed) {
		t.Fatalf("encode error = %v, want %v", err, errEncoderFlushed)
	}
}

func TestEncoderCodecStatePropagatesWriteError(t *testing.T) {
	want := errors.New("write failed")
	frameStorage := byte(0)
	receiveCalls := 0
	state := encoderCodecState{
		sendFrame: func(avcodec.Context, avutil.Frame) error { return nil },
		receivePacket: func(avcodec.Context, avcodec.Packet) error {
			receiveCalls++
			return nil
		},
	}
	err := state.encode(nil, avutil.Frame(unsafe.Pointer(&frameStorage)), nil, func(avcodec.Packet) error {
		return want
	})
	if !errors.Is(err, want) || receiveCalls != 1 {
		t.Fatalf("encode = %v, receives=%d", err, receiveCalls)
	}
}

func TestEncoderCodecStateRejectsImpossibleDoubleEAGAIN(t *testing.T) {
	frameStorage := byte(0)
	state := encoderCodecState{
		sendFrame: func(avcodec.Context, avutil.Frame) error {
			return avutil.NewError(avutil.AVERROR_EAGAIN, "send")
		},
		receivePacket: func(avcodec.Context, avcodec.Packet) error {
			return avutil.NewError(avutil.AVERROR_EAGAIN, "receive")
		},
	}
	err := state.encode(nil, avutil.Frame(unsafe.Pointer(&frameStorage)), nil, func(avcodec.Packet) error { return nil })
	if !errors.Is(err, errEncoderProtocolStalled) {
		t.Fatalf("encode error = %v, want %v", err, errEncoderProtocolStalled)
	}
}
