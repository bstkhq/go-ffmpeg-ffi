//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestDecoderCodecStateRetainsInputUntilAccepted(t *testing.T) {
	packet := avcodec.Packet(unsafe.Pointer(uintptr(1)))
	receiveCalls := 0
	sendCalls := 0
	freeCalls := 0
	state := decoderCodecState{
		receiveFrame: func(avcodec.Context, avutil.Frame) error {
			receiveCalls++
			if receiveCalls == 1 || receiveCalls == 3 {
				return nil
			}
			return avutil.NewError(avutil.AVERROR_EAGAIN, "receive")
		},
		sendPacket: func(_ avcodec.Context, got avcodec.Packet) error {
			sendCalls++
			if got != packet {
				t.Fatalf("sent packet = %p, want %p", got, packet)
			}
			return nil
		},
		freePacket: func(got *avcodec.Packet) {
			freeCalls++
			*got = nil
		},
	}
	if err := state.enqueueOwned(packet); err != nil {
		t.Fatal(err)
	}

	ready, err := state.next(nil, nil)
	if err != nil || !ready {
		t.Fatalf("first next = (%v, %v), want ready frame", ready, err)
	}
	if sendCalls != 0 || freeCalls != 0 || !state.hasPending() {
		t.Fatalf("packet was consumed before pending output drained")
	}

	ready, err = state.next(nil, nil)
	if err != nil || !ready {
		t.Fatalf("second next = (%v, %v), want ready frame", ready, err)
	}
	if sendCalls != 1 || freeCalls != 1 || state.hasPending() {
		t.Fatalf("accepted packet: sends=%d frees=%d pending=%v", sendCalls, freeCalls, state.hasPending())
	}
}

func TestDecoderCodecStateFlushesOnceAndDrainsToEOF(t *testing.T) {
	receiveResults := []error{
		avutil.NewError(avutil.AVERROR_EAGAIN, "receive"),
		nil,
		nil,
		avutil.NewError(avutil.AVERROR_EOF, "receive"),
	}
	sendCalls := 0
	state := decoderCodecState{
		receiveFrame: func(avcodec.Context, avutil.Frame) error {
			result := receiveResults[0]
			receiveResults = receiveResults[1:]
			return result
		},
		sendPacket: func(_ avcodec.Context, packet avcodec.Packet) error {
			sendCalls++
			if packet != nil {
				t.Fatalf("flush sent non-nil packet %p", packet)
			}
			return nil
		},
	}
	state.requestFlush()

	for i := 0; i < 2; i++ {
		ready, err := state.next(nil, nil)
		if err != nil || !ready {
			t.Fatalf("next %d = (%v, %v), want ready frame", i, ready, err)
		}
	}
	ready, err := state.next(nil, nil)
	if err != nil || ready || !state.drained {
		t.Fatalf("final next = (%v, %v), drained=%v", ready, err, state.drained)
	}
	if sendCalls != 1 {
		t.Fatalf("flush sends = %d, want 1", sendCalls)
	}
}

func TestDecoderCodecStateRejectsImpossibleDoubleEAGAIN(t *testing.T) {
	state := decoderCodecState{
		receiveFrame: func(avcodec.Context, avutil.Frame) error {
			return avutil.NewError(avutil.AVERROR_EAGAIN, "receive")
		},
		sendPacket: func(avcodec.Context, avcodec.Packet) error {
			return avutil.NewError(avutil.AVERROR_EAGAIN, "send")
		},
		freePacket: func(packet *avcodec.Packet) { *packet = nil },
	}
	if err := state.enqueueOwned(avcodec.Packet(unsafe.Pointer(uintptr(1)))); err != nil {
		t.Fatal(err)
	}

	_, err := state.next(nil, nil)
	if !errors.Is(err, errDecoderProtocolStalled) {
		t.Fatalf("next error = %v, want %v", err, errDecoderProtocolStalled)
	}
}

func TestDecoderCodecStateResetReleasesPendingInput(t *testing.T) {
	freeCalls := 0
	state := decoderCodecState{
		freePacket: func(packet *avcodec.Packet) {
			freeCalls++
			*packet = nil
		},
	}
	for i := uintptr(1); i <= 2; i++ {
		if err := state.enqueueOwned(avcodec.Packet(unsafe.Pointer(i))); err != nil {
			t.Fatal(err)
		}
	}
	state.requestFlush()
	state.reset()

	if freeCalls != 2 || state.hasPending() || state.flushRequested || state.flushSent || state.drained {
		t.Fatalf("reset: frees=%d pending=%v flush=%v/%v drained=%v",
			freeCalls, state.hasPending(), state.flushRequested, state.flushSent, state.drained)
	}
}
