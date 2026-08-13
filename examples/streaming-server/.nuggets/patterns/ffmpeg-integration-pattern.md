---
title: FFmpeg Integration Pattern in Streaming Server
keywords: [ffmpeg, integration, media-processing, initialization, lifecycle]
tags: [core-concept, pattern, advanced]
related: [../media/decoder-lifecycle.md, ../media/encoder-lifecycle.md, ../performance/memory-efficiency.md]
source: [server/media/processor.go, server/media/pool.go]
---

# FFmpeg Integration Pattern in Streaming Server

The streaming server uses ffmpeg as its fundamental media processing layer. Understanding how ffmpeg integrates with the streaming architecture is critical for contributors working on transcoding, ingest, and delivery pipelines.

## Initialization Strategy

Unlike typical CLI tools that initialize FFmpeg once at startup, the streaming server uses lazy initialization within media processing goroutines:

```go
// In each transcoder goroutine
type StreamTranscoder struct {
    mu sync.RWMutex

    decoder *ffmpeg.Decoder
    encoder *ffmpeg.Encoder
    scaler  *ffmpeg.Scaler

    sourceURL string
    destFile  string
}

func (st *StreamTranscoder) Start(ctx context.Context) error {
    // FFmpeg is auto-initialized on first ffmpeg.Decoder call
    // This happens in the transcoding goroutine, so errors are goroutine-local
    decoder, err := ffmpeg.NewDecoder(st.sourceURL,
        ffmpeg.WithHWDevice("cuda"),
        ffmpeg.WithStreams(ffmpeg.MediaTypeVideo, ffmpeg.MediaTypeAudio),
    )
    if err != nil {
        return fmt.Errorf("decoder init failed: %w", err)
    }
    st.decoder = decoder
    defer st.decoder.Close()

    // Continue processing...
}
```

**Why lazy init?** Avoids global state coupling and allows different transcoding nodes to use different FFmpeg configurations (e.g., different hardware acceleration).

## Resource Pooling

ffmpeg objects (Decoder, Encoder, Frame) are relatively expensive to allocate. The server maintains object pools:

```go
// In server/media/pool.go
type DecoderPool struct {
    available chan *ffmpeg.Decoder
    factory   func() (*ffmpeg.Decoder, error)
    size      int
    mu        sync.Mutex
}

func (p *DecoderPool) Acquire(ctx context.Context) (*ffmpeg.Decoder, error) {
    select {
    case decoder := <-p.available:
        return decoder, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        return p.factory()
    }
}

func (p *DecoderPool) Release(decoder *ffmpeg.Decoder) error {
    // Reset decoder state if needed
    select {
    case p.available <- decoder:
        return nil
    default:
        // Pool is full, close the decoder
        return decoder.Close()
    }
}
```

**Note:** Decoders cannot be reused across different sources - they're source-specific. Encoders are similarly stream-specific. The pool pattern here is more about rate-limiting and batch initialization.

## Error Handling Pattern

ffmpeg's error system uses EAGAIN (try again) and EOF semantics. The streaming server wraps these:

```go
func (st *StreamTranscoder) ProcessFrame(frame ffmpeg.Frame) error {
    // Encoding is asynchronous - ffmpeg.IsAgain means buffer is full, try again
    if err := st.encoder.EncodeFrame(frame); err != nil {
        if ffmpeg.IsAgain(err) {
            // Encoder needs output flushed, normal operation
            return st.flushEncodedPackets()
        }
        return fmt.Errorf("encode failed: %w", err)
    }
    return st.flushEncodedPackets()
}
```

## Integration with Custom I/O

The server uses ffmpeg's custom I/O for RTMP ingest and protocol abstraction:

```go
// Custom reader wrapping RTMP connection
type RTMPStreamReader struct {
    conn *rtmp.Connection
    buf  []byte
}

func (r *RTMPStreamReader) Read(p []byte) (int, error) {
    // Read RTMP frames and convert to raw media bytes
    return r.conn.ReadRaw(p)
}

// Use in decoder
decoder, err := ffmpeg.NewDecoderFromReader(
    r,
    ffmpeg.WithFormat("flv"),  // RTMP uses FLV container
)
```

## Hardware Acceleration Integration

The server's hardware acceleration strategy maps to ffmpeg's HWDevice pattern:

```go
// Hardware device assignment based on availability
func selectHWDevice() string {
    if canUseCUDA() {
        return "cuda"
    }
    if canUseVAAPI() {
        return "vaapi"
    }
    return ""  // Software decode
}

// Per-transcode job
transcoder.decoder = ffmpeg.NewDecoder(source,
    ffmpeg.WithHWDevice(selectHWDevice()),
)
```

## Metadata Preservation

When doing stream copy or remuxing, metadata must be preserved:

```go
// Remuxing pattern (fast path)
remuxer, err := ffmpeg.NewRemuxer(sourceFile, outputFile)
if err != nil {
    return err
}
defer remuxer.Close()

// Metadata is automatically copied in remuxer mode
if err := remuxer.Remux(); err != nil {
    return err
}
```

See [stream copy optimization](../media/stream-copy-optimization.md) for zero-copy transcoding patterns.
