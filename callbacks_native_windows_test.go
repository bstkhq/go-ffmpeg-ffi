//go:build windows && (amd64 || arm64)

package ffgo

import (
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/ebitengine/purego"
)

func TestWindowsNativeCallbacksRegisterWithPureGo(t *testing.T) {
	callbacks := []any{
		nativeDecoderInterruptCallback,
		nativeCustomIOReadCallback,
		nativeCustomIOWriteCallback,
		nativeCustomIOSeekCallback,
		nativeLogCallbackTrampoline,
		nativeWrappedBufferFreeCallback,
	}
	for index, callback := range callbacks {
		if pointer := purego.NewCallback(callback); pointer == 0 {
			t.Fatalf("callback %d registered with a zero pointer", index)
		}
	}
}

func TestWindowsNativeCallbackResultsPreserveCBits(t *testing.T) {
	cdecl := purego.CDecl{}
	if got := uint32(nativeDecoderInterruptCallback(cdecl, 0)); got != 1 {
		t.Fatalf("interrupt callback bits = %#x, want %#x", got, uint32(1))
	}
	externalError := int32(avutil.AVERROR_EXTERNAL)
	wantError := uint32(externalError)
	if got := uint32(nativeCustomIOReadCallback(cdecl, 0, nil, 0)); got != wantError {
		t.Fatalf("read callback bits = %#x, want %#x", got, wantError)
	}
	if got := uint32(nativeCustomIOWriteCallback(cdecl, 0, nil, 0)); got != wantError {
		t.Fatalf("write callback bits = %#x, want %#x", got, wantError)
	}
	wantSeekError := uint64(int64(externalError))
	if got := uint64(nativeCustomIOSeekCallback(cdecl, 0, 0, 0)); got != wantSeekError {
		t.Fatalf("seek callback bits = %#x, want %#x", got, wantSeekError)
	}
}
