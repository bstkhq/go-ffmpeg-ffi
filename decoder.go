//go:build amd64 || arm64

package ffmpeg

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
)

// Decoder decodes media files.
//
// Do not call decoder operations concurrently. Close is the exception: it may
// be called concurrently to interrupt a blocking read. Frames and packets
// returned without Copy or Clone are borrowed and must not outlive the next
// decoder operation.
type Decoder struct {
	mu          sync.Mutex
	closeSignal sync.Once

	formatCtx     avformat.FormatContext
	videoCodecCtx avcodec.Context
	audioCodecCtx avcodec.Context
	packet        avcodec.Packet
	frame         avutil.Frame

	videoStreamIdx int
	audioStreamIdx int

	videoInfo       *StreamInfo
	audioInfo       *StreamInfo
	streamInfoCache map[int]*StreamInfo

	videoDecoderOpen bool
	audioDecoderOpen bool
	videoState       decoderCodecState
	audioState       decoderCodecState
	packetQueue      decoderPacketQueue
	packetPool       decoderPacketPool
	demuxEOF         bool
	activeMedia      MediaType
	prefetchedFrame  avutil.Frame
	prefetchedMedia  MediaType
	customIO         *CustomIOContext
	interrupt        *decoderInterrupt
	cleanup          func()
	closed           bool
}

func newDecoder(interrupt *decoderInterrupt) *Decoder {
	return &Decoder{
		videoStreamIdx:  -1,
		audioStreamIdx:  -1,
		interrupt:       interrupt,
		streamInfoCache: make(map[int]*StreamInfo),
	}
}

// allocateDecodeResources installs the reusable packet and frame required by
// every Decoder entry point. Keep this allocation centralized so constructors
// for files, custom I/O, and capture devices cannot publish a partial Decoder.
func (d *Decoder) allocateDecodeResources() error {
	packet := avcodec.PacketAlloc()
	if packet == nil {
		return errors.New("ffmpeg: failed to allocate packet")
	}

	frame := avutil.FrameAlloc()
	if frame == nil {
		avcodec.PacketFree(&packet)
		return errors.New("ffmpeg: failed to allocate frame")
	}

	d.packet = packet
	d.frame = frame
	return nil
}

// DecoderOptions configures decoder behavior.
type DecoderOptions struct {
	// Format hint (e.g., "mp4", "mkv") - optional
	Format string

	// FFmpeg options passed to avformat_open_input
	AVOptions map[string]string

	// Typed probing controls (mapped to avformat_open_input AVDictionary).
	ProbeSizeBytes  int
	AnalyzeDuration time.Duration
	MaxProbePackets int
	FormatWhitelist []string
	CodecWhitelist  []string

	// ProbeScore, when >0, requires the detected probe score to be at least this value.
	// If TryMultipleFormats is enabled, ffmpeg will attempt additional forced demuxers when the
	// auto-detected probe score is below this threshold.
	ProbeScore int

	// FormatBlacklist excludes demuxers from TryMultipleFormats attempts.
	// Note: this is an ffmpeg-side filter; it is not passed as an avformat_open_input option.
	FormatBlacklist []string

	// TryMultipleFormats, when true, retries avformat_open_input with forced demuxers if
	// auto-detection fails or yields a low probe score.
	TryMultipleFormats bool

	// Streams specifies which stream types to decode (nil = all streams).
	// Packets for every selected stream are retained until consumed. Select only
	// the streams you need when using DecodeVideo or DecodeAudio exclusively.
	Streams []MediaType

	// ProgramID selects a specific program to decode in multi-program inputs (e.g. MPEG-TS).
	// When set, ffmpeg will pick the best video/audio streams within the program.
	ProgramID int

	// HWDevice specifies the hardware device for hardware acceleration (e.g., "cuda", "vaapi")
	HWDevice string
}

func buildDecoderAVOptions(opts *DecoderOptions) map[string]string {
	if opts == nil {
		return nil
	}
	out := make(map[string]string, len(opts.AVOptions)+8)
	for k, v := range opts.AVOptions {
		out[k] = v
	}
	// Typed fields override raw AVOptions for clarity/determinism.
	if opts.ProbeSizeBytes > 0 {
		out["probesize"] = strconv.Itoa(opts.ProbeSizeBytes)
	}
	if opts.AnalyzeDuration > 0 {
		out["analyzeduration"] = strconv.FormatInt(opts.AnalyzeDuration.Microseconds(), 10)
	}
	if opts.MaxProbePackets > 0 {
		out["max_probe_packets"] = strconv.Itoa(opts.MaxProbePackets)
	}
	if len(opts.FormatWhitelist) > 0 {
		out["format_whitelist"] = strings.Join(opts.FormatWhitelist, ",")
	}
	if len(opts.CodecWhitelist) > 0 {
		out["codec_whitelist"] = strings.Join(opts.CodecWhitelist, ",")
	}
	return out
}

func cloneDecoderOptions(opts *DecoderOptions) *DecoderOptions {
	if opts == nil {
		return &DecoderOptions{}
	}
	clone := *opts
	clone.AVOptions = cloneStringMap(opts.AVOptions)
	clone.FormatWhitelist = append([]string(nil), opts.FormatWhitelist...)
	clone.CodecWhitelist = append([]string(nil), opts.CodecWhitelist...)
	clone.FormatBlacklist = append([]string(nil), opts.FormatBlacklist...)
	clone.Streams = append([]MediaType(nil), opts.Streams...)
	return &clone
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

// NewDecoder opens a media file for decoding.
func NewDecoder(path string, opts *DecoderOptions) (*Decoder, error) {
	return NewDecoderContext(context.Background(), path, opts)
}

// NewDecoderContext opens a media file and allows FFmpeg probing and I/O to be canceled.
func NewDecoderContext(ctx context.Context, path string, opts *DecoderOptions) (*Decoder, error) {
	if ctx == nil {
		return nil, errors.New("ffmpeg: context cannot be nil")
	}
	// Ensure FFmpeg is loaded
	if err := bindings.Load(); err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &DecoderOptions{}
	}

	d := newDecoder(newDecoderInterrupt())
	if err := d.beginInterrupt(ctx); err != nil {
		d.interrupt.release(nil)
		return nil, err
	}
	defer d.clearInterrupt()

	// Open input file (with optional retry logic for ambiguous probing).
	var err error
	d.formatCtx, err = openInputWithRetries(path, opts, d.interrupt)
	if err != nil {
		d.interrupt.release(d.formatCtx)
		return nil, err
	}

	// Find stream info
	if err := d.interrupt.finish(avformat.FindStreamInfo(d.formatCtx, nil)); err != nil {
		d.interrupt.release(d.formatCtx)
		avformat.CloseInput(&d.formatCtx)
		return nil, err
	}

	// Stream selection.
	wantVideo, wantAudio := true, true
	if len(opts.Streams) > 0 {
		wantVideo, wantAudio = false, false
		for _, mt := range opts.Streams {
			switch mt {
			case MediaTypeVideo:
				wantVideo = true
			case MediaTypeAudio:
				wantAudio = true
			}
		}
	}

	if opts != nil && opts.ProgramID > 0 {
		if err := d.selectProgramStreams(opts.ProgramID, wantVideo, wantAudio); err != nil {
			d.Close()
			return nil, err
		}
	} else {
		if wantVideo {
			d.videoStreamIdx = int(avformat.FindBestStream(d.formatCtx, avutil.MediaTypeVideo, -1, -1, nil, 0))
			if d.videoStreamIdx >= 0 {
				info, err := d.getStreamInfo(d.videoStreamIdx)
				if err != nil {
					d.Close()
					return nil, err
				}
				d.videoInfo = info
			}
		}

		if wantAudio {
			d.audioStreamIdx = int(avformat.FindBestStream(d.formatCtx, avutil.MediaTypeAudio, -1, -1, nil, 0))
			if d.audioStreamIdx >= 0 {
				info, err := d.getStreamInfo(d.audioStreamIdx)
				if err != nil {
					d.Close()
					return nil, err
				}
				d.audioInfo = info
			}
		}
	}

	if err := d.allocateDecodeResources(); err != nil {
		d.Close()
		return nil, err
	}

	return d, nil
}

// getStreamInfo extracts stream information.
func (d *Decoder) getStreamInfo(streamIdx int) (*StreamInfo, error) {
	if info, ok := d.streamInfoCache[streamIdx]; ok {
		return info, nil
	}
	stream := avformat.GetStream(d.formatCtx, streamIdx)
	if stream == nil {
		return nil, nil
	}

	codecPar := avformat.GetStreamCodecPar(stream)
	if codecPar == nil {
		return nil, nil
	}
	ownedCodecPar, err := ownCodecParameters(codecPar)
	if err != nil {
		return nil, err
	}

	codecType := avformat.GetCodecParType(codecPar)
	codecID := avformat.GetCodecParCodecID(codecPar)

	// Get time base
	tbNum, tbDen := avformat.GetStreamTimeBase(stream)

	// Get codec name
	var codecName string
	if codec := avcodec.FindDecoder(codecID); codec != nil {
		codecName = avcodec.GetCodecName(codec)
	}

	info := &StreamInfo{
		Index:     streamIdx,
		Type:      codecType,
		CodecID:   codecID,
		CodecName: codecName,
		TimeBase:  avutil.NewRational(tbNum, tbDen),
		codecPar:  ownedCodecPar,
	}

	if codecType == avutil.MediaTypeVideo {
		info.Width = int(avformat.GetCodecParWidth(codecPar))
		info.Height = int(avformat.GetCodecParHeight(codecPar))
		info.PixelFmt = PixelFormat(avformat.GetCodecParFormat(codecPar))

		// Get frame rate
		frNum, frDen := avformat.GetStreamAvgFrameRate(stream)
		info.FrameRate = avutil.NewRational(frNum, frDen)
	} else if codecType == avutil.MediaTypeAudio {
		info.SampleRate = int(avformat.GetCodecParSampleRate(codecPar))
		info.Channels = int(avformat.GetCodecParChannels(codecPar))
	}

	if d.streamInfoCache == nil {
		d.streamInfoCache = make(map[int]*StreamInfo)
	}
	d.streamInfoCache[streamIdx] = info
	return info, nil
}

// VideoStream returns information about the video stream.
// Returns nil if no video stream is present.
func (d *Decoder) VideoStream() *StreamInfo {
	return d.videoInfo
}

// AudioStream returns information about the audio stream.
// Returns nil if no audio stream is present.
func (d *Decoder) AudioStream() *StreamInfo {
	return d.audioInfo
}

// HasVideo returns true if the file has a video stream.
func (d *Decoder) HasVideo() bool {
	return d.videoStreamIdx >= 0
}

// HasAudio returns true if the file has an audio stream.
func (d *Decoder) HasAudio() bool {
	return d.audioStreamIdx >= 0
}

// NumStreams returns the total number of streams.
func (d *Decoder) NumStreams() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.formatCtx == nil {
		return 0
	}
	return avformat.GetNbStreams(d.formatCtx)
}

// Duration returns the duration as time.Duration.
func (d *Decoder) Duration() time.Duration {
	us := d.DurationMicroseconds()
	if us <= 0 {
		return 0
	}
	return time.Duration(us) * time.Microsecond
}

// DurationMicroseconds returns the duration in microseconds (AV_TIME_BASE units).
func (d *Decoder) DurationMicroseconds() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.durationMicrosecondsLocked()
}

func (d *Decoder) durationMicrosecondsLocked() int64 {
	if d.formatCtx == nil {
		return 0
	}
	return avformat.GetDuration(d.formatCtx)
}

// BitRate returns the bit rate.
func (d *Decoder) BitRate() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.formatCtx == nil {
		return 0
	}
	return avformat.GetBitRate(d.formatCtx)
}

// ReadPacket reads the next packet from the file.
// Returns (nil, nil) on EOF.
//
// The returned packet is BORROWED (decoder-owned and internally reused).
// Do not free it; if you need to keep it, call PacketClone().
func (d *Decoder) ReadPacket() (*Packet, error) {
	return d.ReadPacketContext(context.Background())
}

// ReadPacketContext reads the next packet and interrupts blocking FFmpeg I/O when ctx is canceled.
func (d *Decoder) ReadPacketContext(ctx context.Context) (*Packet, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, errDecoderClosed
	}
	if err := d.beginInterrupt(ctx); err != nil {
		return nil, err
	}
	defer d.clearInterrupt()

	packet, err := d.readPacketLocked()
	return packet, d.finishInterrupt(err)
}

// OpenVideoDecoder opens a codec context for video decoding.
// Must be called before DecodeVideoPacket.
func (d *Decoder) OpenVideoDecoder() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.openVideoDecoderLocked()
}

func (d *Decoder) openVideoDecoderLocked() error {
	if d.closed {
		return errDecoderClosed
	}
	if d.videoStreamIdx < 0 {
		return ErrNoVideoStream
	}

	if d.videoDecoderOpen {
		return nil // Already opened
	}

	stream := avformat.GetStream(d.formatCtx, d.videoStreamIdx)
	codecPar := avformat.GetStreamCodecPar(stream)
	codecID := avformat.GetCodecParCodecID(codecPar)

	// Find decoder
	codec := avcodec.FindDecoder(codecID)
	if codec == nil {
		return errors.New("ffmpeg: decoder not found")
	}

	// Allocate codec context
	d.videoCodecCtx = avcodec.AllocContext3(codec)
	if d.videoCodecCtx == nil {
		return errors.New("ffmpeg: failed to allocate codec context")
	}

	// Copy codec parameters
	if err := avcodec.ParametersToContext(d.videoCodecCtx, codecPar); err != nil {
		avcodec.FreeContext(&d.videoCodecCtx)
		return err
	}

	// Open codec
	if err := avcodec.Open2(d.videoCodecCtx, codec, nil); err != nil {
		avcodec.FreeContext(&d.videoCodecCtx)
		return err
	}

	d.videoDecoderOpen = true
	return nil
}

// OpenAudioDecoder opens a codec context for audio decoding.
func (d *Decoder) OpenAudioDecoder() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.openAudioDecoderLocked()
}

func (d *Decoder) openAudioDecoderLocked() error {
	if d.closed {
		return errDecoderClosed
	}
	if d.audioStreamIdx < 0 {
		return ErrNoAudioStream
	}

	if d.audioDecoderOpen {
		return nil // Already opened
	}

	stream := avformat.GetStream(d.formatCtx, d.audioStreamIdx)
	codecPar := avformat.GetStreamCodecPar(stream)
	codecID := avformat.GetCodecParCodecID(codecPar)

	// Find decoder
	codec := avcodec.FindDecoder(codecID)
	if codec == nil {
		return errors.New("ffmpeg: audio decoder not found")
	}

	// Allocate codec context
	d.audioCodecCtx = avcodec.AllocContext3(codec)
	if d.audioCodecCtx == nil {
		return errors.New("ffmpeg: failed to allocate audio codec context")
	}

	// Copy codec parameters
	if err := avcodec.ParametersToContext(d.audioCodecCtx, codecPar); err != nil {
		avcodec.FreeContext(&d.audioCodecCtx)
		return err
	}

	// Open codec
	if err := avcodec.Open2(d.audioCodecCtx, codec, nil); err != nil {
		avcodec.FreeContext(&d.audioCodecCtx)
		return err
	}

	d.audioDecoderOpen = true
	return nil
}

// DecodeVideoPacket decodes a video packet and returns the decoded frame.
// Returns an empty frame with nil error if more data is needed (EAGAIN), and
// returns io.EOF after the decoder is fully drained.
// The returned frame is owned by the decoder; copy it if you need to keep it.
func (d *Decoder) DecodeVideoPacket(pkt *Packet) (Frame, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.decodePacketLocked(MediaTypeVideo, pkt)
}

// DecodeVideoPacketCopy decodes a video packet and returns an owned frame.
//
// Unlike DecodeVideoPacket (which returns a decoder-owned, internally reused frame),
// this method returns a cloned frame that the caller MUST free with FrameFree.
// Returns an empty frame with nil error if more data is needed (EAGAIN), and
// returns io.EOF after the decoder is fully drained.
func (d *Decoder) DecodeVideoPacketCopy(pkt *Packet) (Frame, error) {
	frame, err := d.DecodeVideoPacket(pkt)
	if err != nil || frame.IsNil() {
		return Frame{}, err
	}
	return FrameClone(frame)
}

// DecodeAudioPacket decodes an audio packet and returns the decoded frame.
// Returns an empty frame with nil error if more data is needed (EAGAIN), and
// returns io.EOF after the decoder is fully drained.
// The returned frame is owned by the decoder; copy it if you need to keep it.
func (d *Decoder) DecodeAudioPacket(pkt *Packet) (Frame, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.decodePacketLocked(MediaTypeAudio, pkt)
}

func (d *Decoder) decodePacketLocked(mediaType MediaType, packet *Packet) (Frame, error) {
	if d.closed {
		return Frame{}, errDecoderClosed
	}

	state, ctx, err := d.codecStateLocked(mediaType)
	if err != nil {
		return Frame{}, err
	}
	if packet == nil || packet.ptr == nil {
		state.requestFlush()
	} else {
		clone, err := d.clonePacketLocked(packet.ptr)
		if err != nil {
			return Frame{}, err
		}
		if err := state.enqueueOwned(clone); err != nil {
			d.recyclePacketLocked(&clone)
			return Frame{}, err
		}
	}

	ready, err := state.next(ctx, d.frame)
	if err != nil {
		if avutil.IsEOF(err) {
			return Frame{}, io.EOF
		}
		return Frame{}, err
	}
	if !ready {
		if state.drained {
			return Frame{}, io.EOF
		}
		return Frame{}, nil
	}
	return Frame{ptr: d.frame, owned: false}, nil
}

// DecodeAudioPacketCopy decodes an audio packet and returns an owned frame.
//
// Unlike DecodeAudioPacket (which returns a decoder-owned, internally reused frame),
// this method returns a cloned frame that the caller MUST free with FrameFree.
// Returns an empty frame with nil error if more data is needed (EAGAIN), and
// returns io.EOF after the decoder is fully drained.
func (d *Decoder) DecodeAudioPacketCopy(pkt *Packet) (Frame, error) {
	frame, err := d.DecodeAudioPacket(pkt)
	if err != nil || frame.IsNil() {
		return Frame{}, err
	}
	return FrameClone(frame)
}

// DecodeVideo reads and decodes the next video frame.
// This is a convenience method that handles packet reading internally.
// Packets for other selected streams remain available to DecodeAudio or ReadFrame.
// The returned frame is owned by the decoder; do not call FrameFree on it.
// If you need to keep the frame beyond the next decode call, make a copy.
// Returns io.EOF after the decoder is fully drained.
func (d *Decoder) DecodeVideo() (Frame, error) {
	return d.DecodeVideoContext(context.Background())
}

// DecodeVideoContext decodes the next video frame with cancellation.
func (d *Decoder) DecodeVideoContext(ctx context.Context) (Frame, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.beginInterrupt(ctx); err != nil {
		return Frame{}, err
	}
	defer d.clearInterrupt()

	if !d.videoDecoderOpen {
		if err := d.openVideoDecoderLocked(); err != nil {
			return Frame{}, err
		}
	}
	frame, err := d.nextFrameLocked(MediaTypeVideo)
	return frame, d.finishInterrupt(err)
}

// DecodeVideoCopy reads and decodes the next video frame and returns an owned frame.
//
// The caller MUST free the returned frame with FrameFree.
// Returns io.EOF after the decoder is fully drained.
func (d *Decoder) DecodeVideoCopy() (Frame, error) {
	return d.DecodeVideoCopyContext(context.Background())
}

// DecodeVideoCopyContext decodes the next video frame as an owned frame with cancellation.
func (d *Decoder) DecodeVideoCopyContext(ctx context.Context) (Frame, error) {
	frame, err := d.DecodeVideoContext(ctx)
	if err != nil || frame.IsNil() {
		return Frame{}, err
	}
	return FrameClone(frame)
}

// DecodeAudio reads and decodes the next audio frame.
// This is a convenience method that handles packet reading internally.
// Packets for other selected streams remain available to DecodeVideo or ReadFrame.
// The returned frame is owned by the decoder; do not call FrameFree on it.
// If you need to keep the frame beyond the next decode call, make a copy.
// Returns nil frame on EOF.
func (d *Decoder) DecodeAudio() (Frame, error) {
	return d.DecodeAudioContext(context.Background())
}

// DecodeAudioContext decodes the next audio frame with cancellation.
func (d *Decoder) DecodeAudioContext(ctx context.Context) (Frame, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.beginInterrupt(ctx); err != nil {
		return Frame{}, err
	}
	defer d.clearInterrupt()

	if !d.audioDecoderOpen {
		if err := d.openAudioDecoderLocked(); err != nil {
			return Frame{}, err
		}
	}
	frame, err := d.nextFrameLocked(MediaTypeAudio)
	return frame, d.finishInterrupt(err)
}

// ReadFrame reads and decodes the next frame (video or audio).
// Returns a FrameWrapper with the MediaType set.
// The frame is owned by the decoder; call Copy() if you need to keep it.
// Returns io.EOF after all selected streams are fully drained.
func (d *Decoder) ReadFrame() (*FrameWrapper, error) {
	return d.ReadFrameContext(context.Background())
}

// ReadFrameContext reads and decodes the next frame with cancellation.
func (d *Decoder) ReadFrameContext(ctx context.Context) (*FrameWrapper, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, errDecoderClosed
	}
	if err := d.beginInterrupt(ctx); err != nil {
		return nil, err
	}
	defer d.clearInterrupt()
	// Open decoders if needed
	if d.HasVideo() && !d.videoDecoderOpen {
		if err := d.openVideoDecoderLocked(); err != nil {
			return nil, err
		}
	}
	if d.HasAudio() && !d.audioDecoderOpen {
		if err := d.openAudioDecoderLocked(); err != nil {
			return nil, err
		}
	}
	frame, err := d.readFrameLocked()
	return frame, d.finishInterrupt(err)
}

// ReadFrameCopy reads and decodes the next frame (video or audio) and returns an owned frame wrapper.
//
// The returned wrapper owns its underlying frame; the caller MUST call Free() when done.
// Returns io.EOF after all selected streams are fully drained.
func (d *Decoder) ReadFrameCopy() (*FrameWrapper, error) {
	return d.ReadFrameCopyContext(context.Background())
}

// ReadFrameCopyContext reads an owned frame wrapper with cancellation.
func (d *Decoder) ReadFrameCopyContext(ctx context.Context) (*FrameWrapper, error) {
	fw, err := d.ReadFrameContext(ctx)
	if err != nil || fw == nil {
		return nil, err
	}
	return fw.Copy()
}

// FlushDecoder discards buffered codec output and queued packets for selected streams.
func (d *Decoder) FlushDecoder() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.videoCodecCtx != nil {
		avcodec.FlushBuffers(d.videoCodecCtx)
	}
	if d.audioCodecCtx != nil {
		avcodec.FlushBuffers(d.audioCodecCtx)
	}
	d.clearDecodeStateLocked()
}

// Seek seeks to a position in the file.
// The timestamp is specified as time.Duration from the start.
func (d *Decoder) Seek(ts time.Duration) error {
	return d.SeekContext(context.Background(), ts)
}

// SeekContext seeks to a position and interrupts blocking FFmpeg I/O when ctx is canceled.
func (d *Decoder) SeekContext(ctx context.Context, ts time.Duration) error {
	return d.SeekTimestampContext(ctx, ts.Microseconds())
}

// SeekTimestamp seeks to a position in the file.
// timestamp is in AV_TIME_BASE (microseconds).
func (d *Decoder) SeekTimestamp(timestamp int64) error {
	return d.SeekTimestampContext(context.Background(), timestamp)
}

// SeekTimestampContext seeks to a timestamp with cancellation.
func (d *Decoder) SeekTimestampContext(ctx context.Context, timestamp int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return errDecoderClosed
	}
	if err := d.beginInterrupt(ctx); err != nil {
		return err
	}
	defer d.clearInterrupt()

	// Seek to keyframe before target
	if err := d.seekInputLocked(-1, timestamp, avformat.SeekFlagBackward); err != nil {
		return d.finishInterrupt(err)
	}

	// Flush decoder buffers
	if d.videoCodecCtx != nil {
		avcodec.FlushBuffers(d.videoCodecCtx)
	}
	if d.audioCodecCtx != nil {
		avcodec.FlushBuffers(d.audioCodecCtx)
	}
	d.clearDecodeStateLocked()

	return nil
}

// Close releases all resources.
func (d *Decoder) Close() error {
	d.closeSignal.Do(func() {
		if d.interrupt != nil {
			d.interrupt.stop()
		}
		if d.customIO != nil {
			d.customIO.cancelPending()
		}
	})
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}
	d.closed = true
	d.clearDecodeStateLocked()
	d.clearPacketPoolLocked()

	// Free frame
	if d.frame != nil {
		avutil.FrameFree(&d.frame)
	}

	// Free packet
	if d.packet != nil {
		avcodec.PacketFree(&d.packet)
	}

	// Free video codec context
	if d.videoCodecCtx != nil {
		avcodec.FreeContext(&d.videoCodecCtx)
	}

	// Free audio codec context
	if d.audioCodecCtx != nil {
		avcodec.FreeContext(&d.audioCodecCtx)
	}

	// Close input
	if d.interrupt != nil {
		d.interrupt.release(d.formatCtx)
	}
	if d.formatCtx != nil {
		avformat.CloseInput(&d.formatCtx)
	}

	// Cleanup any extra resources (e.g. custom I/O, temp files).
	if d.cleanup != nil {
		d.cleanup()
		d.cleanup = nil
	}
	if d.customIO != nil {
		_ = d.customIO.Close()
		d.customIO = nil
	}

	return nil
}
