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

type decoderInterruptState struct {
	mu        sync.RWMutex
	operation context.Context
	closed    atomic.Bool
}

type decoderInterrupt struct {
	state  *decoderInterruptState
	lease  *handles.Lease
	handle uintptr
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
	state := &decoderInterruptState{}
	interrupt := &decoderInterrupt{state: state}
	interrupt.lease = handles.RegisterLease(state, state.stop)
	interrupt.handle = interrupt.lease.ID()
	return interrupt
}

func decoderInterruptCallback(_ purego.CDecl, opaque uintptr) (result int32) {
	// A missing or malformed handle is safer to treat as an interruption.
	result = 1
	defer func() {
		if recover() != nil {
			result = 1
		}
	}()
	state, ok := handles.Lookup(opaque).(*decoderInterruptState)
	if !ok || state.interrupted() {
		return result
	}
	return 0
}

func (s *decoderInterrupt) attach(formatCtx avformat.FormatContext) {
	avformat.SetInterruptCallback(formatCtx, decoderInterruptPtr, s.handle)
}

func (s *decoderInterrupt) begin(ctx context.Context) error {
	return s.state.begin(ctx)
}

func (s *decoderInterruptState) begin(ctx context.Context) error {
	if ctx == nil {
		return errors.New("ffgo: context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed.Load() {
		return errDecoderClosed
	}
	s.mu.Lock()
	s.operation = ctx
	s.mu.Unlock()
	return nil
}

func (s *decoderInterrupt) clear() {
	s.state.clear()
}

func (s *decoderInterruptState) clear() {
	s.mu.Lock()
	s.operation = nil
	s.mu.Unlock()
}

func (s *decoderInterrupt) finish(nativeErr error) error {
	return s.state.finish(nativeErr)
}

func (s *decoderInterruptState) finish(nativeErr error) error {
	if err := s.operationError(); err != nil {
		return errors.Join(nativeErr, err)
	}
	return nativeErr
}

func (s *decoderInterruptState) interrupted() bool {
	return s.operationError() != nil
}

func (s *decoderInterruptState) operationError() error {
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
	if s != nil && s.state != nil {
		s.state.stop()
	}
}

func (s *decoderInterruptState) stop() { s.closed.Store(true) }

func (s *decoderInterrupt) release(formatCtx avformat.FormatContext) {
	if s == nil {
		return
	}
	if formatCtx != nil {
		avformat.SetInterruptCallback(formatCtx, 0, 0)
	}
	if s.lease != nil {
		s.lease.Release()
		s.lease = nil
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
	d.interrupt.state.mu.RLock()
	ctx := d.interrupt.state.operation
	d.interrupt.state.mu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
