//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"fmt"
	"sync"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
)

// Muxer combines multiple streams into a container.
// It provides low-level control over muxing, allowing stream copy mode
// or encoding with multiple audio/subtitle tracks.
type Muxer struct {
	mu             sync.Mutex
	formatCtx      avformat.FormatContext
	ioCtx          avformat.IOContext
	streams        []*MuxerStream
	headerWritten  bool
	trailerWritten bool
	path           string
	headerOptions  map[string]string
	closed         bool
}

// MuxerStream represents a stream being muxed.
type MuxerStream struct {
	muxer     *Muxer
	stream    avformat.Stream
	codecCtx  avcodec.Context
	index     int
	timeBase  Rational
	mediaType MediaType
	encoder   *streamEncoder // nil for copy mode
	copyMode  bool
}

// streamEncoder handles encoding for a muxer stream.
type streamEncoder struct {
	codecCtx avcodec.Context
	packet   avcodec.Packet
	frame    Frame // reusable frame for format conversion if needed
	state    encoderCodecState
}

// NewMuxer creates a muxer for the given output path and format.
// The format parameter is the FFmpeg mux format name (e.g., "matroska", "mp4", "avi").
// If format is empty, it will be guessed from the file extension.
func NewMuxer(path string, format string) (*Muxer, error) {
	if err := bindings.Load(); err != nil {
		return nil, err
	}

	if path == "" {
		return nil, errors.New("ffgo: output path cannot be empty")
	}

	// If format not specified, guess from extension
	if format == "" {
		format = guessFormatFromPath(path)
	}
	if format == "" {
		return nil, errors.New("ffgo: cannot determine output format")
	}

	m := &Muxer{
		path:    path,
		streams: make([]*MuxerStream, 0),
	}

	// Create output format context
	if err := avformat.AllocOutputContext2(&m.formatCtx, nil, format, path); err != nil {
		return nil, err
	}

	return m, nil
}

// VideoStreamConfig configures a video stream for the muxer.
type VideoStreamConfig struct {
	Codec       CodecID     // Video codec (e.g., CodecIDH264)
	Width       int         // Video width
	Height      int         // Video height
	PixelFormat PixelFormat // Pixel format (default: YUV420P)
	FrameRate   int         // Frame rate in fps
	BitRate     int64       // Bitrate in bits/second
	GOPSize     int         // GOP size (keyframe interval)
	MaxBFrames  int         // Maximum number of B-frames
}

// AddVideoStream adds a video stream to the muxer with encoding.
func (m *Muxer) AddVideoStream(config *VideoStreamConfig) (*MuxerStream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, closedError("muxer")
	}
	if m.headerWritten {
		return nil, errors.New("ffgo: cannot add streams after header is written")
	}
	if config == nil {
		return nil, errors.New("ffgo: video config is required")
	}
	configCopy := *config
	config = &configCopy

	// Apply defaults
	if config.Codec == CodecIDNone {
		config.Codec = CodecIDH264
	}
	if config.PixelFormat == PixelFormatNone {
		config.PixelFormat = PixelFormatYUV420P
	}
	if config.FrameRate <= 0 {
		config.FrameRate = 30
	}
	if config.BitRate <= 0 {
		config.BitRate = 2000000
	}
	if config.GOPSize <= 0 {
		config.GOPSize = 12
	}

	// Find encoder
	codec := avcodec.FindEncoder(config.Codec)
	if codec == nil {
		return nil, errors.New("ffgo: video encoder not found")
	}

	// Create codec context
	codecCtx := avcodec.AllocContext3(codec)
	if codecCtx == nil {
		return nil, errors.New("ffgo: failed to allocate video codec context")
	}

	// Configure codec
	avcodec.SetCtxWidth(codecCtx, int32(config.Width))
	avcodec.SetCtxHeight(codecCtx, int32(config.Height))
	avcodec.SetCtxPixFmt(codecCtx, int32(config.PixelFormat))
	avcodec.SetCtxTimeBase(codecCtx, 1, int32(config.FrameRate))
	avcodec.SetCtxFramerate(codecCtx, int32(config.FrameRate), 1)
	avcodec.SetCtxBitRate(codecCtx, config.BitRate)
	avcodec.SetCtxGopSize(codecCtx, int32(config.GOPSize))
	avcodec.SetCtxMaxBFrames(codecCtx, int32(config.MaxBFrames))

	if avformat.NeedsGlobalHeader(m.formatCtx) {
		flags := avcodec.GetCtxFlags(codecCtx)
		avcodec.SetCtxFlags(codecCtx, flags|avcodec.CodecFlagGlobalHeader)
	}

	// Open codec
	if err := avcodec.Open2(codecCtx, codec, nil); err != nil {
		avcodec.FreeContext(&codecCtx)
		return nil, err
	}

	packet := avcodec.PacketAlloc()
	if packet == nil {
		avcodec.FreeContext(&codecCtx)
		return nil, errors.New("ffgo: failed to allocate video packet")
	}

	// Register the stream only after encoder setup succeeds. AVStream entries
	// cannot be removed from an AVFormatContext, so registering earlier would
	// leave an unusable stream behind when avcodec_open2 fails.
	stream := avformat.NewStream(m.formatCtx, codec)
	if stream == nil {
		avcodec.PacketFree(&packet)
		avcodec.FreeContext(&codecCtx)
		return nil, errors.New("ffgo: failed to create video stream")
	}

	// Copy parameters to stream
	codecPar := avformat.GetStreamCodecPar(stream)
	if err := avcodec.ParametersFromContext(codecPar, codecCtx); err != nil {
		avcodec.PacketFree(&packet)
		avcodec.FreeContext(&codecCtx)
		return nil, err
	}

	ms := &MuxerStream{
		muxer:     m,
		stream:    stream,
		codecCtx:  codecCtx,
		index:     int(avformat.GetStreamIndex(stream)),
		timeBase:  NewRational(1, int32(config.FrameRate)),
		mediaType: MediaTypeVideo,
		encoder: &streamEncoder{
			codecCtx: codecCtx,
			packet:   packet,
		},
	}

	m.streams = append(m.streams, ms)
	return ms, nil
}

// AudioStreamConfig configures an audio stream for the muxer.
type AudioStreamConfig struct {
	Codec        CodecID      // Audio codec (e.g., CodecIDAAC)
	SampleRate   int          // Sample rate in Hz
	Channels     int          // Number of channels
	SampleFormat SampleFormat // Sample format
	BitRate      int64        // Bitrate in bits/second
}

// AddAudioStream adds an audio stream to the muxer with encoding.
func (m *Muxer) AddAudioStream(config *AudioStreamConfig) (*MuxerStream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, closedError("muxer")
	}
	if m.headerWritten {
		return nil, errors.New("ffgo: cannot add streams after header is written")
	}
	if config == nil {
		return nil, errors.New("ffgo: audio config is required")
	}
	configCopy := *config
	config = &configCopy

	// Apply defaults
	if config.Codec == CodecIDNone {
		config.Codec = CodecIDAAC
	}
	if config.SampleRate <= 0 {
		config.SampleRate = 48000
	}
	if config.Channels <= 0 {
		config.Channels = 2
	}
	if config.SampleFormat == SampleFormatNone {
		config.SampleFormat = SampleFormatFltP
	}
	if config.BitRate <= 0 {
		config.BitRate = 128000
	}

	// Find encoder
	codec := avcodec.FindEncoder(config.Codec)
	if codec == nil {
		return nil, errors.New("ffgo: audio encoder not found")
	}

	// Create codec context
	codecCtx := avcodec.AllocContext3(codec)
	if codecCtx == nil {
		return nil, errors.New("ffgo: failed to allocate audio codec context")
	}

	// Configure codec
	avcodec.SetCtxSampleRate(codecCtx, int32(config.SampleRate))
	avcodec.SetCtxSampleFmt(codecCtx, int32(config.SampleFormat))
	avcodec.SetCtxBitRate(codecCtx, config.BitRate)
	avcodec.SetCtxTimeBase(codecCtx, 1, int32(config.SampleRate))

	// Set channel layout based on channel count
	avcodec.SetCtxChannelLayout(codecCtx, int32(config.Channels))

	if avformat.NeedsGlobalHeader(m.formatCtx) {
		flags := avcodec.GetCtxFlags(codecCtx)
		avcodec.SetCtxFlags(codecCtx, flags|avcodec.CodecFlagGlobalHeader)
	}

	// Open codec
	if err := avcodec.Open2(codecCtx, codec, nil); err != nil {
		avcodec.FreeContext(&codecCtx)
		return nil, err
	}

	packet := avcodec.PacketAlloc()
	if packet == nil {
		avcodec.FreeContext(&codecCtx)
		return nil, errors.New("ffgo: failed to allocate audio packet")
	}

	stream := avformat.NewStream(m.formatCtx, codec)
	if stream == nil {
		avcodec.PacketFree(&packet)
		avcodec.FreeContext(&codecCtx)
		return nil, errors.New("ffgo: failed to create audio stream")
	}

	// Copy parameters to stream
	codecPar := avformat.GetStreamCodecPar(stream)
	if err := avcodec.ParametersFromContext(codecPar, codecCtx); err != nil {
		avcodec.PacketFree(&packet)
		avcodec.FreeContext(&codecCtx)
		return nil, err
	}

	ms := &MuxerStream{
		muxer:     m,
		stream:    stream,
		codecCtx:  codecCtx,
		index:     int(avformat.GetStreamIndex(stream)),
		timeBase:  NewRational(1, int32(config.SampleRate)),
		mediaType: MediaTypeAudio,
		encoder: &streamEncoder{
			codecCtx: codecCtx,
			packet:   packet,
		},
	}

	m.streams = append(m.streams, ms)
	return ms, nil
}

// CopyStreamConfig configures a stream for copy mode (no re-encoding).
type CopyStreamConfig struct {
	CodecParameters avcodec.Parameters // Source stream codec parameters
	TimeBase        Rational           // Source stream time base
}

// AddCopyStream adds a stream in copy mode (no re-encoding).
// The codec parameters are copied from the source stream.
func (m *Muxer) AddCopyStream(config *CopyStreamConfig) (*MuxerStream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, closedError("muxer")
	}
	if m.headerWritten {
		return nil, errors.New("ffgo: cannot add streams after header is written")
	}
	if config == nil || config.CodecParameters == nil {
		return nil, errors.New("ffgo: codec parameters are required for copy stream")
	}

	// Create stream
	stream := avformat.NewStream(m.formatCtx, nil)
	if stream == nil {
		return nil, errors.New("ffgo: failed to create copy stream")
	}

	// Copy codec parameters
	codecPar := avformat.GetStreamCodecPar(stream)
	if err := avcodec.ParametersCopy(codecPar, config.CodecParameters); err != nil {
		return nil, err
	}

	// Set time base
	avformat.SetStreamTimeBase(stream, config.TimeBase.Num, config.TimeBase.Den)

	ms := &MuxerStream{
		muxer:     m,
		stream:    stream,
		index:     int(avformat.GetStreamIndex(stream)),
		timeBase:  config.TimeBase,
		mediaType: avformat.GetCodecParType(codecPar),
		copyMode:  true,
	}

	m.streams = append(m.streams, ms)
	return ms, nil
}

// WriteHeader writes the container header.
// Must be called after all streams are added and before writing any frames/packets.
func (m *Muxer) WriteHeader() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return closedError("muxer")
	}
	if m.headerWritten {
		return errors.New("ffgo: header already written")
	}
	if len(m.streams) == 0 {
		return errors.New("ffgo: no streams added")
	}

	return m.writeHeaderWithOptionsLocked(m.headerOptions)
}

// WriteHeaderWithOptions writes the container header with muxer-specific options.
// Options are passed to FFmpeg's avformat_write_header.
func (m *Muxer) WriteHeaderWithOptions(opts map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return closedError("muxer")
	}
	if m.headerWritten {
		return errors.New("ffgo: header already written")
	}
	if len(m.streams) == 0 {
		return errors.New("ffgo: no streams added")
	}

	merged := cloneStringMap(m.headerOptions)
	if merged == nil && len(opts) > 0 {
		merged = make(map[string]string, len(opts))
	}
	for k, v := range opts {
		merged[k] = v
	}
	return m.writeHeaderWithOptionsLocked(merged)
}

func (m *Muxer) writeHeaderWithOptionsLocked(opts map[string]string) error {
	var dict avutil.Dictionary
	for k, v := range opts {
		if v == "" {
			continue
		}
		if err := avutil.DictSet(&dict, k, v, 0); err != nil {
			if dict != nil {
				avutil.DictFree(&dict)
			}
			return err
		}
	}
	defer func() {
		if dict != nil {
			avutil.DictFree(&dict)
		}
	}()

	return m.writeHeaderLocked(&dict)
}

func (m *Muxer) writeHeaderLocked(dict *avutil.Dictionary) error {
	// Formats marked AVFMT_NOFILE, such as HLS and DASH, open and atomically
	// replace their own manifests and segments. Holding the primary path open
	// here prevents those replacements on Windows.
	if !avformat.HasNoFile(m.formatCtx) {
		if err := avformat.IOOpen(&m.ioCtx, m.path, avformat.IOFlagWrite); err != nil {
			return err
		}
		avformat.SetIOContext(m.formatCtx, m.ioCtx)
	}

	// Write header
	if err := avformat.WriteHeader(m.formatCtx, dict); err != nil {
		return err
	}

	m.headerWritten = true
	return nil
}

// WriteFrame encodes and writes a frame to a stream.
// Only valid for streams created with AddVideoStream or AddAudioStream.
func (m *Muxer) WriteFrame(ms *MuxerStream, frame Frame) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return closedError("muxer")
	}
	if !m.headerWritten {
		return errors.New("ffgo: header not written")
	}
	if m.trailerWritten {
		return errors.New("ffgo: trailer already written")
	}
	if ms == nil || ms.muxer != m {
		return errors.New("ffgo: invalid stream")
	}
	if ms.copyMode {
		return errors.New("ffgo: cannot write frames to copy-mode stream, use WritePacket")
	}
	if ms.encoder == nil {
		return errors.New("ffgo: stream has no encoder")
	}
	if err := frame.poolLeaseError(); err != nil {
		return err
	}

	return ms.encoder.state.encode(ms.codecCtx, frame.ptr, ms.encoder.packet, m.packetWriter(ms))
}

// WritePacket writes a packet directly to a stream.
// For copy-mode streams, timestamps should already be in the source time base.
func (m *Muxer) WritePacket(ms *MuxerStream, packet *Packet) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return closedError("muxer")
	}
	if !m.headerWritten {
		return errors.New("ffgo: header not written")
	}
	if m.trailerWritten {
		return errors.New("ffgo: trailer already written")
	}
	if ms == nil || ms.muxer != m {
		return errors.New("ffgo: invalid stream")
	}
	if packet == nil || packet.ptr == nil {
		return errors.New("ffgo: packet cannot be nil")
	}

	// Set stream index
	avcodec.SetPacketStreamIndex(packet.ptr, int32(ms.index))

	// Rescale timestamps for copy mode
	if ms.copyMode {
		streamTbNum, streamTbDen := avformat.GetStreamTimeBase(ms.stream)
		streamTb := NewRational(streamTbNum, streamTbDen)
		avcodec.RescalePacketTS(packet.ptr, ms.timeBase, streamTb)
	}

	// Write packet
	return avformat.InterleavedWriteFrame(m.formatCtx, packet.ptr)
}

// WriteTrailer finalizes the container.
// Must be called after all frames/packets are written.
// It drains every encoded stream through codec EOF before writing the trailer.
func (m *Muxer) WriteTrailer() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return closedError("muxer")
	}
	if !m.headerWritten {
		return errors.New("ffgo: header not written")
	}
	if m.trailerWritten {
		return errors.New("ffgo: trailer already written")
	}

	return m.writeTrailerLocked()
}

func (m *Muxer) writeTrailerLocked() error {
	var trailerErrors []error
	for _, ms := range m.streams {
		if ms.encoder != nil && ms.codecCtx != nil {
			if err := m.flushEncoder(ms); err != nil {
				trailerErrors = append(trailerErrors, fmt.Errorf("ffgo: flush stream %d: %w", ms.index, err))
			}
		}
	}

	if err := avformat.WriteTrailer(m.formatCtx); err != nil {
		trailerErrors = append(trailerErrors, err)
	} else {
		m.trailerWritten = true
	}
	return errors.Join(trailerErrors...)
}

// flushEncoder flushes remaining packets from an encoder.
func (m *Muxer) flushEncoder(ms *MuxerStream) error {
	return ms.encoder.state.encode(ms.codecCtx, nil, ms.encoder.packet, m.packetWriter(ms))
}

func (m *Muxer) packetWriter(ms *MuxerStream) func(avcodec.Packet) error {
	return func(packet avcodec.Packet) error {
		avcodec.SetPacketStreamIndex(packet, int32(ms.index))
		// Some video encoders, including FFmpeg 9's native MPEG-4 encoder,
		// leave CFR packet duration unset. MP4 then excludes the final delayed
		// frame from the track duration and marks its packet for discard.
		if ms.mediaType == MediaTypeVideo && avcodec.GetPacketDuration(packet) <= 0 {
			avcodec.SetPacketDuration(packet, 1)
		}
		streamTbNum, streamTbDen := avformat.GetStreamTimeBase(ms.stream)
		streamTb := NewRational(streamTbNum, streamTbDen)
		avcodec.RescalePacketTS(packet, ms.timeBase, streamTb)
		return avformat.InterleavedWriteFrame(m.formatCtx, packet)
	}
}

// Close releases all resources.
func (m *Muxer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	var closeErr error
	if m.headerWritten && !m.trailerWritten {
		closeErr = m.writeTrailerLocked()
	}
	m.closed = true

	// Free encoder resources
	for _, ms := range m.streams {
		if ms.encoder != nil {
			if ms.encoder.packet != nil {
				avcodec.PacketFree(&ms.encoder.packet)
			}
			if !ms.encoder.frame.IsNil() {
				_ = ms.encoder.frame.Free()
			}
		}
		if ms.codecCtx != nil && !ms.copyMode {
			avcodec.FreeContext(&ms.codecCtx)
		}
	}

	// Close I/O context (errors during cleanup are non-fatal)
	if m.ioCtx != nil {
		_ = avformat.IOCloseP(&m.ioCtx)
	}

	// Free format context
	if m.formatCtx != nil {
		avformat.FreeContext(m.formatCtx)
		m.formatCtx = nil
	}

	return closeErr
}

// Streams returns all streams in the muxer.
func (m *Muxer) Streams() []*MuxerStream {
	m.mu.Lock()
	defer m.mu.Unlock()
	streams := make([]*MuxerStream, len(m.streams))
	copy(streams, m.streams)
	return streams
}

// Index returns the stream index.
func (ms *MuxerStream) Index() int {
	return ms.index
}

// MediaType returns the stream's media type.
func (ms *MuxerStream) MediaType() MediaType {
	return ms.mediaType
}

// TimeBase returns the stream's time base.
func (ms *MuxerStream) TimeBase() Rational {
	return ms.timeBase
}

// IsCopyMode returns true if the stream is in copy mode (no encoding).
func (ms *MuxerStream) IsCopyMode() bool {
	return ms.copyMode
}
