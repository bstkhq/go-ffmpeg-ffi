//go:build !ios && (amd64 || arm64)

package ffgo

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/handles"
	"github.com/ebitengine/purego"
)

// FramePool reuses AVFrame allocations to reduce GC/FFmpeg allocation churn.
//
// Frames returned from Get() are OWNED by the caller and must be returned via Put().
type FramePool struct {
	mu       sync.Mutex
	idle     []avutil.Frame
	closed   bool
	inUse    int
	maxInUse int
}

type framePoolLease struct {
	pool     *FramePool
	returned atomic.Bool
}

// NewFramePool creates a new pool. If maxInUse <= 0, the pool is unbounded.
func NewFramePool(maxInUse int) *FramePool {
	return &FramePool{maxInUse: maxInUse}
}

// Get returns an owned frame from the pool.
func (p *FramePool) Get() (Frame, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return Frame{}, errors.New("ffgo: frame pool is closed")
	}
	if p.maxInUse > 0 && p.inUse >= p.maxInUse {
		return Frame{}, errors.New("ffgo: frame pool exhausted")
	}

	var fr avutil.Frame
	n := len(p.idle)
	if n > 0 {
		fr = p.idle[n-1]
		p.idle = p.idle[:n-1]
	} else {
		fr = avutil.FrameAlloc()
		if fr == nil {
			return Frame{}, ErrOutOfMemory
		}
	}

	avutil.FrameUnref(fr)
	p.inUse++
	return Frame{
		ptr:       fr,
		owned:     true,
		poolLease: &framePoolLease{pool: p},
	}, nil
}

// Put returns an owned frame to the pool and clears the caller's reference.
func (p *FramePool) Put(f *Frame) error {
	if p == nil {
		return nil
	}
	if f == nil || f.ptr == nil {
		return nil
	}
	if !f.owned {
		return errors.New("ffgo: cannot put borrowed frame into pool")
	}
	if f.poolLease == nil || f.poolLease.pool != p {
		return errors.New("ffgo: frame was not leased by this pool")
	}
	if !f.poolLease.returned.CompareAndSwap(false, true) {
		return ErrFrameLeaseReturned
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inUse <= 0 {
		return errors.New("ffgo: frame pool lease accounting underflow")
	}
	p.inUse--

	if p.closed {
		// Pool is closed: free the frame.
		avutil.FrameFree(&f.ptr)
		f.ptr = nil
		f.owned = false
		f.poolLease = nil
		return nil
	}

	avutil.FrameUnref(f.ptr)
	p.idle = append(p.idle, f.ptr)

	f.ptr = nil
	f.owned = false
	f.poolLease = nil
	return nil
}

// Close releases all idle frames in the pool. Frames still in use are not affected.
func (p *FramePool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true
	for i := range p.idle {
		fr := p.idle[i]
		if fr != nil {
			avutil.FrameFree(&fr)
		}
	}
	p.idle = nil
	return nil
}

// WrappedBufferUsage reports memory currently pinned by Frame.WrapBuffer.
type WrappedBufferUsage struct {
	PinnedBuffers int
	PinnedBytes   int64
}

var (
	wrapOnce        sync.Once
	wrapFreeCBPtr   uintptr
	wrapLimitBytes  atomic.Int64
	wrapPinnedBytes atomic.Int64
	wrapPinnedCount atomic.Int64
)

var errWrappedBufferMemoryLimit = errors.New("ffgo: WrapBuffer exceeds configured memory limit")

// SetWrappedBufferMemoryLimit sets a best-effort limit for total bytes pinned by Frame.WrapBuffer.
// A limit <= 0 disables enforcement.
func SetWrappedBufferMemoryLimit(bytes int64) {
	wrapLimitBytes.Store(bytes)
}

// WrappedBufferMemoryUsage returns the current pinned buffer count/bytes.
func WrappedBufferMemoryUsage() WrappedBufferUsage {
	return WrappedBufferUsage{
		PinnedBuffers: int(wrapPinnedCount.Load()),
		PinnedBytes:   wrapPinnedBytes.Load(),
	}
}

func reserveWrappedBufferBytes(size int64) bool {
	if size <= 0 {
		return false
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	for {
		current := wrapPinnedBytes.Load()
		if current < 0 || current > maxInt64-size {
			return false
		}
		next := current + size
		if limit := wrapLimitBytes.Load(); limit > 0 && next > limit {
			return false
		}
		if wrapPinnedBytes.CompareAndSwap(current, next) {
			return true
		}
	}
}

func initWrapCallback() {
	wrapOnce.Do(func() {
		wrapFreeCBPtr = purego.NewCallback(wrappedBufferFreeCallback)
	})
}

func wrappedBufferFreeCallback(_ purego.CDecl, opaque uintptr, _ *byte) {
	// A panic must never unwind through FFmpeg's native stack.
	defer func() { _ = recover() }()

	v := handles.Take(opaque)
	ent, ok := v.(wrappedBufferHold)
	if !ok {
		return
	}
	wrapPinnedBytes.Add(-ent.size)
	wrapPinnedCount.Add(-1)
	ent.pinner.Unpin()
	runtime.KeepAlive(ent.data)
}

type wrappedBufferHold struct {
	data   []byte
	size   int64
	pinner *runtime.Pinner
}

// WrapBuffer wraps an existing buffer as a video frame without copying.
//
// The buffer is reference-counted by FFmpeg via AVBufferRef. Its backing array
// is pinned until FFmpeg releases the final reference. The caller must not
// resize, reuse, or mutate data concurrently with native access and must release
// the frame with Frame.Free or FrameFree.
// WrapBuffer may run concurrently for independent frames. Calls that target the
// same Frame must be synchronized by the caller.
//
// Supported formats:
// - PixelFormatRGB24
// - PixelFormatRGBA / PixelFormatBGRA
// - PixelFormatYUV420P
// - PixelFormatNV12
func (f *Frame) WrapBuffer(data []byte, width, height int, format PixelFormat) error {
	if f == nil {
		return errors.New("ffgo: frame is nil")
	}
	if err := f.poolLeaseError(); err != nil {
		return err
	}
	if f.ptr != nil && !f.owned {
		return errors.New("ffgo: cannot WrapBuffer into a borrowed frame")
	}
	if width <= 0 || height <= 0 {
		return errors.New("ffgo: invalid dimensions")
	}
	if len(data) == 0 {
		return errors.New("ffgo: data cannot be empty")
	}

	if f.ptr == nil {
		f.ptr = avutil.FrameAlloc()
		if f.ptr == nil {
			return ErrOutOfMemory
		}
		f.owned = true
	}

	planes, linesizes, need, err := planVideoLayout(width, height, format)
	if err != nil {
		return err
	}
	if len(data) < need {
		return errors.New("ffgo: buffer is too small for frame layout")
	}

	initWrapCallback()

	if !reserveWrappedBufferBytes(int64(need)) {
		return errWrappedBufferMemoryLimit
	}

	// Clear existing refs/buffers.
	avutil.FrameUnref(f.ptr)

	// Keep the backing []byte alive until FFmpeg releases the AVBufferRef.
	pinner := new(runtime.Pinner)
	pinner.Pin(&data[0])
	h := handles.Register(wrappedBufferHold{data: data[:need], size: int64(need), pinner: pinner})
	wrapPinnedCount.Add(1)

	bufRef := avutil.BufferCreate(unsafe.Pointer(&data[0]), need, wrapFreeCBPtr, h, 0)
	if bufRef == nil {
		if hold, ok := handles.Take(h).(wrappedBufferHold); ok {
			hold.pinner.Unpin()
		}
		wrapPinnedBytes.Add(-int64(need))
		wrapPinnedCount.Add(-1)
		return errors.New("ffgo: av_buffer_create failed")
	}

	// Fill frame fields.
	avutil.SetFrameWidth(f.ptr, int32(width))
	avutil.SetFrameHeight(f.ptr, int32(height))
	avutil.SetFrameFormat(f.ptr, int32(format))

	// Set data pointers/linesizes and buffer bookkeeping using the selected ABI.
	avutil.ConfigureFrameBuffer(f.ptr, bufRef)
	for i, off := range planes {
		avutil.SetFrameDataPlane(
			f.ptr,
			i,
			unsafe.Pointer(uintptr(unsafe.Pointer(&data[0]))+uintptr(off)),
			int32(linesizes[i]),
		)
	}

	return nil
}

func planVideoLayout(w, h int, fmt PixelFormat) (planeOffsets []int, linesizes []int, total int, err error) {
	if w <= 0 || h <= 0 {
		return nil, nil, 0, errors.New("ffgo: invalid frame dimensions")
	}

	type plane struct{ linesize, rows int }
	var planes []plane
	switch fmt {
	case PixelFormatRGB24:
		ls, ok := checkedLayoutMul(w, 3)
		if !ok {
			return nil, nil, 0, errors.New("ffgo: frame layout size overflows int")
		}
		planes = []plane{{ls, h}}
	case PixelFormatRGBA, PixelFormatBGRA:
		ls, ok := checkedLayoutMul(w, 4)
		if !ok {
			return nil, nil, 0, errors.New("ffgo: frame layout size overflows int")
		}
		planes = []plane{{ls, h}}
	case PixelFormatNV12:
		chromaWidth := w/2 + w%2
		chromaHeight := h/2 + h%2
		uvLinesize, ok := checkedLayoutMul(chromaWidth, 2)
		if !ok {
			return nil, nil, 0, errors.New("ffgo: frame layout size overflows int")
		}
		planes = []plane{{w, h}, {uvLinesize, chromaHeight}}
	case PixelFormatYUV420P:
		chromaWidth := w/2 + w%2
		chromaHeight := h/2 + h%2
		planes = []plane{{w, h}, {chromaWidth, chromaHeight}, {chromaWidth, chromaHeight}}
	default:
		return nil, nil, 0, errors.New("ffgo: unsupported pixel format for WrapBuffer")
	}

	for _, p := range planes {
		planeSize, ok := checkedLayoutMul(p.linesize, p.rows)
		if !ok {
			return nil, nil, 0, errors.New("ffgo: frame layout size overflows int")
		}
		planeOffsets = append(planeOffsets, total)
		linesizes = append(linesizes, p.linesize)
		total, ok = checkedLayoutAdd(total, planeSize)
		if !ok {
			return nil, nil, 0, errors.New("ffgo: frame layout size overflows int")
		}
	}
	return planeOffsets, linesizes, total, nil
}

func checkedLayoutMul(a, b int) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	if a < 0 || b < 0 || (a != 0 && b > maxInt/a) {
		return 0, false
	}
	return a * b, true
}

func checkedLayoutAdd(a, b int) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	if a < 0 || b < 0 || a > maxInt-b {
		return 0, false
	}
	return a + b, true
}
