//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
)

// Encoder encodes video and/or audio frames to a file.
//
// Do not call encoder operations concurrently. Close may be called to cancel a
// cooperative context-aware custom I/O callback before final cleanup.
type Encoder struct {
	mu          sync.Mutex
	closeSignal sync.Once

	formatCtx avformat.FormatContext
	ioCtx     avformat.IOContext
	customIO  *CustomIOContext
	path      string

	// Optional: used when I/O is opened lazily (e.g. network outputs) or needs avio_open2 options.
	ioOptions     map[string]string
	headerOptions map[string]string

	// Video encoding
	videoCodecCtx avcodec.Context
	videoStream   avformat.Stream
	videoPacket   avcodec.Packet
	videoState    encoderCodecState

	// Audio encoding
	audioCodecCtx  avcodec.Context
	audioStream    avformat.Stream
	audioPacket    avcodec.Packet
	audioFrameSize int // Number of samples per frame for codec
	audioState     encoderCodecState

	// Stream copy mode
	copyVideo      bool
	copyAudio      bool
	copyStreams    map[int]streamCopyTarget
	videoStreamIdx int // Output video stream index
	audioStreamIdx int // Output audio stream index

	// Deprecated: use videoCodecCtx
	codecCtx avcodec.Context
	// Deprecated: use videoStream
	stream avformat.Stream
	// Deprecated: use videoPacket
	packet avcodec.Packet

	width       int
	height      int
	pixFmt      PixelFormat
	frameCount  int64
	timeBaseNum int32
	timeBaseDen int32

	// Audio properties
	sampleRate    int
	channels      int
	sampleFormat  SampleFormat
	audioFrameCnt int64

	headerWritten bool
	closed        bool
	hasVideo      bool
	hasAudio      bool
}

type streamCopyTarget struct {
	stream         avformat.Stream
	sourceTimeBase Rational
}

// VideoEncoderConfig configures video encoding parameters.
type VideoEncoderConfig struct {
	// Codec specifies the video codec (default: CodecIDH264).
	Codec CodecID

	// Width is the video width in pixels.
	Width int

	// Height is the video height in pixels.
	Height int

	// FrameRate is the target frame rate (default: 30/1).
	FrameRate Rational

	// Bitrate is the target bit rate in bits/second (default: 2000000).
	// Used for ABR/CBR rate control modes.
	Bitrate int64

	// PixelFormat is the pixel format (default: PixelFormatYUV420P).
	PixelFormat PixelFormat

	// GOPSize is the group of pictures size (default: 12).
	GOPSize int

	// MaxBFrames is the maximum number of B-frames (default: 0).
	MaxBFrames int

	// Preset controls speed/quality tradeoff (e.g., PresetMedium, PresetFast).
	// Slower presets produce smaller files. Empty string uses codec default.
	Preset EncoderPreset

	// Tune optimizes for specific content types (e.g., TuneFilm, TuneAnimation).
	// Empty string uses codec default.
	Tune EncoderTune

	// Profile specifies H.264/H.265 profile (e.g., ProfileHigh, ProfileMain).
	// Higher profiles support more features. Empty string uses codec default.
	Profile VideoProfile

	// Level specifies H.264/H.265 level (e.g., Level4_1 for 1080p60).
	// Higher levels support higher resolutions. Empty string uses auto.
	Level VideoLevel

	// RateControl specifies the rate control mode (default: RateControlABR).
	RateControl RateControlMode

	// CRF is the Constant Rate Factor (0-51 for x264, 0-63 for x265).
	// Used when RateControl is RateControlCRF.
	// Lower values = higher quality, larger files. Typical: 18-28.
	CRF int

	// CQP is the Constant Quantization Parameter.
	// Used when RateControl is RateControlCQP.
	CQP int

	// MinBitrate is the minimum bitrate for VBV (bits/second).
	// Used for rate-constrained encoding.
	MinBitrate int64

	// MaxBitrate is the maximum bitrate for VBV (bits/second).
	// Used for rate-constrained encoding.
	MaxBitrate int64

	// BufferSize is the VBV buffer size (bits).
	// Controls rate variation. Larger = more variation allowed.
	BufferSize int64

	// BFrameStrategy controls B-frame placement (0-2).
	// 0=off, 1=fast, 2=best (slower).
	BFrameStrategy int

	// RefFrames is the number of reference frames (1-16).
	// More reference frames = better compression, slower encoding.
	RefFrames int

	// Threads is the number of encoding threads (default: auto).
	// 0 = auto-detect based on CPU cores.
	Threads int

	// CodecOptions allows setting arbitrary codec-specific options.
	// Keys and values are passed directly to av_opt_set.
	// Example: {"x264-params": "rc-lookahead=40"}
	CodecOptions map[string]string
}

// AudioEncoderConfig configures audio encoding parameters.
// Note: Audio encoding is not yet fully implemented.
type AudioEncoderConfig struct {
	// Codec specifies the audio codec (default: CodecIDAACj).
	Codec CodecID

	// SampleRate is the sample rate in Hz (default: 48000).
	SampleRate int

	// Channels is the number of audio channels (default: 2).
	Channels int

	// Bitrate is the target bit rate in bits/second (default: 128000).
	Bitrate int64
}

// StreamCopySource provides source codec parameters for stream copy mode.
type StreamCopySource struct {
	// VideoParams is the video codec parameters from the source stream.
	VideoParams avcodec.Parameters

	// VideoTimeBase is the time base of the source video stream.
	VideoTimeBase Rational

	// VideoStreamIndex is the index reported by source video packets.
	VideoStreamIndex int

	// AudioParams is the audio codec parameters from the source stream.
	AudioParams avcodec.Parameters

	// AudioTimeBase is the time base of the source audio stream.
	AudioTimeBase Rational

	// AudioStreamIndex is the index reported by source audio packets.
	AudioStreamIndex int

	videoInfo *StreamInfo
	audioInfo *StreamInfo
}

// NewStreamCopySource builds stream-copy configuration from decoder stream
// information. Pass nil for a media type that will not be copied. The source
// retains the StreamInfo values until the encoder copies their parameters.
func NewStreamCopySource(video, audio *StreamInfo) *StreamCopySource {
	source := &StreamCopySource{}
	if video != nil {
		source.VideoParams = video.CodecParameters()
		source.VideoStreamIndex = video.Index
		source.VideoTimeBase = video.TimeBase
		source.videoInfo = video
	}
	if audio != nil {
		source.AudioParams = audio.CodecParameters()
		source.AudioStreamIndex = audio.Index
		source.AudioTimeBase = audio.TimeBase
		source.audioInfo = audio
	}
	return source
}

// EncoderOptions configures encoder behavior with separate video and audio settings.
type EncoderOptions struct {
	// Format optionally overrides output format selection (muxer short name, e.g. "flv", "mpegts", "rtp").
	// If empty, ffgo will attempt to guess from the output path/extension.
	Format string

	// IOOptions are passed to avio_open2 when opening the output (useful for streaming/network outputs).
	IOOptions map[string]string

	// MuxerOptions are passed to avformat_write_header.
	MuxerOptions map[string]string

	// Video contains video encoding settings. Required for video output when not copying.
	Video *VideoEncoderConfig

	// Audio contains audio encoding settings. Optional.
	// Note: Audio encoding is not yet fully implemented.
	Audio *AudioEncoderConfig

	// CopyVideo enables video stream copy mode (no re-encoding).
	// When true, SourceStreams.VideoParams must be set.
	CopyVideo bool

	// CopyAudio enables audio stream copy mode (no re-encoding).
	// When true, SourceStreams.AudioParams must be set.
	CopyAudio bool

	// SourceStreams provides codec parameters from the source for stream copy.
	// Required when CopyVideo or CopyAudio is true.
	SourceStreams *StreamCopySource

	// Pass enables 2-pass encoding when set to 1 or 2.
	// 0 disables multi-pass.
	Pass int

	// PassLogFile is the passlogfile base path used by the encoder (e.g. x264/x265).
	// If empty, TwoPassTranscode may generate a temporary base.
	PassLogFile string

	// PassOutput optionally overrides the output path for pass 1.
	// If empty, TwoPassTranscode will create a temporary file.
	PassOutput string
}

func normalizeVideoFrameRate(frameRate Rational) Rational {
	if frameRate.Num <= 0 || frameRate.Den <= 0 {
		return NewRational(30, 1)
	}
	return frameRate
}

func cloneEncoderOptions(opts *EncoderOptions) *EncoderOptions {
	if opts == nil {
		return nil
	}
	clone := *opts
	clone.IOOptions = cloneStringMap(opts.IOOptions)
	clone.MuxerOptions = cloneStringMap(opts.MuxerOptions)
	if opts.Video != nil {
		video := *opts.Video
		video.CodecOptions = cloneStringMap(opts.Video.CodecOptions)
		clone.Video = &video
	}
	if opts.Audio != nil {
		audio := *opts.Audio
		clone.Audio = &audio
	}
	return &clone
}

// NewEncoder creates a new encoder with separate video and audio configuration.
// It supports advanced codec options like presets, profiles, CRF, etc.
// For stream copy mode, set CopyVideo/CopyAudio and provide SourceStreams.
func NewEncoder(path string, opts *EncoderOptions) (*Encoder, error) {
	return newEncoder(path, opts, nil)
}

func newEncoder(path string, opts *EncoderOptions, customIO *CustomIOContext) (*Encoder, error) {
	if opts == nil {
		return nil, errors.New("ffgo: EncoderOptions is required")
	}
	opts = cloneEncoderOptions(opts)

	// Validate options - must have either encoding config or stream copy
	hasVideoEncode := opts.Video != nil
	hasAudioEncode := opts.Audio != nil
	hasVideoCopy := opts.CopyVideo
	hasAudioCopy := opts.CopyAudio

	if !hasVideoEncode && !hasAudioEncode && !hasVideoCopy && !hasAudioCopy {
		return nil, errors.New("ffgo: must specify Video config, Audio config, CopyVideo, or CopyAudio")
	}

	// Validate stream copy options
	if hasVideoCopy && (opts.SourceStreams == nil || opts.SourceStreams.VideoParams == nil) {
		return nil, errors.New("ffgo: SourceStreams.VideoParams required when CopyVideo is true")
	}
	if hasAudioCopy && (opts.SourceStreams == nil || opts.SourceStreams.AudioParams == nil) {
		return nil, errors.New("ffgo: SourceStreams.AudioParams required when CopyAudio is true")
	}
	if hasVideoCopy && opts.SourceStreams.VideoStreamIndex < 0 {
		return nil, errors.New("ffgo: SourceStreams.VideoStreamIndex must be non-negative")
	}
	if hasAudioCopy && opts.SourceStreams.AudioStreamIndex < 0 {
		return nil, errors.New("ffgo: SourceStreams.AudioStreamIndex must be non-negative")
	}
	if hasVideoCopy && (opts.SourceStreams.VideoTimeBase.Num <= 0 || opts.SourceStreams.VideoTimeBase.Den <= 0) {
		return nil, errors.New("ffgo: SourceStreams.VideoTimeBase must be positive")
	}
	if hasAudioCopy && (opts.SourceStreams.AudioTimeBase.Num <= 0 || opts.SourceStreams.AudioTimeBase.Den <= 0) {
		return nil, errors.New("ffgo: SourceStreams.AudioTimeBase must be positive")
	}
	if hasVideoCopy && hasAudioCopy && opts.SourceStreams.VideoStreamIndex == opts.SourceStreams.AudioStreamIndex {
		return nil, errors.New("ffgo: source video and audio stream indices must differ")
	}

	// Ensure FFmpeg is loaded
	if err := bindings.Load(); err != nil {
		return nil, err
	}

	// Handle stream copy mode
	if hasVideoCopy || hasAudioCopy {
		return newEncoderStreamCopy(path, opts, customIO)
	}

	// Clone video config so we can safely inject encoder-specific options (e.g. 2-pass for libx265)
	// without mutating caller-owned config.
	videoCfg := *opts.Video
	video := &videoCfg

	// Apply defaults for encoding mode
	if video.Width <= 0 || video.Height <= 0 {
		return nil, errors.New("ffgo: width and height must be positive")
	}
	pixFmt := video.PixelFormat
	if pixFmt == PixelFormatNone {
		pixFmt = PixelFormatYUV420P
	}
	codecID := video.Codec
	if codecID == CodecIDNone {
		codecID = CodecIDH264
	}
	bitrate := video.Bitrate
	if bitrate <= 0 && video.RateControl != RateControlCRF && video.RateControl != RateControlCQP {
		bitrate = 2000000
	}
	gopSize := video.GOPSize
	if gopSize <= 0 {
		gopSize = 12
	}

	// Handle frame rate
	frameRate := normalizeVideoFrameRate(video.FrameRate)
	timeBase := frameRate.Invert()

	e := &Encoder{
		width:         video.Width,
		height:        video.Height,
		pixFmt:        pixFmt,
		timeBaseNum:   timeBase.Num,
		timeBaseDen:   timeBase.Den,
		hasVideo:      true,
		path:          path,
		ioOptions:     opts.IOOptions,
		headerOptions: opts.MuxerOptions,
		customIO:      customIO,
	}
	if customIO != nil {
		e.ioCtx = customIO.AVIOContext()
	}

	// Determine output format (optionally forced).
	formatName := opts.Format
	if formatName == "" {
		formatName = guessFormatFromPath(path)
	}
	if formatName == "" {
		return nil, errors.New("ffgo: cannot determine output format from filename")
	}

	// Create output format context
	if err := avformat.AllocOutputContext2(&e.formatCtx, nil, formatName, path); err != nil {
		e.cleanup()
		return nil, err
	}
	if customIO != nil {
		avformat.SetIOContext(e.formatCtx, customIO.AVIOContext())
		avformat.AddFlags(e.formatCtx, avformat.AVFMT_FLAG_CUSTOM_IO)
	}

	// Find encoder
	codec := avcodec.FindEncoder(codecID)
	if codec == nil {
		e.cleanup()
		return nil, errors.New("ffgo: encoder not found")
	}

	// Encoder-specific: libx265 does not expose passlogfile/stats AVOptions via FFmpeg,
	// but does support 2-pass via x265-params (pass=N:stats=FILE).
	if opts.Pass != 0 && strings.HasPrefix(avcodec.GetCodecName(codec), "libx265") {
		if video.CodecOptions == nil {
			video.CodecOptions = make(map[string]string)
		}
		// Only inject if user didn't already specify pass/stats.
		xp := video.CodecOptions["x265-params"]
		if !strings.Contains(xp, "pass=") && !strings.Contains(xp, "stats=") {
			if xp != "" && !strings.HasSuffix(xp, ":") {
				xp += ":"
			}
			xp += "pass=" + intToString(opts.Pass) + ":stats=" + opts.PassLogFile
			video.CodecOptions["x265-params"] = xp
		}
	}

	// Create video stream
	e.videoStream = avformat.NewStream(e.formatCtx, codec)
	if e.videoStream == nil {
		e.cleanup()
		return nil, errors.New("ffgo: failed to create stream")
	}
	e.stream = e.videoStream // Backward compatibility

	// Create video codec context
	e.videoCodecCtx = avcodec.AllocContext3(codec)
	if e.videoCodecCtx == nil {
		e.cleanup()
		return nil, errors.New("ffgo: failed to allocate codec context")
	}
	e.codecCtx = e.videoCodecCtx // Backward compatibility

	// Configure basic codec context parameters
	avcodec.SetCtxWidth(e.codecCtx, int32(video.Width))
	avcodec.SetCtxHeight(e.codecCtx, int32(video.Height))
	avcodec.SetCtxPixFmt(e.codecCtx, int32(pixFmt))
	avcodec.SetCtxTimeBase(e.codecCtx, timeBase.Num, timeBase.Den)
	avcodec.SetCtxFramerate(e.codecCtx, frameRate.Num, frameRate.Den)
	avcodec.SetCtxGopSize(e.codecCtx, int32(gopSize))
	avcodec.SetCtxMaxBFrames(e.codecCtx, int32(video.MaxBFrames))

	// Set bitrate for ABR/CBR modes
	if bitrate > 0 {
		avcodec.SetCtxBitRate(e.codecCtx, bitrate)
	}

	// Apply advanced codec options via av_opt_set (before opening codec)
	if err := applyVideoOptions(unsafe.Pointer(e.codecCtx), video); err != nil {
		e.cleanup()
		return nil, err
	}

	// Set global header flag if needed by container format
	if avformat.NeedsGlobalHeader(e.formatCtx) {
		flags := avcodec.GetCtxFlags(e.codecCtx)
		avcodec.SetCtxFlags(e.codecCtx, flags|avcodec.CodecFlagGlobalHeader)
	}

	// Configure multi-pass flags (FFmpeg uses codec context flags, not an option named "pass").
	if opts.Pass != 0 {
		flags := avcodec.GetCtxFlags(e.codecCtx)
		flags &^= (avcodec.CodecFlagPass1 | avcodec.CodecFlagPass2)
		if opts.Pass == 1 {
			flags |= avcodec.CodecFlagPass1
		} else if opts.Pass == 2 {
			flags |= avcodec.CodecFlagPass2
		}
		avcodec.SetCtxFlags(e.codecCtx, flags)
	}

	// Open codec (pass pass/passlogfile via AVDictionary** to ensure the encoder's
	// private options (e.g. libx264/libx265) are applied before priv_data is allocated).
	var openDict avutil.Dictionary
	if opts.Pass != 0 {
		if opts.Pass != 1 && opts.Pass != 2 {
			e.cleanup()
			return nil, errors.New("ffgo: Pass must be 0, 1, or 2")
		}
		if opts.PassLogFile == "" {
			e.cleanup()
			return nil, errors.New("ffgo: PassLogFile is required when Pass is set")
		}
		// Set stats filename (libx264/libx265 accept both keys).
		if err := avutil.DictSet(&openDict, "passlogfile", opts.PassLogFile, 0); err != nil {
			if openDict != nil {
				avutil.DictFree(&openDict)
			}
			e.cleanup()
			return nil, err
		}
		_ = avutil.DictSet(&openDict, "stats", opts.PassLogFile, 0)
	}

	// Open codec
	if err := avcodec.Open2(e.codecCtx, codec, &openDict); err != nil {
		if openDict != nil {
			avutil.DictFree(&openDict)
		}
		e.cleanup()
		return nil, err
	}
	if openDict != nil {
		avutil.DictFree(&openDict)
	}

	// Copy codec parameters to stream
	codecPar := avformat.GetStreamCodecPar(e.stream)
	if err := avcodec.ParametersFromContext(codecPar, e.codecCtx); err != nil {
		e.cleanup()
		return nil, err
	}

	// Set stream time base
	avformat.SetStreamTimeBase(e.stream, timeBase.Num, timeBase.Den)

	// Open output file if needed
	if customIO == nil && !avformat.HasNoFile(e.formatCtx) {
		// For network-style outputs (or when IOOptions are provided), open lazily on header write.
		// This avoids connecting during encoder construction.
		if !looksLikeURL(path) && len(opts.IOOptions) == 0 {
			if err := avformat.IOOpen(&e.ioCtx, path, avformat.IOFlagWrite); err != nil {
				e.cleanup()
				return nil, err
			}
			avformat.SetIOContext(e.formatCtx, e.ioCtx)
		}
	}

	// Allocate video packet
	e.videoPacket = avcodec.PacketAlloc()
	if e.videoPacket == nil {
		e.cleanup()
		return nil, errors.New("ffgo: failed to allocate packet")
	}
	e.packet = e.videoPacket // Backward compatibility

	// Setup audio if configured
	if opts.Audio != nil {
		if err := e.setupAudio(opts.Audio); err != nil {
			e.Close()
			return nil, err
		}
	}

	return e, nil
}

func intToString(v int) string {
	switch v {
	case 1:
		return "1"
	case 2:
		return "2"
	default:
		return ""
	}
}

func looksLikeURL(s string) bool {
	return strings.Contains(s, "://")
}

func (e *Encoder) ensureIOOpenLocked() error {
	if e.closed {
		return ErrEncoderClosed
	}
	if e.formatCtx == nil {
		return errors.New("ffgo: encoder is not initialized")
	}
	if avformat.HasNoFile(e.formatCtx) {
		return nil
	}
	if e.ioCtx != nil {
		return nil
	}
	if e.path == "" {
		return errors.New("ffgo: output path is not set")
	}

	// Build IO options for avio_open2 if provided.
	if len(e.ioOptions) > 0 {
		var dict avutil.Dictionary
		for k, v := range e.ioOptions {
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
		err := avformat.IOOpen2(&e.ioCtx, e.path, avformat.IOFlagWrite, &dict)
		if dict != nil {
			avutil.DictFree(&dict)
		}
		if err != nil {
			return err
		}
		avformat.SetIOContext(e.formatCtx, e.ioCtx)
		return nil
	}

	if err := avformat.IOOpen(&e.ioCtx, e.path, avformat.IOFlagWrite); err != nil {
		return err
	}
	avformat.SetIOContext(e.formatCtx, e.ioCtx)
	return nil
}

func (e *Encoder) writeHeaderLocked() error {
	if e.headerWritten {
		return nil
	}
	if err := e.ensureIOOpenLocked(); err != nil {
		return err
	}

	var dict avutil.Dictionary
	for k, v := range e.headerOptions {
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

	if err := e.writeOutputHeaderLocked(&dict); err != nil {
		return err
	}
	e.headerWritten = true
	return nil
}

// newEncoderStreamCopy creates an encoder in stream copy mode.
// Packets are copied directly without decoding/encoding.
func newEncoderStreamCopy(path string, opts *EncoderOptions, customIO *CustomIOContext) (*Encoder, error) {
	// Determine output format (optionally forced).
	formatName := ""
	if opts != nil {
		formatName = opts.Format
	}
	if formatName == "" {
		formatName = guessFormatFromPath(path)
	}
	if formatName == "" {
		return nil, errors.New("ffgo: cannot determine output format from filename")
	}

	e := &Encoder{
		copyVideo:      opts.CopyVideo,
		copyAudio:      opts.CopyAudio,
		copyStreams:    make(map[int]streamCopyTarget, 2),
		videoStreamIdx: -1,
		audioStreamIdx: -1,
		path:           path,
		ioOptions:      opts.IOOptions,
		headerOptions:  opts.MuxerOptions,
		customIO:       customIO,
	}
	if customIO != nil {
		e.ioCtx = customIO.AVIOContext()
	}

	// Create output format context
	if err := avformat.AllocOutputContext2(&e.formatCtx, nil, formatName, path); err != nil {
		e.cleanup()
		return nil, err
	}
	if customIO != nil {
		avformat.SetIOContext(e.formatCtx, customIO.AVIOContext())
		avformat.AddFlags(e.formatCtx, avformat.AVFMT_FLAG_CUSTOM_IO)
	}

	// Setup video stream for copy mode
	if opts.CopyVideo && opts.SourceStreams != nil && opts.SourceStreams.VideoParams != nil {
		// Create stream without codec
		stream := avformat.NewStream(e.formatCtx, nil)
		if stream == nil {
			e.cleanup()
			return nil, errors.New("ffgo: failed to create video stream for copy")
		}
		e.videoStream = stream
		e.videoStreamIdx = int(avformat.GetStreamIndex(stream))

		// Copy codec parameters from source
		codecPar := avformat.GetStreamCodecPar(stream)
		err := avcodec.ParametersCopy(codecPar, opts.SourceStreams.VideoParams)
		runtime.KeepAlive(opts.SourceStreams)
		if err != nil {
			e.cleanup()
			return nil, errors.New("ffgo: failed to copy video codec parameters")
		}

		// Request the source time base. The muxer may adjust it when writing the
		// header, so WritePacket reads the final destination value later.
		avformat.SetStreamTimeBase(stream, opts.SourceStreams.VideoTimeBase.Num, opts.SourceStreams.VideoTimeBase.Den)
		e.copyStreams[opts.SourceStreams.VideoStreamIndex] = streamCopyTarget{
			stream:         stream,
			sourceTimeBase: opts.SourceStreams.VideoTimeBase,
		}
		e.hasVideo = true
	}

	// Setup audio stream for copy mode
	if opts.CopyAudio && opts.SourceStreams != nil && opts.SourceStreams.AudioParams != nil {
		// Create stream without codec
		stream := avformat.NewStream(e.formatCtx, nil)
		if stream == nil {
			e.cleanup()
			return nil, errors.New("ffgo: failed to create audio stream for copy")
		}
		e.audioStream = stream
		e.audioStreamIdx = int(avformat.GetStreamIndex(stream))

		// Copy codec parameters from source
		codecPar := avformat.GetStreamCodecPar(stream)
		err := avcodec.ParametersCopy(codecPar, opts.SourceStreams.AudioParams)
		runtime.KeepAlive(opts.SourceStreams)
		if err != nil {
			e.cleanup()
			return nil, errors.New("ffgo: failed to copy audio codec parameters")
		}

		avformat.SetStreamTimeBase(stream, opts.SourceStreams.AudioTimeBase.Num, opts.SourceStreams.AudioTimeBase.Den)
		e.copyStreams[opts.SourceStreams.AudioStreamIndex] = streamCopyTarget{
			stream:         stream,
			sourceTimeBase: opts.SourceStreams.AudioTimeBase,
		}
		e.hasAudio = true
	}

	// Setup audio encoding if CopyVideo but encoding audio
	if opts.CopyVideo && opts.Audio != nil && !opts.CopyAudio {
		if err := e.setupAudio(opts.Audio); err != nil {
			e.Close()
			return nil, err
		}
	}
	// Open output file if needed
	if customIO == nil && !avformat.HasNoFile(e.formatCtx) {
		if !looksLikeURL(path) && len(opts.IOOptions) == 0 {
			if err := avformat.IOOpen(&e.ioCtx, path, avformat.IOFlagWrite); err != nil {
				e.cleanup()
				return nil, err
			}
			avformat.SetIOContext(e.formatCtx, e.ioCtx)
		}
	}

	// Allocate packet for WritePacket
	e.videoPacket = avcodec.PacketAlloc()
	if e.videoPacket == nil {
		e.cleanup()
		return nil, errors.New("ffgo: failed to allocate packet")
	}

	return e, nil
}

// WritePacket writes a packet directly to the output (for stream copy mode).
// The packet's stream index should match the source stream.
// For video packets, set streamIndex to match the source video stream.
// For audio packets, set streamIndex to match the source audio stream.
// WritePacket retains its own packet reference and leaves packet unchanged.
func (e *Encoder) WritePacket(packet *Packet) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrEncoderClosed
	}

	if !e.copyVideo && !e.copyAudio {
		return errors.New("ffgo: WritePacket only available in stream copy mode")
	}

	if packet == nil || packet.ptr == nil {
		return errors.New("ffgo: packet cannot be nil")
	}

	sourceStreamIndex := int(avcodec.GetPacketStreamIndex(packet.ptr))
	target, ok := e.copyStreams[sourceStreamIndex]
	if !ok {
		return fmt.Errorf("ffgo: source stream %d is not configured for copy", sourceStreamIndex)
	}

	// Write header if not yet written. This can change the destination time
	// base, so read that value only after the header succeeds.
	if !e.headerWritten {
		if err := e.writeHeaderLocked(); err != nil {
			return err
		}
	}

	avcodec.PacketUnref(e.videoPacket)
	if err := avcodec.PacketRef(e.videoPacket, packet.ptr); err != nil {
		return err
	}
	defer avcodec.PacketUnref(e.videoPacket)

	dstNum, dstDen := avformat.GetStreamTimeBase(target.stream)
	avcodec.RescalePacketTS(e.videoPacket, target.sourceTimeBase, NewRational(dstNum, dstDen))

	avcodec.SetPacketStreamIndex(e.videoPacket, avformat.GetStreamIndex(target.stream))

	return e.writeOutputPacketLocked(e.videoPacket)
}

// applyVideoOptions applies advanced video encoding options via av_opt_set.
// This must be called BEFORE avcodec_open2.
func applyVideoOptions(ctx unsafe.Pointer, cfg *VideoEncoderConfig) error {
	if ctx == nil {
		return nil
	}

	// Preset (speed/quality tradeoff)
	if cfg.Preset != "" {
		if err := avutil.OptSet(ctx, "preset", string(cfg.Preset), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			// Some codecs don't support preset, ignore error
			_ = err
		}
	}

	// Tune (content-specific optimization)
	if cfg.Tune != "" {
		if err := avutil.OptSet(ctx, "tune", string(cfg.Tune), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}

	// Profile
	if cfg.Profile != "" {
		if err := avutil.OptSet(ctx, "profile", string(cfg.Profile), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}

	// Level
	if cfg.Level != "" {
		if err := avutil.OptSet(ctx, "level", string(cfg.Level), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}

	// Rate control
	switch cfg.RateControl {
	case RateControlCRF:
		if cfg.CRF > 0 {
			if err := avutil.OptSetInt(ctx, "crf", int64(cfg.CRF), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
				_ = err
			}
		}
	case RateControlCQP:
		if cfg.CQP > 0 {
			if err := avutil.OptSetInt(ctx, "qp", int64(cfg.CQP), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
				_ = err
			}
		}
	}

	// VBV buffer settings (for CBR/constrained VBR)
	if cfg.MinBitrate > 0 {
		if err := avutil.OptSetInt(ctx, "minrate", cfg.MinBitrate, avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}
	if cfg.MaxBitrate > 0 {
		if err := avutil.OptSetInt(ctx, "maxrate", cfg.MaxBitrate, avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}
	if cfg.BufferSize > 0 {
		if err := avutil.OptSetInt(ctx, "bufsize", cfg.BufferSize, avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}
	if cfg.BFrameStrategy > 0 {
		if err := avutil.OptSetInt(ctx, "b_strategy", int64(cfg.BFrameStrategy), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}

	// Reference frames
	if cfg.RefFrames > 0 {
		if err := avutil.OptSetInt(ctx, "refs", int64(cfg.RefFrames), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}

	// Threading
	if cfg.Threads > 0 {
		if err := avutil.OptSetInt(ctx, "threads", int64(cfg.Threads), avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			_ = err
		}
	}

	// Custom codec options
	for key, value := range cfg.CodecOptions {
		if err := avutil.OptSet(ctx, key, value, avutil.AV_OPT_SEARCH_CHILDREN); err != nil {
			return fmt.Errorf("ffgo: set codec option %q: %w", key, err)
		}
	}

	return nil
}

// setupAudio adds an audio stream to the encoder.
func (e *Encoder) setupAudio(cfg *AudioEncoderConfig) error {
	// Apply defaults
	codecID := cfg.Codec
	if codecID == CodecIDNone {
		codecID = CodecIDAAC
	}
	sampleRate := cfg.SampleRate
	if sampleRate <= 0 {
		sampleRate = 48000
	}
	channels := cfg.Channels
	if channels <= 0 {
		channels = 2
	}
	bitrate := cfg.Bitrate
	if bitrate <= 0 {
		bitrate = 128000
	}

	// Find audio encoder
	audioCodec := avcodec.FindEncoder(codecID)
	if audioCodec == nil {
		return errors.New("ffgo: audio encoder not found")
	}

	// Create audio stream
	e.audioStream = avformat.NewStream(e.formatCtx, audioCodec)
	if e.audioStream == nil {
		return errors.New("ffgo: failed to create audio stream")
	}

	// Create audio codec context
	e.audioCodecCtx = avcodec.AllocContext3(audioCodec)
	if e.audioCodecCtx == nil {
		return errors.New("ffgo: failed to allocate audio codec context")
	}

	// Configure audio codec context
	avcodec.SetCtxSampleRate(e.audioCodecCtx, int32(sampleRate))
	avcodec.SetCtxChannelLayout(e.audioCodecCtx, int32(channels))     // FFmpeg 5.1+ requires ch_layout
	avcodec.SetCtxSampleFmt(e.audioCodecCtx, int32(SampleFormatFLTP)) // AAC requires FLTP
	avcodec.SetCtxBitRate(e.audioCodecCtx, bitrate)
	avcodec.SetCtxTimeBase(e.audioCodecCtx, 1, int32(sampleRate))

	// Set global header flag if needed
	if avformat.NeedsGlobalHeader(e.formatCtx) {
		flags := avcodec.GetCtxFlags(e.audioCodecCtx)
		avcodec.SetCtxFlags(e.audioCodecCtx, flags|avcodec.CodecFlagGlobalHeader)
	}

	// Open audio codec
	if err := avcodec.Open2(e.audioCodecCtx, audioCodec, nil); err != nil {
		avcodec.FreeContext(&e.audioCodecCtx)
		return err
	}

	// Copy codec parameters to stream
	codecPar := avformat.GetStreamCodecPar(e.audioStream)
	if err := avcodec.ParametersFromContext(codecPar, e.audioCodecCtx); err != nil {
		return err
	}

	// Set stream time base
	avformat.SetStreamTimeBase(e.audioStream, 1, int32(sampleRate))

	// Allocate audio packet
	e.audioPacket = avcodec.PacketAlloc()
	if e.audioPacket == nil {
		return errors.New("ffgo: failed to allocate audio packet")
	}

	// Store audio properties
	e.sampleRate = sampleRate
	e.channels = channels
	e.sampleFormat = SampleFormatFLTP
	e.hasAudio = true

	// Get frame size from codec (needed for encoding)
	e.audioFrameSize = avcodec.GetCtxFrameSize(e.audioCodecCtx)

	return nil
}

// WriteHeader writes the file header. Must be called before WriteFrame.
func (e *Encoder) WriteHeader() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrEncoderClosed
	}
	return e.writeHeaderLocked()
}

// WriteFrame encodes and writes a frame.
// The frame must have the correct format, width, and height.
func (e *Encoder) WriteFrame(frame Frame) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrEncoderClosed
	}
	if err := frame.poolLeaseError(); err != nil {
		return err
	}

	// Auto-write header if not done
	if !e.headerWritten {
		if err := e.writeHeaderLocked(); err != nil {
			return err
		}
	}

	if frame.ptr == nil {
		return e.flushEncodersLocked()
	}
	return e.encodeVideoFrameLocked(frame)
}

// WriteVideoFrame encodes and writes a video frame.
// This is an alias for WriteFrame for semantic clarity.
func (e *Encoder) WriteVideoFrame(frame Frame) error {
	return e.WriteFrame(frame)
}

// WriteAudioFrame encodes and writes an audio frame.
func (e *Encoder) WriteAudioFrame(frame Frame) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrEncoderClosed
	}
	if err := frame.poolLeaseError(); err != nil {
		return err
	}
	if !e.hasAudio {
		return errors.New("ffgo: encoder was not configured with audio")
	}
	if e.audioCodecCtx == nil {
		return errors.New("ffgo: audio codec context not initialized")
	}

	// Ensure header is written
	if !e.headerWritten {
		if err := e.writeHeaderLocked(); err != nil {
			return err
		}
	}

	return e.encodeAudioFrameLocked(frame)
}

// Flush flushes every configured encoder and writes all delayed packets.
// It is idempotent. No more frames may be written after the first successful flush.
func (e *Encoder) Flush() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrEncoderClosed
	}
	if !e.headerWritten {
		if err := e.writeHeaderLocked(); err != nil {
			return err
		}
	}
	return e.flushEncodersLocked()
}

// Width returns the encoder width.
func (e *Encoder) Width() int {
	return e.width
}

// Height returns the encoder height.
func (e *Encoder) Height() int {
	return e.height
}

// PixelFormat returns the encoder pixel format.
func (e *Encoder) PixelFormat() PixelFormat {
	return e.pixFmt
}

// FrameCount returns the number of frames written.
func (e *Encoder) FrameCount() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.frameCount
}

// HasAudio returns true if the encoder has audio.
func (e *Encoder) HasAudio() bool {
	return e.hasAudio
}

// HasVideo returns true if the encoder has video.
func (e *Encoder) HasVideo() bool {
	return e.hasVideo
}

// AudioFrameSize returns the number of samples per audio frame.
// This is needed when creating audio frames for encoding.
// Returns 0 if no audio is configured.
func (e *Encoder) AudioFrameSize() int {
	return e.audioFrameSize
}

// SampleRate returns the audio sample rate.
// Returns 0 if no audio is configured.
func (e *Encoder) SampleRate() int {
	return e.sampleRate
}

// Channels returns the number of audio channels.
// Returns 0 if no audio is configured.
func (e *Encoder) Channels() int {
	return e.channels
}

// SampleFormat returns the audio sample format.
// Returns SampleFormatNone if no audio is configured.
func (e *Encoder) AudioSampleFormat() SampleFormat {
	return e.sampleFormat
}

// Close finalizes and closes the encoder.
func (e *Encoder) Close() error {
	e.closeSignal.Do(func() {
		if e.customIO != nil {
			e.customIO.cancelPending()
		}
	})
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true
	if e.customIO != nil {
		// Close canceled an in-flight callback before waiting for the mutex.
		// Use a fresh lifetime for this call's own flush and trailer writes.
		e.customIO.resetCancellation()
	}

	var closeErrors []error
	if e.headerWritten {
		if err := e.flushEncodersLocked(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}

	// Write trailer
	if e.formatCtx != nil && e.headerWritten {
		if err := e.writeOutputTrailerLocked(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}

	e.cleanup()
	return errors.Join(closeErrors...)
}

// cleanup releases all resources.
func (e *Encoder) cleanup() {
	// Free video packet
	if e.videoPacket != nil {
		avcodec.PacketFree(&e.videoPacket)
	}
	// Also clear deprecated alias
	e.packet = nil

	// Free video codec context
	if e.videoCodecCtx != nil {
		avcodec.FreeContext(&e.videoCodecCtx)
	}
	// Also clear deprecated alias
	e.codecCtx = nil

	// Free audio packet
	if e.audioPacket != nil {
		avcodec.PacketFree(&e.audioPacket)
	}

	// Free audio codec context
	if e.audioCodecCtx != nil {
		avcodec.FreeContext(&e.audioCodecCtx)
	}

	// Close I/O context (errors during cleanup are non-fatal).
	if e.customIO != nil {
		if e.formatCtx != nil {
			avformat.SetIOContext(e.formatCtx, nil)
		}
		_ = e.customIO.Close()
		e.customIO = nil
		e.ioCtx = nil
	} else if e.ioCtx != nil && e.formatCtx != nil {
		_ = avformat.IOCloseP(&e.ioCtx)
	}

	// Free format context
	if e.formatCtx != nil {
		avformat.FreeContext(e.formatCtx)
		e.formatCtx = nil
	}
}

// guessFormatFromPath determines the output format from filename extension.
func guessFormatFromPath(path string) string {
	// A pattern in a parent directory is part of the path, not the output name.
	base := path
	if separator := strings.LastIndexAny(path, `/\`); separator >= 0 {
		base = path[separator+1:]
	}
	if isImageSequencePattern(base) {
		return "image2"
	}

	// Get extension
	ext := ""
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			ext = path[i+1:]
			break
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	ext = strings.ToLower(ext)

	// Map common extensions to FFmpeg format names
	switch ext {
	case "mp4", "m4v":
		return "mp4"
	case "mkv":
		return "matroska"
	case "webm":
		return "webm"
	case "avi":
		return "avi"
	case "mov":
		return "mov"
	case "flv":
		return "flv"
	case "ts", "m2ts":
		return "mpegts"
	case "mpg", "mpeg":
		return "mpeg"
	case "ogg", "ogv":
		return "ogg"
	case "gif":
		return "gif"
	case "png":
		return "image2"
	case "jpg", "jpeg":
		return "image2"
	case "bmp":
		return "image2"
	default:
		return ""
	}
}

// isImageSequencePattern checks if path contains printf-style format specifiers
// like %d, %04d, etc. that indicate an image sequence pattern.
func isImageSequencePattern(path string) bool {
	for i := 0; i < len(path)-1; i++ {
		if path[i] == '%' {
			// Check if followed by digits and 'd'
			j := i + 1
			// Skip width specifier (e.g., "04" in "%04d")
			for j < len(path) && path[j] >= '0' && path[j] <= '9' {
				j++
			}
			// Must end with 'd'
			if j < len(path) && path[j] == 'd' {
				return true
			}
		}
	}
	return false
}
