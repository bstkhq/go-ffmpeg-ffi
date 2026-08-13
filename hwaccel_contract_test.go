//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestTransferHWFrameToSystemPropagatesFailure(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	dst := avutil.FrameAlloc()
	if dst == nil {
		t.Fatal("allocate destination frame")
	}
	defer avutil.FrameFree(&dst)

	want := errors.New("transfer failed")
	calls := 0
	err := transferHWFrameToSystem(dst, nil, func(_, _ avutil.Frame, _ int32) error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("transfer error = %v, want wrapped %v", err, want)
	}
	if calls != 1 {
		t.Fatalf("transfer calls = %d, want 1", calls)
	}
}

func TestTransferHWFrameToSystemRejectsMissingDestination(t *testing.T) {
	called := false
	err := transferHWFrameToSystem(nil, nil, func(_, _ avutil.Frame, _ int32) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("transfer error = %v, want %v", err, ErrOutOfMemory)
	}
	if called {
		t.Fatal("transfer called without a destination frame")
	}
}

func TestHWDeviceAttachRejectsClosedDevice(t *testing.T) {
	device := &HWDevice{closed: true}
	err := device.attachToCodecContext(nil)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("attach error = %v, want %v", err, ErrClosed)
	}
}
