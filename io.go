//go:build amd64 || arm64

package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/handles"
	"github.com/ebitengine/purego"
)

// IOCallbacks provides custom I/O operations for reading and writing media.
// Callback implementations must be safe to invoke from FFmpeg-owned threads.
type IOCallbacks struct {
	// ReadContext is the cancellation-aware form of Read. When set, it is
	// preferred over Read and receives the context for the active FFmpeg operation.
	ReadContext func(ctx context.Context, buf []byte) (int, error)

	// Read reads up to len(buf) bytes into buf.
	// Returns the number of bytes read and any error encountered.
	// At end of file, returns 0, io.EOF.
	//
	// The callback may be invoked synchronously by decoder constructors while
	// FFmpeg probes the input. For live/channel-backed sources, block until
	// demuxable bytes are available or return io.EOF when the source is closed.
	// Do not return 0, nil to mean "no data yet"; that gives FFmpeg no forward
	// progress during probing.
	//
	// Read cannot be force-canceled while it is blocked. Use ReadContext for
	// sources that must unblock promptly when an operation or decoder is closed.
	Read func(buf []byte) (int, error)

	// WriteContext is the cancellation-aware form of Write. When set, it is
	// preferred over Write and receives the context for the active FFmpeg operation.
	WriteContext func(ctx context.Context, buf []byte) (int, error)

	// Write writes len(buf) bytes from buf.
	// Returns the number of bytes written and any error encountered.
	// Use WriteContext when a blocked write must support cancellation.
	Write func(buf []byte) (int, error)

	// SeekContext is the cancellation-aware form of Seek. When set, it is
	// preferred over Seek and receives the context for the active FFmpeg operation.
	SeekContext func(ctx context.Context, offset int64, whence int) (int64, error)

	// Seek seeks to the given offset.
	// whence: 0 = SEEK_SET, 1 = SEEK_CUR, 2 = SEEK_END
	// Returns the new offset and any error encountered.
	// Use SeekContext when a blocked seek must support cancellation.
	Seek func(offset int64, whence int) (int64, error)
}

// CustomIOContext wraps an AVIOContext with custom callbacks.
type CustomIOContext struct {
	mu sync.Mutex
	*customIOState
	avioCtx avformat.IOContext
	handle  uintptr
	lease   *handles.Lease
	closed  bool
}

// customIOState contains only the Go state needed by native callbacks. The
// handle registry retains this state rather than its owning CustomIOContext,
// allowing an abandoned registration lease to be finalized.
type customIOState struct {
	errorMu        sync.Mutex
	contextMu      sync.RWMutex
	callbackMu     sync.Mutex
	callbackCond   *sync.Cond
	activeCallback int
	callbacksDone  bool
	callbacks      *IOCallbacks
	callbackErr    error
	pendingReadErr error
	lifetimeCtx    context.Context
	lifetimeCancel context.CancelFunc
	operationCtx   context.Context
	operationEnd   context.CancelFunc
	stopLifetime   func() bool
}

// Default buffer size for custom I/O (32KB)
const defaultIOBufferSize = 32 * 1024

const (
	avSeekSize  int32 = 0x10000
	avSeekForce int32 = 0x20000
)

// Pre-registered callbacks to avoid hitting purego's callback limit.
// These are registered once and reused across all CustomIOContext instances.
var (
	ioCallbacksOnce    sync.Once
	readCallbackPtr    uintptr
	writeCallbackPtr   uintptr
	seekCallbackPtr    uintptr
	ioCallbacksInitErr error
)

type ioCallbackPointers struct {
	read  uintptr
	write uintptr
	seek  uintptr
}

func initIOCallbacks() error {
	ioCallbacksOnce.Do(func() {
		callbacks, err := newIOCallbackPointers(purego.NewCallback)
		if err != nil {
			ioCallbacksInitErr = err
			return
		}
		readCallbackPtr = callbacks.read
		writeCallbackPtr = callbacks.write
		seekCallbackPtr = callbacks.seek
	})

	return ioCallbacksInitErr
}

func newIOCallbackPointers(newCallback func(any) uintptr) (callbacks ioCallbackPointers, err error) {
	defer func() {
		if value := recover(); value != nil {
			callbacks = ioCallbackPointers{}
			err = fmt.Errorf("ffmpeg: initialize custom I/O callbacks: %w", callbackPanicError(value))
		}
	}()

	// Read callback: int read_packet(void *opaque, uint8_t *buf, int buf_size)
	callbacks.read = newCallback(nativeCustomIOReadCallback)

	// Write callback: int write_packet(void *opaque, uint8_t *buf, int buf_size)
	callbacks.write = newCallback(nativeCustomIOWriteCallback)

	// Seek callback: int64_t seek(void *opaque, int64_t offset, int whence)
	callbacks.seek = newCallback(nativeCustomIOSeekCallback)

	return callbacks, nil
}

func lookupCustomIOState(opaque uintptr) *customIOState {
	state, _ := handles.Lookup(opaque).(*customIOState)
	return state
}

func customIOReadCallback(_ purego.CDecl, opaque uintptr, buf *byte, bufSize int32) (result int32) {
	result = avutil.AVERROR_EXTERNAL
	state := lookupCustomIOState(opaque)
	if state == nil {
		return result
	}
	defer func() {
		if value := recover(); value != nil {
			state.recordCallbackError("read", callbackPanicError(value))
			result = avutil.AVERROR_EXTERNAL
		}
	}()
	if !state.beginCallback() {
		return result
	}
	defer state.endCallback()

	if err := state.takePendingReadError(); err != nil {
		if errors.Is(err, io.EOF) {
			return avutil.AVERROR_EOF
		}
		state.recordCallbackError("read", err)
		return result
	}
	if buf == nil || bufSize <= 0 {
		state.recordCallbackError("read", errors.New("invalid destination buffer"))
		return result
	}
	if state.callbacks == nil || (state.callbacks.ReadContext == nil && state.callbacks.Read == nil) {
		state.recordCallbackError("read", errors.New("callback is not configured"))
		return result
	}

	goBuffer := unsafe.Slice(buf, int(bufSize))
	var n int
	var err error
	if state.callbacks.ReadContext != nil {
		n, err = state.callbacks.ReadContext(state.callbackContext(), goBuffer)
	} else {
		n, err = state.callbacks.Read(goBuffer)
	}
	if n < 0 || n > int(bufSize) {
		state.recordCallbackError("read", errors.Join(errors.New("invalid byte count"), err))
		return result
	}
	if n > 0 {
		if err != nil {
			state.setPendingReadError(err)
		}
		return int32(n)
	}
	if errors.Is(err, io.EOF) {
		return avutil.AVERROR_EOF
	}
	if err == nil {
		err = io.ErrNoProgress
	}
	state.recordCallbackError("read", err)
	return result
}

func customIOWriteCallback(_ purego.CDecl, opaque uintptr, buf *byte, bufSize int32) (result int32) {
	result = avutil.AVERROR_EXTERNAL
	state := lookupCustomIOState(opaque)
	if state == nil {
		return result
	}
	defer func() {
		if value := recover(); value != nil {
			state.recordCallbackError("write", callbackPanicError(value))
			result = avutil.AVERROR_EXTERNAL
		}
	}()
	if !state.beginCallback() {
		return result
	}
	defer state.endCallback()

	if buf == nil || bufSize <= 0 {
		state.recordCallbackError("write", errors.New("invalid source buffer"))
		return result
	}
	if state.callbacks == nil || (state.callbacks.WriteContext == nil && state.callbacks.Write == nil) {
		state.recordCallbackError("write", errors.New("callback is not configured"))
		return result
	}

	goBuffer := unsafe.Slice(buf, int(bufSize))
	var n int
	var err error
	if state.callbacks.WriteContext != nil {
		n, err = state.callbacks.WriteContext(state.callbackContext(), goBuffer)
	} else {
		n, err = state.callbacks.Write(goBuffer)
	}
	if n < 0 || n > int(bufSize) {
		state.recordCallbackError("write", errors.Join(errors.New("invalid byte count"), err))
		return result
	}
	if err != nil {
		state.recordCallbackError("write", err)
		if n < int(bufSize) {
			return result
		}
	}
	if n != int(bufSize) {
		state.recordCallbackError("write", io.ErrShortWrite)
		return result
	}
	return int32(n)
}

func customIOSeekCallback(_ purego.CDecl, opaque uintptr, offset int64, whence int32) (result int64) {
	result = int64(avutil.AVERROR_EXTERNAL)
	state := lookupCustomIOState(opaque)
	if state == nil {
		return result
	}
	defer func() {
		if value := recover(); value != nil {
			state.recordCallbackError("seek", callbackPanicError(value))
			result = int64(avutil.AVERROR_EXTERNAL)
		}
	}()
	if !state.beginCallback() {
		return result
	}
	defer state.endCallback()

	if state.callbacks == nil || (state.callbacks.SeekContext == nil && state.callbacks.Seek == nil) {
		return -1
	}
	seek := func(offset int64, whence int) (int64, error) {
		if state.callbacks.SeekContext != nil {
			return state.callbacks.SeekContext(state.callbackContext(), offset, whence)
		}
		return state.callbacks.Seek(offset, whence)
	}
	if whence&avSeekSize != 0 {
		current, err := seek(0, io.SeekCurrent)
		if err != nil {
			state.recordCallbackError("seek", err)
			return result
		}
		end, err := seek(0, io.SeekEnd)
		if err != nil {
			state.recordCallbackError("seek", err)
			return result
		}
		if _, err := seek(current, io.SeekStart); err != nil {
			state.recordCallbackError("seek", err)
			return result
		}
		return end
	}

	newPosition, err := seek(offset, int(whence&^avSeekForce))
	if err != nil {
		state.recordCallbackError("seek", err)
		return result
	}
	return newPosition
}

func callbackPanicError(value any) error {
	if err, ok := value.(error); ok {
		return fmt.Errorf("panic: %w", err)
	}
	return fmt.Errorf("panic: %v", value)
}

func (c *customIOState) recordCallbackError(operation string, err error) {
	if err == nil {
		return
	}
	c.errorMu.Lock()
	defer c.errorMu.Unlock()
	if c.callbackErr == nil {
		c.callbackErr = fmt.Errorf("ffmpeg: custom I/O %s callback: %w", operation, err)
	}
}

func (c *customIOState) setPendingReadError(err error) {
	c.errorMu.Lock()
	c.pendingReadErr = err
	c.errorMu.Unlock()
}

func (c *customIOState) takePendingReadError() error {
	c.errorMu.Lock()
	defer c.errorMu.Unlock()
	err := c.pendingReadErr
	c.pendingReadErr = nil
	return err
}

func (c *customIOState) beginOperation() {
	c.beginOperationContext(context.Background())
}

func (c *customIOState) beginOperationContext(ctx context.Context) {
	c.endOperation()
	c.errorMu.Lock()
	c.callbackErr = nil
	c.errorMu.Unlock()

	operationCtx, operationEnd := context.WithCancel(ctx)
	c.contextMu.Lock()
	if c.lifetimeCtx == nil {
		c.lifetimeCtx, c.lifetimeCancel = context.WithCancel(context.Background())
	}
	stopLifetime := context.AfterFunc(c.lifetimeCtx, operationEnd)
	c.operationCtx = operationCtx
	c.operationEnd = operationEnd
	c.stopLifetime = stopLifetime
	c.contextMu.Unlock()
}

func (c *customIOState) finishOperation(nativeErr error) error {
	c.errorMu.Lock()
	callbackErr := c.callbackErr
	c.callbackErr = nil
	c.errorMu.Unlock()
	c.endOperation()
	return errors.Join(nativeErr, callbackErr)
}

func (c *customIOState) callbackContext() context.Context {
	c.contextMu.RLock()
	ctx := c.operationCtx
	if ctx == nil {
		ctx = c.lifetimeCtx
	}
	c.contextMu.RUnlock()
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func (c *customIOState) endOperation() {
	c.contextMu.Lock()
	operationEnd := c.operationEnd
	stopLifetime := c.stopLifetime
	c.operationCtx = nil
	c.operationEnd = nil
	c.stopLifetime = nil
	c.contextMu.Unlock()
	if stopLifetime != nil {
		stopLifetime()
	}
	if operationEnd != nil {
		operationEnd()
	}
}

func (c *customIOState) cancelPending() {
	if c == nil {
		return
	}
	c.contextMu.RLock()
	cancel := c.lifetimeCancel
	c.contextMu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

func (c *customIOState) resetCancellation() {
	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	c.contextMu.Lock()
	previousCancel := c.lifetimeCancel
	c.lifetimeCtx = lifetimeCtx
	c.lifetimeCancel = lifetimeCancel
	c.contextMu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
}

func (c *customIOState) beginCallback() bool {
	c.callbackMu.Lock()
	defer c.callbackMu.Unlock()
	if c.callbacksDone {
		return false
	}
	c.activeCallback++
	return true
}

func (c *customIOState) endCallback() {
	c.callbackMu.Lock()
	c.activeCallback--
	if c.activeCallback == 0 && c.callbackCond != nil {
		c.callbackCond.Broadcast()
	}
	c.callbackMu.Unlock()
}

func (c *customIOState) waitForCallbacks() {
	c.callbackMu.Lock()
	c.callbacksDone = true
	if c.callbackCond == nil {
		c.callbackCond = sync.NewCond(&c.callbackMu)
	}
	for c.activeCallback > 0 {
		c.callbackCond.Wait()
	}
	c.callbackMu.Unlock()
}

// abandon prevents future callbacks and cancels cooperative callbacks without
// waiting on user code from the runtime finalizer goroutine.
func (c *customIOState) abandon() {
	c.callbackMu.Lock()
	c.callbacksDone = true
	c.callbackMu.Unlock()
	c.cancelPending()
	c.endOperation()
}

func (d *Decoder) readInputPacketLocked(packet avcodec.Packet) error {
	if d.customIO == nil {
		return avformat.ReadFrame(d.formatCtx, packet)
	}
	d.customIO.beginOperationContext(d.interruptContext())
	return d.customIO.finishOperation(avformat.ReadFrame(d.formatCtx, packet))
}

func (d *Decoder) seekInputLocked(streamIndex int32, timestamp int64, flags int32) error {
	if d.customIO == nil {
		return avformat.SeekFrame(d.formatCtx, streamIndex, timestamp, flags)
	}
	d.customIO.beginOperationContext(d.interruptContext())
	return d.customIO.finishOperation(avformat.SeekFrame(d.formatCtx, streamIndex, timestamp, flags))
}

func (e *Encoder) writeOutputHeaderLocked(options *avutil.Dictionary) error {
	if e.customIO == nil {
		return avformat.WriteHeader(e.formatCtx, options)
	}
	e.customIO.beginOperation()
	return e.customIO.finishOperation(avformat.WriteHeader(e.formatCtx, options))
}

func (e *Encoder) writeOutputPacketLocked(packet avcodec.Packet) error {
	if e.customIO == nil {
		return avformat.InterleavedWriteFrame(e.formatCtx, packet)
	}
	e.customIO.beginOperation()
	return e.customIO.finishOperation(avformat.InterleavedWriteFrame(e.formatCtx, packet))
}

func (e *Encoder) writeOutputTrailerLocked() error {
	if e.customIO == nil {
		return avformat.WriteTrailer(e.formatCtx)
	}
	e.customIO.beginOperation()
	return e.customIO.finishOperation(avformat.WriteTrailer(e.formatCtx))
}

// NewCustomIOContext creates a new custom I/O context with the given callbacks.
func NewCustomIOContext(callbacks *IOCallbacks, writable bool) (*CustomIOContext, error) {
	return NewCustomIOContextWithSize(callbacks, writable, defaultIOBufferSize)
}

// NewCustomIOContextWithSize creates a new custom I/O context with a specific buffer size.
func NewCustomIOContextWithSize(callbacks *IOCallbacks, writable bool, bufferSize int) (*CustomIOContext, error) {
	if callbacks == nil {
		return nil, errors.New("ffmpeg: callbacks cannot be nil")
	}
	if !writable && callbacks.ReadContext == nil && callbacks.Read == nil {
		return nil, errors.New("ffmpeg: read callback required for readable I/O context")
	}
	if writable && callbacks.WriteContext == nil && callbacks.Write == nil {
		return nil, errors.New("ffmpeg: write callback required for writable I/O context")
	}
	if bufferSize <= 0 {
		return nil, errors.New("ffmpeg: I/O buffer size must be positive")
	}

	// Ensure FFmpeg is loaded
	if err := bindings.Load(); err != nil {
		return nil, err
	}

	// Initialize global callbacks
	if err := initIOCallbacks(); err != nil {
		return nil, err
	}

	// Allocate the buffer with av_malloc as required by avio_alloc_context.
	buffer := avutil.Malloc(uintptr(bufferSize))
	if buffer == nil {
		return nil, errors.New("ffmpeg: failed to allocate I/O buffer")
	}

	lifetimeCtx, lifetimeCancel := context.WithCancel(context.Background())
	state := &customIOState{
		callbacks:      callbacks,
		lifetimeCtx:    lifetimeCtx,
		lifetimeCancel: lifetimeCancel,
	}
	ctx := &CustomIOContext{
		customIOState: state,
	}

	// Register handle for callback lookup
	ctx.lease = handles.RegisterLease(state, state.abandon)
	ctx.handle = ctx.lease.ID()

	// Determine which callbacks to use
	var readCb, writeCb, seekCb uintptr
	if callbacks.ReadContext != nil || callbacks.Read != nil {
		readCb = readCallbackPtr
	}
	if callbacks.WriteContext != nil || callbacks.Write != nil {
		writeCb = writeCallbackPtr
	}
	if callbacks.SeekContext != nil || callbacks.Seek != nil {
		seekCb = seekCallbackPtr
	}

	// Create AVIOContext
	ctx.avioCtx = avformat.IOAllocContext(
		buffer,
		bufferSize,
		writable,
		ctx.handle,
		readCb,
		writeCb,
		seekCb,
	)

	if ctx.avioCtx == nil {
		avutil.Free(buffer)
		ctx.lease.Release()
		ctx.lease = nil
		ctx.handle = 0
		state.cancelPending()
		return nil, errors.New("ffmpeg: failed to create AVIOContext")
	}

	return ctx, nil
}

// Close releases the I/O context. It cancels context-aware callbacks and waits
// for active callback trampolines before releasing native memory.
func (c *CustomIOContext) Close() error {
	c.cancelPending()
	c.waitForCallbacks()
	c.endOperation()
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	// Free the AVIOContext and whichever buffer FFmpeg currently stores in it.
	if c.avioCtx != nil {
		avformat.IOContextFree(&c.avioCtx)
	}

	c.callbacks = nil
	c.errorMu.Lock()
	c.callbackErr = nil
	c.pendingReadErr = nil
	c.errorMu.Unlock()

	// Release the callback registration after native callbacks have stopped.
	if c.lease != nil {
		c.lease.Release()
		c.lease = nil
		c.handle = 0
	}

	return nil
}

// AVIOContext returns the underlying AVIOContext pointer.
func (c *CustomIOContext) AVIOContext() avformat.IOContext {
	return c.avioCtx
}

// NewDecoderFromIO creates a decoder with custom I/O.
//
// This constructor opens the input and reads stream information before it
// returns, so Read can be called immediately and repeatedly. A live source must
// feed a continuous demuxable byte stream concurrently with construction. Raw
// RTP payloads are not a demuxable byte stream by themselves; depacketize them
// first (for example into Annex B H.264/H.265 access units) and pass the
// corresponding format hint such as "h264" or "hevc".
func NewDecoderFromIO(callbacks *IOCallbacks, opts *DecoderOptions) (*Decoder, error) {
	return NewDecoderFromIOContext(context.Background(), callbacks, opts)
}

// NewDecoderFromIOContext creates a decoder with custom I/O and cancellation.
// The returned decoder owns the CustomIOContext and closes it with Decoder.Close.
// Construction performs FFmpeg probing synchronously and therefore needs enough
// valid input bytes to identify the stream before it can return.
func NewDecoderFromIOContext(ctx context.Context, callbacks *IOCallbacks, opts *DecoderOptions) (*Decoder, error) {
	if ctx == nil {
		return nil, errors.New("ffmpeg: context cannot be nil")
	}
	opts = cloneDecoderOptions(opts)
	if err := validateHWDecoderConfig(opts.Hardware); err != nil {
		return nil, err
	}
	// Create custom I/O context
	ioCtx, err := NewCustomIOContext(callbacks, false)
	if err != nil {
		return nil, err
	}
	interrupt := newDecoderInterrupt()
	if err := interrupt.begin(ctx); err != nil {
		interrupt.release(nil)
		_ = ioCtx.Close()
		return nil, err
	}
	defer interrupt.clear()

	// Allocate format context
	formatCtx := avformat.AllocContext()
	if formatCtx == nil {
		interrupt.release(nil)
		_ = ioCtx.Close()
		return nil, errors.New("ffmpeg: failed to allocate format context")
	}
	interrupt.attach(formatCtx)

	// Set custom I/O
	avformat.SetIOContext(formatCtx, ioCtx.AVIOContext())

	// Set CUSTOM_IO flag to tell FFmpeg we own the I/O context
	avformat.AddFlags(formatCtx, avformat.AVFMT_FLAG_CUSTOM_IO)

	// Optional format hint.
	var inputFmt avformat.InputFormat
	if opts != nil && opts.Format != "" {
		inputFmt = avformat.FindInputFormat(opts.Format)
		if inputFmt == nil {
			interrupt.release(formatCtx)
			_ = ioCtx.Close()
			avformat.FreeContext(formatCtx)
			return nil, errors.New("ffmpeg: input format not found")
		}
	}

	// Build AVDictionary from options.
	var avDict avutil.Dictionary
	for key, value := range buildDecoderAVOptions(opts) {
		if value == "" {
			continue
		}
		if err := avutil.DictSet(&avDict, key, value, 0); err != nil {
			if avDict != nil {
				avutil.DictFree(&avDict)
			}
			interrupt.release(formatCtx)
			_ = ioCtx.Close()
			avformat.FreeContext(formatCtx)
			return nil, err
		}
	}

	// Open input with custom I/O (pass empty string since we have custom I/O)
	ioCtx.beginOperationContext(ctx)
	if err := interrupt.finish(ioCtx.finishOperation(avformat.OpenInput(&formatCtx, "", inputFmt, &avDict))); err != nil {
		if avDict != nil {
			avutil.DictFree(&avDict)
		}
		interrupt.release(formatCtx)
		if formatCtx != nil {
			avformat.CloseInput(&formatCtx)
		}
		_ = ioCtx.Close()
		return nil, err
	}

	// Free any remaining dictionary entries (FFmpeg may have consumed some)
	if avDict != nil {
		avutil.DictFree(&avDict)
	}

	// Find stream info
	ioCtx.beginOperationContext(ctx)
	if err := interrupt.finish(ioCtx.finishOperation(avformat.FindStreamInfo(formatCtx, nil))); err != nil {
		interrupt.release(formatCtx)
		avformat.CloseInput(&formatCtx)
		_ = ioCtx.Close()
		return nil, err
	}

	d := newDecoder(interrupt)
	d.formatCtx = formatCtx
	d.hardwareConfig = opts.Hardware
	if opts.Hardware != nil && opts.Hardware.Mode != HardwareAccelerationDisabled {
		d.videoDecoderInfo.HardwareState = HardwareStatePending
	}

	// Ensure the custom I/O stays alive and is cleaned up.
	d.customIO = ioCtx

	// Find best video stream
	d.videoStreamIdx = int(avformat.FindBestStream(d.formatCtx, avutil.MediaTypeVideo, -1, -1, nil, 0))
	if d.videoStreamIdx >= 0 {
		info, err := d.getStreamInfo(d.videoStreamIdx)
		if err != nil {
			d.Close()
			return nil, err
		}
		d.videoInfo = info
	}

	// Find best audio stream
	d.audioStreamIdx = int(avformat.FindBestStream(d.formatCtx, avutil.MediaTypeAudio, -1, -1, nil, 0))
	if d.audioStreamIdx >= 0 {
		info, err := d.getStreamInfo(d.audioStreamIdx)
		if err != nil {
			d.Close()
			return nil, err
		}
		d.audioInfo = info
	}

	if err := d.allocateDecodeResources(); err != nil {
		d.Close()
		return nil, err
	}

	return d, nil
}

// NewDecoderFromReader creates a decoder that reads from an io.Reader.
// If r implements io.Seeker, seeking will be supported.
func NewDecoderFromReader(r io.Reader, opts *DecoderOptions) (*Decoder, error) {
	return NewDecoderFromReaderContext(context.Background(), r, opts)
}

// NewDecoderFromReaderContext creates a decoder from an io.Reader with cancellation.
func NewDecoderFromReaderContext(ctx context.Context, r io.Reader, opts *DecoderOptions) (*Decoder, error) {
	if r == nil {
		return nil, errors.New("ffmpeg: reader cannot be nil")
	}

	callbacks := &IOCallbacks{
		Read: func(buf []byte) (int, error) {
			return r.Read(buf)
		},
	}

	if seeker, ok := r.(io.Seeker); ok {
		callbacks.Seek = func(offset int64, whence int) (int64, error) {
			return seeker.Seek(offset, whence)
		}
	}

	return NewDecoderFromIOContext(ctx, callbacks, opts)
}

// NewEncoderToWriter creates an encoder that writes to an io.Writer.
// If w implements io.Seeker, seeking will be supported.
// format is the output format (e.g., "mp4", "mkv", "avi").
func NewEncoderToWriter(w io.Writer, format string, opts *EncoderOptions) (*Encoder, error) {
	if w == nil {
		return nil, errors.New("ffmpeg: writer cannot be nil")
	}
	callbacks := &IOCallbacks{
		Write: func(buf []byte) (int, error) {
			return w.Write(buf)
		},
	}
	if seeker, ok := w.(io.Seeker); ok {
		callbacks.Seek = func(offset int64, whence int) (int64, error) {
			return seeker.Seek(offset, whence)
		}
	}
	return NewEncoderFromIO(callbacks, format, opts)
}

// NewEncoderFromIO creates an encoder with custom I/O.
// The encoder owns the CustomIOContext and closes it with Encoder.Close.
func NewEncoderFromIO(callbacks *IOCallbacks, format string, opts *EncoderOptions) (*Encoder, error) {
	if opts == nil {
		return nil, errors.New("ffmpeg: EncoderOptions is required")
	}
	ioCtx, err := NewCustomIOContext(callbacks, true)
	if err != nil {
		return nil, err
	}
	options := *opts
	if format != "" {
		options.Format = format
	}
	encoder, err := newEncoder("", &options, ioCtx)
	if err != nil {
		_ = ioCtx.Close()
		return nil, err
	}
	return encoder, nil
}
