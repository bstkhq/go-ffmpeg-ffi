//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"context"
	"errors"
	"testing"

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
