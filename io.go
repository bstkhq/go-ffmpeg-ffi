//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
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
type IOCallbacks struct {
	// Read reads up to len(buf) bytes into buf.
	// Returns the number of bytes read and any error encountered.
	// At end of file, returns 0, io.EOF.
	//
	// The callback may be invoked synchronously by decoder constructors while
	// FFmpeg probes the input. For live/channel-backed sources, block until
	// demuxable bytes are available or return io.EOF when the source is closed.
	// Do not return 0, nil to mean "no data yet"; that gives FFmpeg no forward
	// progress during probing.
	Read func(buf []byte) (int, error)

	// Write writes len(buf) bytes from buf.
	// Returns the number of bytes written and any error encountered.
	Write func(buf []byte) (int, error)

	// Seek seeks to the given offset.
	// whence: 0 = SEEK_SET, 1 = SEEK_CUR, 2 = SEEK_END
	// Returns the new offset and any error encountered.
	Seek func(offset int64, whence int) (int64, error)
}

// CustomIOContext wraps an AVIOContext with custom callbacks.
type CustomIOContext struct {
	mu             sync.Mutex
	errorMu        sync.Mutex
	avioCtx        avformat.IOContext
	buffer         unsafe.Pointer // Allocated with av_malloc, owned by FFmpeg
	bufferGo       []byte         // Go slice view of buffer (for callbacks)
	callbacks      *IOCallbacks
	handle         uintptr
	callbackErr    error
	pendingReadErr error
	closed         bool
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

func initIOCallbacks() error {
	ioCallbacksOnce.Do(func() {
		// Read callback: int read_packet(void *opaque, uint8_t *buf, int buf_size)
		readCallbackPtr = purego.NewCallback(customIOReadCallback)

		// Write callback: int write_packet(void *opaque, uint8_t *buf, int buf_size)
		writeCallbackPtr = purego.NewCallback(customIOWriteCallback)

		// Seek callback: int64_t seek(void *opaque, int64_t offset, int whence)
		seekCallbackPtr = purego.NewCallback(customIOSeekCallback)
	})

	return ioCallbacksInitErr
}

func lookupCustomIOContext(opaque uintptr) *CustomIOContext {
	ctx, _ := handles.Lookup(opaque).(*CustomIOContext)
	return ctx
}

func customIOReadCallback(_ purego.CDecl, opaque uintptr, buf *byte, bufSize int32) (result int32) {
	result = avutil.AVERROR_EXTERNAL
	ctx := lookupCustomIOContext(opaque)
	if ctx == nil {
		return result
	}
	defer func() {
		if value := recover(); value != nil {
			ctx.recordCallbackError("read", callbackPanicError(value))
			result = avutil.AVERROR_EXTERNAL
		}
	}()

	if err := ctx.takePendingReadError(); err != nil {
		if errors.Is(err, io.EOF) {
			return avutil.AVERROR_EOF
		}
		ctx.recordCallbackError("read", err)
		return result
	}
	if buf == nil || bufSize <= 0 {
		ctx.recordCallbackError("read", errors.New("invalid destination buffer"))
		return result
	}
	if ctx.callbacks == nil || ctx.callbacks.Read == nil {
		ctx.recordCallbackError("read", errors.New("callback is not configured"))
		return result
	}

	n, err := ctx.callbacks.Read(unsafe.Slice(buf, int(bufSize)))
	if n < 0 || n > int(bufSize) {
		ctx.recordCallbackError("read", errors.Join(errors.New("invalid byte count"), err))
		return result
	}
	if n > 0 {
		if err != nil {
			ctx.setPendingReadError(err)
		}
		return int32(n)
	}
	if errors.Is(err, io.EOF) {
		return avutil.AVERROR_EOF
	}
	if err == nil {
		err = io.ErrNoProgress
	}
	ctx.recordCallbackError("read", err)
	return result
}

func customIOWriteCallback(_ purego.CDecl, opaque uintptr, buf *byte, bufSize int32) (result int32) {
	result = avutil.AVERROR_EXTERNAL
	ctx := lookupCustomIOContext(opaque)
	if ctx == nil {
		return result
	}
	defer func() {
		if value := recover(); value != nil {
			ctx.recordCallbackError("write", callbackPanicError(value))
			result = avutil.AVERROR_EXTERNAL
		}
	}()

	if buf == nil || bufSize <= 0 {
		ctx.recordCallbackError("write", errors.New("invalid source buffer"))
		return result
	}
	if ctx.callbacks == nil || ctx.callbacks.Write == nil {
		ctx.recordCallbackError("write", errors.New("callback is not configured"))
		return result
	}

	n, err := ctx.callbacks.Write(unsafe.Slice(buf, int(bufSize)))
	if n < 0 || n > int(bufSize) {
		ctx.recordCallbackError("write", errors.Join(errors.New("invalid byte count"), err))
		return result
	}
	if err != nil {
		ctx.recordCallbackError("write", err)
		if n < int(bufSize) {
			return result
		}
	}
	if n != int(bufSize) {
		ctx.recordCallbackError("write", io.ErrShortWrite)
		return result
	}
	return int32(n)
}

func customIOSeekCallback(_ purego.CDecl, opaque uintptr, offset int64, whence int32) (result int64) {
	result = int64(avutil.AVERROR_EXTERNAL)
	ctx := lookupCustomIOContext(opaque)
	if ctx == nil {
		return result
	}
	defer func() {
		if value := recover(); value != nil {
			ctx.recordCallbackError("seek", callbackPanicError(value))
			result = int64(avutil.AVERROR_EXTERNAL)
		}
	}()

	if ctx.callbacks == nil || ctx.callbacks.Seek == nil {
		return -1
	}
	if whence&avSeekSize != 0 {
		current, err := ctx.callbacks.Seek(0, io.SeekCurrent)
		if err != nil {
			ctx.recordCallbackError("seek", err)
			return result
		}
		end, err := ctx.callbacks.Seek(0, io.SeekEnd)
		if err != nil {
			ctx.recordCallbackError("seek", err)
			return result
		}
		if _, err := ctx.callbacks.Seek(current, io.SeekStart); err != nil {
			ctx.recordCallbackError("seek", err)
			return result
		}
		return end
	}

	newPosition, err := ctx.callbacks.Seek(offset, int(whence&^avSeekForce))
	if err != nil {
		ctx.recordCallbackError("seek", err)
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

func (c *CustomIOContext) recordCallbackError(operation string, err error) {
	if err == nil {
		return
	}
	c.errorMu.Lock()
	defer c.errorMu.Unlock()
	if c.callbackErr == nil {
		c.callbackErr = fmt.Errorf("ffgo: custom I/O %s callback: %w", operation, err)
	}
}

func (c *CustomIOContext) setPendingReadError(err error) {
	c.errorMu.Lock()
	c.pendingReadErr = err
	c.errorMu.Unlock()
}

func (c *CustomIOContext) takePendingReadError() error {
	c.errorMu.Lock()
	defer c.errorMu.Unlock()
	err := c.pendingReadErr
	c.pendingReadErr = nil
	return err
}

func (c *CustomIOContext) beginOperation() {
	c.errorMu.Lock()
	c.callbackErr = nil
	c.errorMu.Unlock()
}

func (c *CustomIOContext) finishOperation(nativeErr error) error {
	c.errorMu.Lock()
	callbackErr := c.callbackErr
	c.callbackErr = nil
	c.errorMu.Unlock()
	return errors.Join(nativeErr, callbackErr)
}

func (d *Decoder) readInputPacketLocked(packet avcodec.Packet) error {
	if d.customIO == nil {
		return avformat.ReadFrame(d.formatCtx, packet)
	}
	d.customIO.beginOperation()
	return d.customIO.finishOperation(avformat.ReadFrame(d.formatCtx, packet))
}

func (d *Decoder) seekInputLocked(streamIndex int32, timestamp int64, flags int32) error {
	if d.customIO == nil {
		return avformat.SeekFrame(d.formatCtx, streamIndex, timestamp, flags)
	}
	d.customIO.beginOperation()
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
		return nil, errors.New("ffgo: callbacks cannot be nil")
	}
	if !writable && callbacks.Read == nil {
		return nil, errors.New("ffgo: read callback required for readable I/O context")
	}
	if writable && callbacks.Write == nil {
		return nil, errors.New("ffgo: write callback required for writable I/O context")
	}
	if bufferSize <= 0 {
		return nil, errors.New("ffgo: I/O buffer size must be positive")
	}

	// Ensure FFmpeg is loaded
	if err := bindings.Load(); err != nil {
		return nil, err
	}

	// Initialize global callbacks
	if err := initIOCallbacks(); err != nil {
		return nil, err
	}

	// Allocate buffer with av_malloc (required by FFmpeg - it will free it)
	buffer := avutil.Malloc(uintptr(bufferSize))
	if buffer == nil {
		return nil, errors.New("ffgo: failed to allocate I/O buffer")
	}

	ctx := &CustomIOContext{
		buffer:    buffer,
		bufferGo:  unsafe.Slice((*byte)(buffer), bufferSize),
		callbacks: callbacks,
	}

	// Register handle for callback lookup
	ctx.handle = handles.Register(ctx)

	// Determine which callbacks to use
	var readCb, writeCb, seekCb uintptr
	if callbacks.Read != nil {
		readCb = readCallbackPtr
	}
	if callbacks.Write != nil {
		writeCb = writeCallbackPtr
	}
	if callbacks.Seek != nil {
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
		handles.Unregister(ctx.handle)
		return nil, errors.New("ffgo: failed to create AVIOContext")
	}

	return ctx, nil
}

// Close releases the I/O context.
// Note: avio_context_free also frees the buffer, so we don't free it manually.
func (c *CustomIOContext) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	// Free AVIOContext (this also frees the buffer)
	if c.avioCtx != nil {
		avformat.IOContextFree(&c.avioCtx)
	}

	// Clear buffer references (buffer is freed by IOContextFree)
	c.buffer = nil
	c.bufferGo = nil

	// Unregister handle
	if c.handle != 0 {
		handles.Unregister(c.handle)
		c.handle = 0
	}

	return nil
}

// AVIOContext returns the underlying AVIOContext pointer.
func (c *CustomIOContext) AVIOContext() avformat.IOContext {
	return c.avioCtx
}

// NewDecoderFromIO creates a decoder with custom I/O.
// format is the format hint (e.g., "mp4", "mkv", "avi") - can be empty for auto-detection.
//
// This constructor opens the input and reads stream information before it
// returns, so Read can be called immediately and repeatedly. A live source must
// feed a continuous demuxable byte stream concurrently with construction. Raw
// RTP payloads are not a demuxable byte stream by themselves; depacketize them
// first (for example into Annex B H.264/H.265 access units) and pass the
// corresponding format hint such as "h264" or "hevc".
func NewDecoderFromIO(callbacks *IOCallbacks, format string) (*Decoder, error) {
	return NewDecoderFromIOWithOptions(callbacks, &DecoderOptions{Format: format})
}

// NewDecoderFromIOWithOptions creates a decoder with custom I/O and DecoderOptions.
//
// It supports passing typed probing controls (probesize/analyzeduration/etc) and a format hint.
// The returned decoder owns the CustomIOContext and will close it on Decoder.Close().
// Like NewDecoderFromIO, this call performs FFmpeg probing synchronously and
// therefore needs enough valid input bytes to identify the stream before it can
// return a Decoder.
func NewDecoderFromIOWithOptions(callbacks *IOCallbacks, opts *DecoderOptions) (*Decoder, error) {
	// Create custom I/O context
	ioCtx, err := NewCustomIOContext(callbacks, false)
	if err != nil {
		return nil, err
	}

	// Allocate format context
	formatCtx := avformat.AllocContext()
	if formatCtx == nil {
		ioCtx.Close()
		return nil, errors.New("ffgo: failed to allocate format context")
	}

	// Set custom I/O
	avformat.SetIOContext(formatCtx, ioCtx.AVIOContext())

	// Set CUSTOM_IO flag to tell FFmpeg we own the I/O context
	avformat.AddFlags(formatCtx, avformat.AVFMT_FLAG_CUSTOM_IO)

	// Optional format hint.
	var inputFmt avformat.InputFormat
	if opts != nil && opts.Format != "" {
		inputFmt = avformat.FindInputFormat(opts.Format)
		if inputFmt == nil {
			ioCtx.Close()
			avformat.FreeContext(formatCtx)
			return nil, errors.New("ffgo: input format not found")
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
			ioCtx.Close()
			avformat.FreeContext(formatCtx)
			return nil, err
		}
	}

	// Open input with custom I/O (pass empty string since we have custom I/O)
	ioCtx.beginOperation()
	if err := ioCtx.finishOperation(avformat.OpenInput(&formatCtx, "", inputFmt, &avDict)); err != nil {
		if avDict != nil {
			avutil.DictFree(&avDict)
		}
		ioCtx.Close()
		avformat.FreeContext(formatCtx)
		return nil, err
	}

	// Free any remaining dictionary entries (FFmpeg may have consumed some)
	if avDict != nil {
		avutil.DictFree(&avDict)
	}

	// Find stream info
	ioCtx.beginOperation()
	if err := ioCtx.finishOperation(avformat.FindStreamInfo(formatCtx, nil)); err != nil {
		avformat.CloseInput(&formatCtx)
		ioCtx.Close()
		return nil, err
	}

	d := &Decoder{
		formatCtx:      formatCtx,
		videoStreamIdx: -1,
		audioStreamIdx: -1,
	}

	// Ensure the custom I/O stays alive and is cleaned up.
	d.customIO = ioCtx

	// Find best video stream
	d.videoStreamIdx = int(avformat.FindBestStream(d.formatCtx, avutil.MediaTypeVideo, -1, -1, nil, 0))
	if d.videoStreamIdx >= 0 {
		d.videoInfo = d.getStreamInfo(d.videoStreamIdx)
	}

	// Find best audio stream
	d.audioStreamIdx = int(avformat.FindBestStream(d.formatCtx, avutil.MediaTypeAudio, -1, -1, nil, 0))
	if d.audioStreamIdx >= 0 {
		d.audioInfo = d.getStreamInfo(d.audioStreamIdx)
	}

	// Allocate packet and frame
	d.packet = avcodec.PacketAlloc()
	if d.packet == nil {
		d.Close()
		ioCtx.Close()
		return nil, errors.New("ffgo: failed to allocate packet")
	}

	d.frame = avutil.FrameAlloc()
	if d.frame == nil {
		d.Close()
		return nil, errors.New("ffgo: failed to allocate frame")
	}

	return d, nil
}

// NewDecoderFromReader creates a decoder that reads from an io.Reader.
// If r implements io.Seeker, seeking will be supported.
// format is the format hint (e.g., "mp4", "mkv") - can be empty for auto-detection.
func NewDecoderFromReader(r io.Reader, format string) (*Decoder, error) {
	if r == nil {
		return nil, errors.New("ffgo: reader cannot be nil")
	}

	callbacks := &IOCallbacks{
		Read: func(buf []byte) (int, error) {
			return r.Read(buf)
		},
	}

	// Check if reader supports seeking
	if seeker, ok := r.(io.Seeker); ok {
		callbacks.Seek = func(offset int64, whence int) (int64, error) {
			return seeker.Seek(offset, whence)
		}
	}

	return NewDecoderFromIO(callbacks, format)
}

// NewDecoderFromReaderWithOptions creates a decoder that reads from an io.Reader using DecoderOptions.
// If r implements io.Seeker, seeking will be supported.
func NewDecoderFromReaderWithOptions(r io.Reader, opts *DecoderOptions) (*Decoder, error) {
	if r == nil {
		return nil, errors.New("ffgo: reader cannot be nil")
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

	return NewDecoderFromIOWithOptions(callbacks, opts)
}

// NewEncoderToWriter creates an encoder that writes to an io.Writer.
// If w implements io.Seeker, seeking will be supported.
// format is the output format (e.g., "mp4", "mkv", "avi").
func NewEncoderToWriter(w io.Writer, format string, config EncoderConfig) (*Encoder, error) {
	if w == nil {
		return nil, errors.New("ffgo: writer cannot be nil")
	}

	callbacks := &IOCallbacks{
		Write: func(buf []byte) (int, error) {
			return w.Write(buf)
		},
	}

	// Check if writer supports seeking
	if seeker, ok := w.(io.Seeker); ok {
		callbacks.Seek = func(offset int64, whence int) (int64, error) {
			return seeker.Seek(offset, whence)
		}
	}

	return NewEncoderFromIO(callbacks, format, config)
}

// NewEncoderToWriterWithOptions creates an encoder that writes to an io.Writer
// using the EncoderOptions configuration.
// If w implements io.Seeker, seeking will be supported.
// format is the output format (e.g., "mp4", "mkv", "avi").
func NewEncoderToWriterWithOptions(w io.Writer, format string, opts *EncoderOptions) (*Encoder, error) {
	if opts == nil || opts.Video == nil {
		return nil, errors.New("ffgo: EncoderOptions.Video is required")
	}

	video := opts.Video

	// Convert VideoEncoderConfig to EncoderConfig
	cfg := EncoderConfig{
		Width:       video.Width,
		Height:      video.Height,
		PixelFormat: video.PixelFormat,
		CodecID:     video.Codec,
		BitRate:     video.Bitrate,
		GOPSize:     video.GOPSize,
		MaxBFrames:  video.MaxBFrames,
	}

	// Handle frame rate
	if video.FrameRate.Den > 0 {
		cfg.FrameRate = int(video.FrameRate.Num / video.FrameRate.Den)
	}
	if cfg.FrameRate <= 0 {
		cfg.FrameRate = 30
	}

	// Apply defaults
	if cfg.CodecID == CodecIDNone {
		cfg.CodecID = CodecIDH264
	}
	if cfg.BitRate <= 0 {
		cfg.BitRate = 2000000
	}
	if cfg.PixelFormat == PixelFormatNone {
		cfg.PixelFormat = PixelFormatYUV420P
	}

	return NewEncoderToWriter(w, format, cfg)
}

// NewEncoderFromIO creates an encoder with custom I/O.
// format is the output format (e.g., "mp4", "mkv", "avi").
func NewEncoderFromIO(callbacks *IOCallbacks, format string, config EncoderConfig) (*Encoder, error) {
	// Ensure FFmpeg is loaded
	if err := bindings.Load(); err != nil {
		return nil, err
	}

	// Create custom I/O context (writable)
	ioCtx, err := NewCustomIOContext(callbacks, true)
	if err != nil {
		return nil, err
	}

	// Allocate output context with format
	var formatCtx avformat.FormatContext
	if err := avformat.AllocOutputContext2(&formatCtx, nil, format, ""); err != nil {
		ioCtx.Close()
		return nil, err
	}

	if formatCtx == nil {
		ioCtx.Close()
		return nil, errors.New("ffgo: failed to allocate output context")
	}

	// Set custom I/O
	avformat.SetIOContext(formatCtx, ioCtx.AVIOContext())
	avformat.AddFlags(formatCtx, avformat.AVFMT_FLAG_CUSTOM_IO)

	// Create a new stream in the output container
	stream := avformat.NewStream(formatCtx, nil)
	if stream == nil {
		avformat.FreeContext(formatCtx)
		ioCtx.Close()
		return nil, errors.New("ffgo: failed to create output stream")
	}

	// Set stream time base
	if config.FrameRate > 0 {
		avformat.SetStreamTimeBase(stream, 1, int32(config.FrameRate))
	} else {
		avformat.SetStreamTimeBase(stream, 1, 30) // Default to 30 fps
	}

	// Find encoder
	codec := avcodec.FindEncoder(config.CodecID)
	if codec == nil {
		avformat.FreeContext(formatCtx)
		ioCtx.Close()
		return nil, errors.New("ffgo: encoder not found")
	}

	// Allocate codec context
	codecCtx := avcodec.AllocContext3(codec)
	if codecCtx == nil {
		avformat.FreeContext(formatCtx)
		ioCtx.Close()
		return nil, errors.New("ffgo: failed to allocate codec context")
	}

	// Configure codec context
	avcodec.SetCtxWidth(codecCtx, int32(config.Width))
	avcodec.SetCtxHeight(codecCtx, int32(config.Height))
	avcodec.SetCtxPixFmt(codecCtx, int32(config.PixelFormat))
	avcodec.SetCtxBitRate(codecCtx, config.BitRate)

	if config.GOPSize > 0 {
		avcodec.SetCtxGopSize(codecCtx, int32(config.GOPSize))
	} else {
		avcodec.SetCtxGopSize(codecCtx, 12)
	}

	avcodec.SetCtxMaxBFrames(codecCtx, int32(config.MaxBFrames))

	if config.FrameRate > 0 {
		avcodec.SetCtxFramerate(codecCtx, int32(config.FrameRate), 1)
		avcodec.SetCtxTimeBase(codecCtx, 1, int32(config.FrameRate))
	} else {
		avcodec.SetCtxFramerate(codecCtx, 30, 1)
		avcodec.SetCtxTimeBase(codecCtx, 1, 30)
	}

	// Check if global header is needed
	if avformat.NeedsGlobalHeader(formatCtx) {
		flags := avcodec.GetCtxFlags(codecCtx)
		avcodec.SetCtxFlags(codecCtx, flags|avcodec.CodecFlagGlobalHeader)
	}

	// Open codec
	if err := avcodec.Open2(codecCtx, codec, nil); err != nil {
		avcodec.FreeContext(&codecCtx)
		avformat.FreeContext(formatCtx)
		ioCtx.Close()
		return nil, err
	}

	// Copy codec parameters to stream
	codecPar := avformat.GetStreamCodecPar(stream)
	if err := avcodec.ParametersFromContext(codecPar, codecCtx); err != nil {
		avcodec.FreeContext(&codecCtx)
		avformat.FreeContext(formatCtx)
		ioCtx.Close()
		return nil, err
	}

	// Write header
	ioCtx.beginOperation()
	if err := ioCtx.finishOperation(avformat.WriteHeader(formatCtx, nil)); err != nil {
		avcodec.FreeContext(&codecCtx)
		avformat.FreeContext(formatCtx)
		ioCtx.Close()
		return nil, err
	}

	// Allocate packet
	packet := avcodec.PacketAlloc()
	if packet == nil {
		avcodec.FreeContext(&codecCtx)
		avformat.FreeContext(formatCtx)
		ioCtx.Close()
		return nil, errors.New("ffgo: failed to allocate packet")
	}

	// Determine frame rate for time base
	frameRate := config.FrameRate
	if frameRate <= 0 {
		frameRate = 30
	}

	return &Encoder{
		formatCtx:     formatCtx,
		ioCtx:         ioCtx.AVIOContext(),
		customIO:      ioCtx,
		videoCodecCtx: codecCtx,
		videoStream:   stream,
		videoPacket:   packet,
		codecCtx:      codecCtx,
		stream:        stream,
		packet:        packet,
		width:         config.Width,
		height:        config.Height,
		pixFmt:        config.PixelFormat,
		frameCount:    0,
		timeBaseNum:   1,
		timeBaseDen:   int32(frameRate),
		headerWritten: true, // Header was already written above
		hasVideo:      true,
	}, nil
}
