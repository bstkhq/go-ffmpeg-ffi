//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/handles"
	"github.com/ebitengine/purego"
)

var errDecoderClosed = errors.New("ffgo: decoder is closed")

type decoderInterrupt struct {
	mu        sync.RWMutex
	operation context.Context
	handle    uintptr
	closed    atomic.Bool
}

var (
	decoderInterruptOnce sync.Once
	decoderInterruptPtr  uintptr
)

func initDecoderInterruptCallback() {
	decoderInterruptOnce.Do(func() {
		decoderInterruptPtr = purego.NewCallback(decoderInterruptCallback)
	})
}

func newDecoderInterrupt() *decoderInterrupt {
	initDecoderInterruptCallback()
	state := &decoderInterrupt{}
	state.handle = handles.Register(state)
	return state
}

func decoderInterruptCallback(_ purego.CDecl, opaque uintptr) (result int32) {
	// A missing or malformed handle is safer to treat as an interruption.
	result = 1
	defer func() {
		if recover() != nil {
			result = 1
		}
	}()
	state, ok := handles.Lookup(opaque).(*decoderInterrupt)
	if !ok || state.interrupted() {
		return result
	}
	return 0
}

func (s *decoderInterrupt) attach(formatCtx avformat.FormatContext) {
	avformat.SetInterruptCallback(formatCtx, decoderInterruptPtr, s.handle)
}

func (s *decoderInterrupt) begin(ctx context.Context) error {
	if ctx == nil {
		return errors.New("ffgo: context cannot be nil")
	}
	if s.closed.Load() {
		return errDecoderClosed
	}
	s.mu.Lock()
	s.operation = ctx
	s.mu.Unlock()
	return ctx.Err()
}

func (s *decoderInterrupt) clear() {
	s.mu.Lock()
	s.operation = nil
	s.mu.Unlock()
}

func (s *decoderInterrupt) finish(nativeErr error) error {
	if err := s.operationError(); err != nil {
		return errors.Join(nativeErr, err)
	}
	return nativeErr
}

func (s *decoderInterrupt) interrupted() bool {
	return s.operationError() != nil
}

func (s *decoderInterrupt) operationError() error {
	if s.closed.Load() {
		return errDecoderClosed
	}
	s.mu.RLock()
	ctx := s.operation
	s.mu.RUnlock()
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func (s *decoderInterrupt) stop() {
	if s != nil {
		s.closed.Store(true)
	}
}

func (s *decoderInterrupt) release(formatCtx avformat.FormatContext) {
	if s == nil {
		return
	}
	if formatCtx != nil {
		avformat.SetInterruptCallback(formatCtx, 0, 0)
	}
	if s.handle != 0 {
		handles.Unregister(s.handle)
		s.handle = 0
	}
}

func (d *Decoder) beginInterrupt(ctx context.Context) error {
	if ctx == nil {
		return errors.New("ffgo: context cannot be nil")
	}
	if d.interrupt == nil {
		return ctx.Err()
	}
	return d.interrupt.begin(ctx)
}

func (d *Decoder) clearInterrupt() {
	if d.interrupt != nil {
		d.interrupt.clear()
	}
}

func (d *Decoder) finishInterrupt(err error) error {
	if d.interrupt == nil {
		return err
	}
	return d.interrupt.finish(err)
}

func (d *Decoder) interruptContext() context.Context {
	if d.interrupt == nil {
		return context.Background()
	}
	d.interrupt.mu.RLock()
	ctx := d.interrupt.operation
	d.interrupt.mu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
