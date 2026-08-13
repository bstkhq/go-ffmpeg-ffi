//go:build amd64 || arm64

package ffgo

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/handles"
	"github.com/ebitengine/purego"
)

func TestDecoderInterruptUsesOperationContext(t *testing.T) {
	state := newDecoderInterrupt()
	defer state.release(nil)
	ctx, cancel := context.WithCancel(context.Background())
	if err := state.begin(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()

	if got := decoderInterruptCallback(purego.CDecl{}, state.handle); got != 1 {
		t.Fatalf("interrupt callback = %d, want 1", got)
	}
	wantNative := errors.New("native operation interrupted")
	if err := state.finish(wantNative); !errors.Is(err, context.Canceled) || !errors.Is(err, wantNative) {
		t.Fatalf("finish error %v does not preserve both causes", err)
	}
}

func TestDecoderInterruptLeaseReleasesAbandonedHandle(t *testing.T) {
	baseline := handles.Count()
	handle, state := abandonDecoderInterrupt()
	waitForHandleRelease(t, handle)

	deadline := time.Now().Add(time.Second)
	for !state.closed.Load() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !state.closed.Load() {
		t.Fatal("abandoned interrupt state was not stopped")
	}
	if got := handles.Count(); got != baseline {
		t.Fatalf("registered handles = %d, want baseline %d", got, baseline)
	}
	if got := decoderInterruptCallback(purego.CDecl{}, handle); got != 1 {
		t.Fatalf("interrupt callback after release = %d, want 1", got)
	}
}

func abandonDecoderInterrupt() (uintptr, *decoderInterruptState) {
	interrupt := newDecoderInterrupt()
	handle := interrupt.handle
	state := interrupt.state
	runtime.KeepAlive(interrupt)
	return handle, state
}

func waitForHandleRelease(t *testing.T, handle uintptr) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for handles.Lookup(handle) != nil && time.Now().Before(deadline) {
		runtime.GC()
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
	if got := handles.Lookup(handle); got != nil {
		handles.Unregister(handle)
		t.Fatalf("handle %d remained registered after garbage collection", handle)
	}
}
