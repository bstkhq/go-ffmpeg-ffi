//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"bytes"
	"errors"
	"io"
	"testing"

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
	encoder, err := NewEncoderToWriter(&output, "mpegts", EncoderConfig{
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
