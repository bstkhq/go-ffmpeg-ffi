//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
	"github.com/ebitengine/purego"
)

// BitstreamFilter represents an FFmpeg bitstream filter.
// Bitstream filters modify packet data without decoding.
type BitstreamFilter struct {
	mu      sync.Mutex
	ctx     bsfContext
	packet  avcodec.Packet
	state   bitstreamFilterState
	pending []avcodec.Packet
	closed  bool
}

// bsfContext is an opaque FFmpeg AVBSFContext pointer.
type bsfContext = unsafe.Pointer

// BSF function bindings
var (
	avBsfGetByName     func(name string) unsafe.Pointer
	avBsfAllocContext  func(filter uintptr, ctx *unsafe.Pointer) int32
	avBsfInit          func(ctx uintptr) int32
	avBsfFree          func(ctx *unsafe.Pointer)
	avBsfSendPacket    func(ctx, pkt uintptr) int32
	avBsfReceivePacket func(ctx, pkt uintptr) int32

	bsfBindingsOnce sync.Once
	bsfBindingsErr  error

	offsetBsfParIn       uintptr
	offsetBsfParOut      uintptr
	offsetBsfTimeBaseIn  uintptr
	offsetBsfTimeBaseOut uintptr
)

func registerBSFBindings() error {
	bsfBindingsOnce.Do(func() {
		bsfBindingsErr = loadBSFBindings()
	})
	return bsfBindingsErr
}

func loadBSFBindings() (err error) {
	if err := bindings.Load(); err != nil {
		return err
	}

	lib := bindings.LibAVCodec()
	if lib == 0 {
		return bindings.ErrNotLoaded
	}

	// Register into locals first so a missing symbol cannot publish a partial
	// binding set to another goroutine. RegisterLibFunc reports lookup failures
	// by panicking, so translate that into the package's normal error path.
	var (
		getByName     func(name string) unsafe.Pointer
		allocContext  func(filter uintptr, ctx *unsafe.Pointer) int32
		initContext   func(ctx uintptr) int32
		freeContext   func(ctx *unsafe.Pointer)
		sendPacket    func(ctx, pkt uintptr) int32
		receivePacket func(ctx, pkt uintptr) int32
	)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("ffgo: register bitstream filter bindings: %v", recovered)
		}
	}()

	purego.RegisterLibFunc(&getByName, lib, "av_bsf_get_by_name")
	purego.RegisterLibFunc(&allocContext, lib, "av_bsf_alloc")
	purego.RegisterLibFunc(&initContext, lib, "av_bsf_init")
	purego.RegisterLibFunc(&freeContext, lib, "av_bsf_free")
	purego.RegisterLibFunc(&sendPacket, lib, "av_bsf_send_packet")
	purego.RegisterLibFunc(&receivePacket, lib, "av_bsf_receive_packet")

	layout := bindings.ABI().BSFContext
	avBsfGetByName = getByName
	avBsfAllocContext = allocContext
	avBsfInit = initContext
	avBsfFree = freeContext
	avBsfSendPacket = sendPacket
	avBsfReceivePacket = receivePacket
	offsetBsfParIn = layout.ParametersIn
	offsetBsfParOut = layout.ParametersOut
	offsetBsfTimeBaseIn = layout.TimeBaseIn
	offsetBsfTimeBaseOut = layout.TimeBaseOut
	return nil
}

// Common bitstream filter names
const (
	// BSFNameH264Mp4ToAnnexB converts H.264 from MP4 format to Annex B format.
	// Useful when remuxing MP4 to transport streams or raw H.264.
	BSFNameH264Mp4ToAnnexB = "h264_mp4toannexb"

	// BSFNameHEVCMp4ToAnnexB converts HEVC from MP4 format to Annex B format.
	BSFNameHEVCMp4ToAnnexB = "hevc_mp4toannexb"

	// BSFNameAACADTSToASC converts AAC from ADTS header format to AudioSpecificConfig.
	// Useful when remuxing to MP4 containers.
	BSFNameAACADTSToASC = "aac_adtstoasc"

	// BSFNameExtractExtradata extracts codec-specific extradata from packets.
	BSFNameExtractExtradata = "extract_extradata"

	// BSFNameDumpExtradata dumps extradata to packets (opposite of extract).
	BSFNameDumpExtradata = "dump_extra"

	// BSFNameRemoveExtradata removes extradata from packets.
	BSFNameRemoveExtradata = "remove_extra"

	// BSFNameNull is a passthrough filter (for testing).
	BSFNameNull = "null"
)

// NewBitstreamFilter creates a new bitstream filter.
// filterName is the name of the filter (e.g., BSFNameH264Mp4ToAnnexB).
func NewBitstreamFilter(filterName string) (*BitstreamFilter, error) {
	if err := registerBSFBindings(); err != nil {
		return nil, err
	}

	// Find the filter
	filter := avBsfGetByName(filterName)
	if filter == nil {
		return nil, errors.New("ffgo: bitstream filter not found: " + filterName)
	}

	// Allocate context
	var ctx bsfContext
	ret := avBsfAllocContext(uintptr(filter), &ctx)
	if ret < 0 {
		return nil, avutil.NewError(ret, "av_bsf_alloc")
	}

	// Allocate packet
	packet := avcodec.PacketAlloc()
	if packet == nil {
		avBsfFree(&ctx)
		return nil, errors.New("ffgo: failed to allocate packet")
	}

	return &BitstreamFilter{
		ctx:    ctx,
		packet: packet,
	}, nil
}

// SetInputCodecParameters sets the input codec parameters.
// This must be called before Init() and should match the codec of packets being filtered.
func (f *BitstreamFilter) SetInputCodecParameters(par avcodec.Parameters) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || f.ctx == nil {
		return closedError("filter")
	}

	// Get par_in pointer and copy parameters
	parIn := *(*unsafe.Pointer)(unsafe.Pointer(uintptr(f.ctx) + offsetBsfParIn))
	if parIn == nil {
		return errors.New("ffgo: filter has no par_in")
	}

	return avcodec.ParametersCopy(parIn, par)
}

// SetInputTimeBase sets the input time base.
func (f *BitstreamFilter) SetInputTimeBase(num, den int32) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || f.ctx == nil {
		return
	}

	*(*avutil.Rational)(unsafe.Pointer(uintptr(f.ctx) + offsetBsfTimeBaseIn)) = avutil.NewRational(num, den)
}

// Init initializes the filter after configuration.
// Must be called after SetInputCodecParameters.
func (f *BitstreamFilter) Init() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || f.ctx == nil {
		return closedError("filter")
	}

	if avBsfInit == nil {
		return bindings.ErrNotLoaded
	}

	ret := avBsfInit(uintptr(f.ctx))
	if ret < 0 {
		return avutil.NewError(ret, "av_bsf_init")
	}
	return nil
}

// Filter sends a packet through the filter and returns the next filtered packet.
// The input packet's data is consumed once accepted by FFmpeg. The returned
// packet is borrowed and remains valid until the next filter operation. Returns
// nil, nil if more input is needed.
func (f *BitstreamFilter) Filter(pkt avcodec.Packet) (avcodec.Packet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || f.ctx == nil {
		return nil, closedError("filter")
	}
	if pkt == nil {
		return nil, errors.New("ffgo: bitstream filter input packet is nil; use Flush")
	}

	avcodec.PacketUnref(f.packet)
	if err := f.state.filter(f.ctx, pkt, f.packet, f.enqueueOutputLocked); err != nil {
		return nil, err
	}
	return f.nextOutputLocked()
}

// Flush returns remaining packets from the filter. Call it repeatedly after all
// input has been sent until it returns nil, nil.
func (f *BitstreamFilter) Flush() (avcodec.Packet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || f.ctx == nil {
		return nil, closedError("filter")
	}

	avcodec.PacketUnref(f.packet)
	if err := f.state.flush(f.ctx, f.packet, f.enqueueOutputLocked); err != nil {
		return nil, err
	}
	return f.nextOutputLocked()
}

func (f *BitstreamFilter) enqueueOutputLocked(packet avcodec.Packet) error {
	clone := avcodec.PacketAlloc()
	if clone == nil {
		return errors.New("ffgo: failed to allocate bitstream filter output packet")
	}
	if err := avcodec.PacketRef(clone, packet); err != nil {
		avcodec.PacketFree(&clone)
		return err
	}
	f.pending = append(f.pending, clone)
	return nil
}

func (f *BitstreamFilter) nextOutputLocked() (avcodec.Packet, error) {
	if len(f.pending) == 0 {
		return nil, nil
	}

	next := f.pending[0]
	if err := avcodec.PacketRef(f.packet, next); err != nil {
		return nil, err
	}
	avcodec.PacketFree(&next)
	f.pending[0] = nil
	f.pending = f.pending[1:]
	if len(f.pending) == 0 {
		f.pending = nil
	}
	return f.packet, nil
}

// GetOutputCodecParameters gets the output codec parameters after Init().
func (f *BitstreamFilter) GetOutputCodecParameters() avcodec.Parameters {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || f.ctx == nil {
		return nil
	}

	return *(*unsafe.Pointer)(unsafe.Pointer(uintptr(f.ctx) + offsetBsfParOut))
}

// GetOutputTimeBase gets the output time base after Init().
func (f *BitstreamFilter) GetOutputTimeBase() (num, den int32) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed || f.ctx == nil {
		return 0, 1
	}

	timeBase := *(*avutil.Rational)(unsafe.Pointer(uintptr(f.ctx) + offsetBsfTimeBaseOut))
	return timeBase.Num, timeBase.Den
}

// Close releases all filter resources.
func (f *BitstreamFilter) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil
	}
	f.closed = true

	if f.packet != nil {
		avcodec.PacketFree(&f.packet)
	}
	for i := range f.pending {
		avcodec.PacketFree(&f.pending[i])
	}
	f.pending = nil
	if f.ctx != nil && avBsfFree != nil {
		avBsfFree(&f.ctx)
	}

	return nil
}

// BitstreamFilterExists checks if a bitstream filter with the given name exists.
func BitstreamFilterExists(name string) bool {
	if err := registerBSFBindings(); err != nil {
		return false
	}

	return avBsfGetByName(name) != nil
}

// ListBitstreamFilters returns a list of common bitstream filter names.
func ListBitstreamFilters() []string {
	return []string{
		BSFNameH264Mp4ToAnnexB,
		BSFNameHEVCMp4ToAnnexB,
		BSFNameAACADTSToASC,
		BSFNameExtractExtradata,
		BSFNameDumpExtradata,
		BSFNameRemoveExtradata,
		BSFNameNull,
	}
}
