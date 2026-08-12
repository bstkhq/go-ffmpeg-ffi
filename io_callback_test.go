//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/handles"
	"github.com/ebitengine/purego"
)

func registerTestCustomIO(t *testing.T, callbacks *IOCallbacks) *CustomIOContext {
	t.Helper()
	ctx := &CustomIOContext{callbacks: callbacks}
	ctx.handle = handles.Register(ctx)
	t.Cleanup(func() { handles.Unregister(ctx.handle) })
	return ctx
}

func TestCustomIOReadPreservesCallbackError(t *testing.T) {
	wantErr := errors.New("reader failed")
	ctx := registerTestCustomIO(t, &IOCallbacks{
		Read: func([]byte) (int, error) { return 0, wantErr },
	})
	buffer := make([]byte, 8)

	ctx.beginOperation()
	code := customIOReadCallback(purego.CDecl{}, ctx.handle, &buffer[0], int32(len(buffer)))
	err := ctx.finishOperation(avutil.NewError(code, "read callback"))

	if code != avutil.AVERROR_EXTERNAL {
		t.Fatalf("callback returned %d, want %d", code, avutil.AVERROR_EXTERNAL)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("operation error %v does not preserve %v", err, wantErr)
	}
	if got := avutil.Code(err); got != avutil.AVERROR_EXTERNAL {
		t.Fatalf("FFmpeg error code = %d, want %d", got, avutil.AVERROR_EXTERNAL)
	}
}

func TestCustomIOReadDefersErrorAfterData(t *testing.T) {
	calls := 0
	ctx := registerTestCustomIO(t, &IOCallbacks{
		Read: func(buf []byte) (int, error) {
			calls++
			buf[0] = 42
			return 1, io.EOF
		},
	})
	buffer := make([]byte, 8)

	ctx.beginOperation()
	if code := customIOReadCallback(purego.CDecl{}, ctx.handle, &buffer[0], int32(len(buffer))); code != 1 {
		t.Fatalf("first callback returned %d, want 1", code)
	}
	if code := customIOReadCallback(purego.CDecl{}, ctx.handle, &buffer[0], int32(len(buffer))); code != avutil.AVERROR_EOF {
		t.Fatalf("second callback returned %d, want %d", code, avutil.AVERROR_EOF)
	}
	if calls != 1 {
		t.Fatalf("Read called %d times, want 1", calls)
	}
	if err := ctx.finishOperation(nil); err != nil {
		t.Fatalf("EOF was reported as callback failure: %v", err)
	}
}

func TestCustomIOCallbackRecoversPanic(t *testing.T) {
	wantErr := errors.New("reader panic")
	ctx := registerTestCustomIO(t, &IOCallbacks{
		Read: func([]byte) (int, error) { panic(wantErr) },
	})
	buffer := make([]byte, 8)

	ctx.beginOperation()
	code := customIOReadCallback(purego.CDecl{}, ctx.handle, &buffer[0], int32(len(buffer)))
	err := ctx.finishOperation(avutil.NewError(code, "read callback"))

	if code != avutil.AVERROR_EXTERNAL {
		t.Fatalf("callback returned %d, want %d", code, avutil.AVERROR_EXTERNAL)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("operation error %v does not preserve panic error %v", err, wantErr)
	}
}

func TestCustomIOWriteRejectsShortWrite(t *testing.T) {
	ctx := registerTestCustomIO(t, &IOCallbacks{
		Write: func(buf []byte) (int, error) { return len(buf) - 1, nil },
	})
	buffer := make([]byte, 8)

	ctx.beginOperation()
	code := customIOWriteCallback(purego.CDecl{}, ctx.handle, &buffer[0], int32(len(buffer)))
	err := ctx.finishOperation(avutil.NewError(code, "write callback"))

	if code != avutil.AVERROR_EXTERNAL {
		t.Fatalf("callback returned %d, want %d", code, avutil.AVERROR_EXTERNAL)
	}
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("operation error %v does not preserve io.ErrShortWrite", err)
	}
}

func TestDecoderFromIOPreservesReadError(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	wantErr := errors.New("source unavailable")
	before := handles.Count()

	_, err := NewDecoderFromIO(&IOCallbacks{
		Read: func([]byte) (int, error) { return 0, wantErr },
	}, "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("NewDecoderFromIO error %v does not preserve %v", err, wantErr)
	}
	if got := handles.Count(); got != before {
		t.Fatalf("registered handles = %d, want baseline %d", got, before)
	}
}

func TestEncoderFromIOOwnsContextAndWritesFrames(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	before := handles.Count()
	var output bytes.Buffer
	encoder, err := NewEncoderFromIO(&IOCallbacks{
		WriteContext: func(_ context.Context, data []byte) (int, error) {
			return output.Write(data)
		},
	}, "mpegts", EncoderConfig{
		Width:       16,
		Height:      16,
		PixelFormat: PixelFormatYUV420P,
		CodecID:     avcodec.CodecIDMPEG2VIDEO,
		BitRate:     100_000,
		FrameRate:   10,
	})
	if err != nil {
		t.Fatal(err)
	}

	frame := FrameAlloc()
	if frame.IsNil() {
		_ = encoder.Close()
		t.Fatal("failed to allocate frame")
	}
	defer func() { _ = FrameFree(&frame) }()
	avutil.SetFrameWidth(frame.ptr, 16)
	avutil.SetFrameHeight(frame.ptr, 16)
	avutil.SetFrameFormat(frame.ptr, int32(PixelFormatYUV420P))
	if err := avutil.FrameGetBufferErr(frame.ptr, 0); err != nil {
		_ = encoder.Close()
		t.Fatal(err)
	}
	fillTestFrame(frame, 0, 16, 16)
	if err := encoder.WriteFrame(frame); err != nil {
		_ = encoder.Close()
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("encoder produced no output")
	}
	if got := handles.Count(); got != before {
		t.Fatalf("registered handles = %d, want baseline %d", got, before)
	}
}

func TestDecoderFromIOContextCancelsBlockedOpen(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	before := handles.Count()
	ctx, cancel := context.WithCancel(context.Background())
	readStarted := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		_, err := NewDecoderFromIOContext(ctx, &IOCallbacks{
			ReadContext: func(ctx context.Context, _ []byte) (int, error) {
				close(readStarted)
				<-ctx.Done()
				return 0, ctx.Err()
			},
		}, "mpegts")
		result <- err
	}()

	select {
	case <-readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("FFmpeg did not invoke the read callback")
	}
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("NewDecoderFromIOContext error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled decoder construction remained blocked")
	}
	if got := handles.Count(); got != before {
		t.Fatalf("registered handles = %d, want baseline %d", got, before)
	}
}

func TestCustomIOCloseCancelsActiveCallback(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	readStarted := make(chan struct{})
	ioCtx, err := NewCustomIOContext(&IOCallbacks{
		ReadContext: func(ctx context.Context, _ []byte) (int, error) {
			close(readStarted)
			<-ctx.Done()
			return 0, ctx.Err()
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	ioCtx.beginOperation()
	callbackDone := make(chan int32, 1)
	buffer := make([]byte, 8)
	go func() {
		callbackDone <- customIOReadCallback(purego.CDecl{}, ioCtx.handle, &buffer[0], int32(len(buffer)))
	}()
	select {
	case <-readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("read callback did not start")
	}
	if err := ioCtx.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-callbackDone:
		if code != avutil.AVERROR_EXTERNAL {
			t.Fatalf("callback returned %d, want %d", code, avutil.AVERROR_EXTERNAL)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CustomIOContext.Close did not cancel the callback")
	}
}

func TestCustomIOCancellationStateConcurrentAccess(t *testing.T) {
	ctx := &CustomIOContext{}
	ctx.resetCancellation()

	const iterations = 500
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			ctx.beginOperationContext(context.Background())
			_ = ctx.callbackContext()
			ctx.endOperation()
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			ctx.resetCancellation()
		}
	}()
	go func() {
		defer wg.Done()
		<-start
		for range iterations {
			_ = ctx.callbackContext()
			ctx.cancelPending()
		}
	}()

	close(start)
	wg.Wait()
	ctx.cancelPending()
	ctx.endOperation()
}
