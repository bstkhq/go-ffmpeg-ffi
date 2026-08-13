//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"fmt"
	"sync"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
)

// Remuxer copies streams from input to output without re-encoding.
// This is useful for changing container formats or extracting streams.
type Remuxer struct {
	mu sync.Mutex

	// Output context
	outputCtx avformat.FormatContext
	outputIO  avformat.IOContext

	// Stream mapping: inputStreamIdx -> outputStreamIdx
	streamMap map[int]int

	// Time base mapping for timestamp rescaling
	inputTimeBases  map[int]avutil.Rational
	outputTimeBases map[int]avutil.Rational

	// Reusable packet
	packet    avcodec.Packet
	packetRef func(dst, src avcodec.Packet) error

	headerWritten bool
	closed        bool
}

// RemuxerConfig configures a remuxer.
type RemuxerConfig struct {
	// InputStreams specifies which input stream indices to copy.
	// If empty, all streams are copied.
	InputStreams []int
}

// NewRemuxer creates a new remuxer that copies packets from decoder to output file.
// The decoder is used to get input stream information.
func NewRemuxer(outputPath string, decoder *Decoder, cfg *RemuxerConfig) (*Remuxer, error) {
	if decoder == nil {
		return nil, errors.New("ffmpeg: decoder is required for remuxing")
	}

	if err := bindings.Load(); err != nil {
		return nil, err
	}

	r := &Remuxer{
		streamMap:       make(map[int]int),
		inputTimeBases:  make(map[int]avutil.Rational),
		outputTimeBases: make(map[int]avutil.Rational),
	}

	// Determine output format from filename
	formatName := guessFormatFromPath(outputPath)
	if formatName == "" {
		return nil, errors.New("ffmpeg: cannot determine output format from filename")
	}

	// Create output format context
	if err := avformat.AllocOutputContext2(&r.outputCtx, nil, formatName, outputPath); err != nil {
		return nil, err
	}

	// Copy the requested slice so caller mutation cannot change stream selection
	// while the decoder snapshot is being built.
	var streamsToCopy []int
	if cfg != nil && len(cfg.InputStreams) > 0 {
		streamsToCopy = append([]int(nil), cfg.InputStreams...)
	}

	// Codec parameters are native pointers owned by the decoder. Copy them into
	// the output context while holding the decoder lock so Close cannot free the
	// input format context underneath this constructor.
	if err := r.copyDecoderStreams(decoder, streamsToCopy); err != nil {
		r.cleanup()
		return nil, err
	}

	// Open output file if needed
	if !avformat.HasNoFile(r.outputCtx) {
		if err := avformat.IOOpen(&r.outputIO, outputPath, avformat.IOFlagWrite); err != nil {
			r.cleanup()
			return nil, err
		}
		avformat.SetIOContext(r.outputCtx, r.outputIO)
	}

	// Allocate packet
	r.packet = avcodec.PacketAlloc()
	if r.packet == nil {
		r.cleanup()
		return nil, errors.New("ffmpeg: failed to allocate packet")
	}

	return r, nil
}

func (r *Remuxer) copyDecoderStreams(decoder *Decoder, requested []int) error {
	decoder.mu.Lock()
	defer decoder.mu.Unlock()

	if decoder.closed || decoder.formatCtx == nil {
		return errDecoderClosed
	}

	streamsToCopy := requested
	if len(streamsToCopy) == 0 {
		numStreams := avformat.GetNbStreams(decoder.formatCtx)
		streamsToCopy = make([]int, numStreams)
		for i := range streamsToCopy {
			streamsToCopy[i] = i
		}
	}

	seen := make(map[int]struct{}, len(streamsToCopy))
	for _, inputIdx := range streamsToCopy {
		if _, duplicate := seen[inputIdx]; duplicate {
			return fmt.Errorf("ffmpeg: duplicate input stream index %d", inputIdx)
		}
		seen[inputIdx] = struct{}{}

		inputStream := avformat.GetStream(decoder.formatCtx, inputIdx)
		if inputStream == nil {
			return fmt.Errorf("ffmpeg: invalid input stream index %d", inputIdx)
		}

		outputStream := avformat.NewStream(r.outputCtx, nil)
		if outputStream == nil {
			return errors.New("ffmpeg: failed to create output stream")
		}

		inputCodecPar := avformat.GetStreamCodecPar(inputStream)
		outputCodecPar := avformat.GetStreamCodecPar(outputStream)
		if inputCodecPar == nil || outputCodecPar == nil {
			return errors.New("ffmpeg: stream has no codec parameters")
		}
		if err := avcodec.ParametersCopy(outputCodecPar, inputCodecPar); err != nil {
			return err
		}

		avcodec.SetCodecParTag(outputCodecPar, 0)
		outputIdx := int(avformat.GetStreamIndex(outputStream))
		if outputIdx < 0 {
			return errors.New("ffmpeg: output stream has no valid index")
		}
		r.streamMap[inputIdx] = outputIdx

		inTBNum, inTBDen := avformat.GetStreamTimeBase(inputStream)
		r.inputTimeBases[inputIdx] = avutil.NewRational(inTBNum, inTBDen)
		outTBNum, outTBDen := avformat.GetStreamTimeBase(outputStream)
		r.outputTimeBases[inputIdx] = avutil.NewRational(outTBNum, outTBDen)
	}
	return nil
}

// WriteHeader writes the output file header.
// Must be called before WritePacket.
func (r *Remuxer) WriteHeader() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return closedError("remuxer")
	}
	if r.headerWritten {
		return nil
	}

	if err := avformat.WriteHeader(r.outputCtx, nil); err != nil {
		return err
	}
	r.headerWritten = true

	// Update output time bases after header is written
	// (some formats may change time bases during header write)
	for inputIdx := range r.streamMap {
		outputIdx := r.streamMap[inputIdx]
		outputStream := avformat.GetStream(r.outputCtx, outputIdx)
		if outputStream != nil {
			tbNum, tbDen := avformat.GetStreamTimeBase(outputStream)
			r.outputTimeBases[inputIdx] = avutil.NewRational(tbNum, tbDen)
		}
	}

	return nil
}

// WritePacket copies a packet to the output.
// The packet's stream index is remapped to the output stream.
// Timestamps are rescaled from input to output time base.
func (r *Remuxer) WritePacket(pkt avcodec.Packet, inputStreamIdx int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return closedError("remuxer")
	}
	if pkt == nil {
		return errors.New("ffmpeg: remux packet is nil")
	}

	// Check if this stream is being copied
	outputIdx, ok := r.streamMap[inputStreamIdx]
	if !ok {
		// Stream not being copied, skip
		return nil
	}

	// Auto-write header if needed
	if !r.headerWritten {
		if err := avformat.WriteHeader(r.outputCtx, nil); err != nil {
			return err
		}
		r.headerWritten = true

		// Update output time bases
		for inIdx := range r.streamMap {
			outIdx := r.streamMap[inIdx]
			outputStream := avformat.GetStream(r.outputCtx, outIdx)
			if outputStream != nil {
				tbNum, tbDen := avformat.GetStreamTimeBase(outputStream)
				r.outputTimeBases[inIdx] = avutil.NewRational(tbNum, tbDen)
			}
		}
	}

	// Reference the packet (don't copy data, just increment refcount). Keep the
	// reusable packet empty on every exit, including allocation failures.
	avcodec.PacketUnref(r.packet)
	if err := r.refPacket(r.packet, pkt); err != nil {
		return err
	}
	defer avcodec.PacketUnref(r.packet)

	// Set output stream index
	avcodec.SetPacketStreamIndex(r.packet, int32(outputIdx))

	// Rescale timestamps from input to output time base
	inputTB := r.inputTimeBases[inputStreamIdx]
	outputTB := r.outputTimeBases[inputStreamIdx]
	avcodec.RescalePacketTS(r.packet, inputTB, outputTB)

	// Write the packet
	return avformat.InterleavedWriteFrame(r.outputCtx, r.packet)
}

func (r *Remuxer) refPacket(dst, src avcodec.Packet) error {
	if r.packetRef != nil {
		return r.packetRef(dst, src)
	}
	return avcodec.PacketRef(dst, src)
}

// Remux copies all packets from a decoder to the output.
// This is a convenience method that reads all packets and writes them.
func (r *Remuxer) Remux(decoder *Decoder) error {
	if err := r.WriteHeader(); err != nil {
		return err
	}

	for {
		pkt, err := decoder.ReadPacket()
		if err != nil {
			return err
		}
		if pkt == nil {
			break
		}

		streamIdx := pkt.StreamIndex()
		if err := r.WritePacket(pkt.ptr, streamIdx); err != nil {
			return err
		}
	}

	return nil
}

// Close finalizes and closes the remuxer.
func (r *Remuxer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true

	var firstErr error

	// Write trailer
	if r.outputCtx != nil && r.headerWritten {
		if err := avformat.WriteTrailer(r.outputCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	r.cleanup()
	return firstErr
}

func (r *Remuxer) cleanup() {
	if r.packet != nil {
		avcodec.PacketFree(&r.packet)
	}
	if r.outputIO != nil && r.outputCtx != nil {
		_ = avformat.IOCloseP(&r.outputIO)
	}
	if r.outputCtx != nil {
		avformat.FreeContext(r.outputCtx)
		r.outputCtx = nil
	}
}

// StreamMapping returns the mapping from input stream indices to output stream indices.
func (r *Remuxer) StreamMapping() map[int]int {
	r.mu.Lock()
	defer r.mu.Unlock()

	mapping := make(map[int]int, len(r.streamMap))
	for inputIdx, outputIdx := range r.streamMap {
		mapping[inputIdx] = outputIdx
	}
	return mapping
}

// NumOutputStreams returns the number of output streams.
func (r *Remuxer) NumOutputStreams() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.streamMap)
}
