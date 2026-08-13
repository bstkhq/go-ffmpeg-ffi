//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestPlanVideoLayoutSubsampledOddDimensions(t *testing.T) {
	tests := []struct {
		name        string
		format      PixelFormat
		wantOffsets []int
		wantLines   []int
		wantTotal   int
	}{
		{
			name:        "YUV420P",
			format:      PixelFormatYUV420P,
			wantOffsets: []int{0, 15, 21},
			wantLines:   []int{5, 3, 3},
			wantTotal:   27,
		},
		{
			name:        "NV12",
			format:      PixelFormatNV12,
			wantOffsets: []int{0, 15},
			wantLines:   []int{5, 6},
			wantTotal:   27,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offsets, lines, total, err := planVideoLayout(5, 3, tt.format)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(offsets, tt.wantOffsets) {
				t.Errorf("offsets = %v, want %v", offsets, tt.wantOffsets)
			}
			if !reflect.DeepEqual(lines, tt.wantLines) {
				t.Errorf("linesizes = %v, want %v", lines, tt.wantLines)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
		})
	}
}

func TestPlanVideoLayoutRejectsOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	if _, _, _, err := planVideoLayout(maxInt, 2, PixelFormatRGBA); err == nil {
		t.Fatal("planVideoLayout accepted dimensions that overflow int")
	}
}

func TestFramePool_GetPutAndLimit(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	p := NewFramePool(1)
	defer p.Close()

	f1, err := p.Get()
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if _, err := p.Get(); err == nil {
		t.Fatalf("expected pool exhausted error")
	}
	if err := p.Put(&f1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if !f1.IsNil() {
		t.Fatalf("expected Put to clear frame")
	}
	if _, err := p.Get(); err != nil {
		t.Fatalf("Get after Put failed: %v", err)
	}
}

func TestFramePoolRejectsForeignAndDuplicateFrames(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	pool := NewFramePool(2)
	defer pool.Close()
	other := NewFramePool(1)
	defer other.Close()

	foreign := FrameAlloc()
	defer func() { _ = FrameFree(&foreign) }()
	if err := pool.Put(&foreign); err == nil {
		t.Fatal("pool accepted a frame it did not lease")
	}

	leased, err := pool.Get()
	if err != nil {
		t.Fatal(err)
	}
	copyOfLease := leased
	if err := other.Put(&leased); err == nil {
		t.Fatal("another pool accepted the lease")
	}
	if err := FrameFree(&leased); err == nil {
		t.Fatal("FrameFree accepted a pooled frame")
	}
	if err := pool.Put(&leased); err != nil {
		t.Fatal(err)
	}
	if !copyOfLease.IsNil() {
		t.Fatal("copied lease remained usable after return")
	}
	if err := pool.Put(&copyOfLease); err == nil {
		t.Fatal("pool accepted the same lease twice")
	}
}

func TestFramePoolAccountsForReturnAfterClose(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	pool := NewFramePool(1)
	frame, err := pool.Get()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if err := pool.Put(&frame); err != nil {
		t.Fatal(err)
	}
	if pool.inUse != 0 {
		t.Fatalf("in-use frames = %d, want 0", pool.inUse)
	}
}

func TestFramePoolCopiedLeaseCannotMutateReusedFrame(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	pool := NewFramePool(1)
	defer pool.Close()
	leased, err := pool.Get()
	if err != nil {
		t.Fatal(err)
	}
	copyOfLease := leased
	if err := pool.Put(&leased); err != nil {
		t.Fatal(err)
	}
	reused, err := pool.Get()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Put(&reused) }()

	reused.SetWidth(123)
	FrameUnref(copyOfLease)
	copyOfLease.SetWidth(456)
	if got := copyOfLease.Width(); got != 0 {
		t.Fatalf("stale lease width = %d, want 0", got)
	}
	if got := reused.Width(); got != 123 {
		t.Fatalf("reused frame width = %d, want 123", got)
	}
	if err := copyOfLease.useError(); !errors.Is(err, ErrFrameLeaseReturned) {
		t.Fatalf("stale lease error = %v, want ErrFrameLeaseReturned", err)
	}
	if err := (&Encoder{}).WriteFrame(copyOfLease); !errors.Is(err, ErrFrameLeaseReturned) {
		t.Fatalf("encoder stale lease error = %v, want ErrFrameLeaseReturned", err)
	}
}

func TestFrameWrapBuffer_RGB24(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	SetWrappedBufferMemoryLimit(0)

	before := WrappedBufferMemoryUsage()

	var f Frame
	w, h := 8, 4
	buf := make([]byte, w*h*3)
	if err := f.WrapBuffer(buf, w, h, PixelFormatRGB24); err != nil {
		t.Fatalf("WrapBuffer failed: %v", err)
	}

	if got := int(avutil.GetFrameWidth(f.ptr)); got != w {
		t.Fatalf("width: got %d want %d", got, w)
	}
	if got := int(avutil.GetFrameHeight(f.ptr)); got != h {
		t.Fatalf("height: got %d want %d", got, h)
	}

	p0 := avutil.GetFrameDataPlane(f.ptr, 0)
	if p0 != unsafe.Pointer(&buf[0]) {
		t.Fatalf("data[0] pointer mismatch: got=%p want=%p", p0, unsafe.Pointer(&buf[0]))
	}
	ls0 := avutil.GetFrameLinesizePlane(f.ptr, 0)
	if int(ls0) != w*3 {
		t.Fatalf("linesize[0]: got %d want %d", ls0, w*3)
	}

	after := WrappedBufferMemoryUsage()
	if after.PinnedBuffers != before.PinnedBuffers+1 {
		t.Fatalf("pinned buffers: before=%d after=%d", before.PinnedBuffers, after.PinnedBuffers)
	}
	if after.PinnedBytes < before.PinnedBytes+int64(len(buf)) {
		t.Fatalf("pinned bytes did not increase as expected: before=%d after=%d", before.PinnedBytes, after.PinnedBytes)
	}

	if err := FrameFree(&f); err != nil {
		t.Fatalf("FrameFree failed: %v", err)
	}
	final := WrappedBufferMemoryUsage()
	if final.PinnedBuffers != before.PinnedBuffers || final.PinnedBytes != before.PinnedBytes {
		t.Fatalf("expected pinned usage to return to baseline: before=%v final=%v", before, final)
	}
}

func TestFrameWrapBufferSubsampledOddDimensions(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	for _, tt := range []struct {
		name      string
		format    PixelFormat
		wantLines []int32
	}{
		{name: "YUV420P", format: PixelFormatYUV420P, wantLines: []int32{5, 3, 3}},
		{name: "NV12", format: PixelFormatNV12, wantLines: []int32{5, 6}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var frame Frame
			if err := frame.WrapBuffer(make([]byte, 27), 5, 3, tt.format); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = FrameFree(&frame) }()

			for plane, want := range tt.wantLines {
				if got := avutil.GetFrameLinesizePlane(frame.ptr, plane); got != want {
					t.Errorf("linesize[%d] = %d, want %d", plane, got, want)
				}
			}
		})
	}
}

func TestFrameWrapBuffer_MemoryLimit(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	defer SetWrappedBufferMemoryLimit(0)

	SetWrappedBufferMemoryLimit(16)

	var f Frame
	t.Cleanup(func() { _ = FrameFree(&f) })
	buf := make([]byte, 64)
	if err := f.WrapBuffer(buf, 8, 4, PixelFormatRGB24); err == nil {
		t.Fatalf("expected WrapBuffer to fail due to memory limit")
	}
}

func TestFrameWrapBufferReplacementAtMemoryLimit(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	const (
		width      = 8
		height     = 4
		frameBytes = width * height * 3
	)
	baseline := WrappedBufferMemoryUsage()
	SetWrappedBufferMemoryLimit(baseline.PinnedBytes + frameBytes)
	t.Cleanup(func() { SetWrappedBufferMemoryLimit(0) })

	var frame Frame
	t.Cleanup(func() { _ = FrameFree(&frame) })
	for replacement := byte(0); replacement < 16; replacement++ {
		data := make([]byte, frameBytes)
		data[0] = replacement
		if err := frame.WrapBuffer(data, width, height, PixelFormatRGB24); err != nil {
			t.Fatalf("replacement %d failed: %v", replacement, err)
		}

		usage := WrappedBufferMemoryUsage()
		want := WrappedBufferUsage{
			PinnedBuffers: baseline.PinnedBuffers + 1,
			PinnedBytes:   baseline.PinnedBytes + frameBytes,
		}
		if usage != want {
			t.Fatalf("usage after replacement %d = %v, want %v", replacement, usage, want)
		}
		if got := avutil.GetFrameDataPlane(frame.ptr, 0); got != unsafe.Pointer(&data[0]) {
			t.Fatalf("data pointer after replacement %d = %p, want %p", replacement, got, unsafe.Pointer(&data[0]))
		}
	}

	if err := FrameFree(&frame); err != nil {
		t.Fatal(err)
	}
	if final := WrappedBufferMemoryUsage(); final != baseline {
		t.Fatalf("pinned usage after replacements = %v, want %v", final, baseline)
	}
}

func TestFrameWrapBufferConcurrentMemoryLimit(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	const (
		workers       = 32
		allowedFrames = 4
		width         = 8
		height        = 4
		frameBytes    = width * height * 3
	)
	baseline := WrappedBufferMemoryUsage()
	SetWrappedBufferMemoryLimit(baseline.PinnedBytes + allowedFrames*frameBytes)
	t.Cleanup(func() { SetWrappedBufferMemoryLimit(0) })

	start := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseAll)
	results := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			<-start
			var frame Frame
			err := frame.WrapBuffer(make([]byte, frameBytes), width, height, PixelFormatRGB24)
			results <- err
			if err == nil {
				<-release
				if err := FrameFree(&frame); err != nil {
					results <- err
				}
			}
		}()
	}
	close(start)

	succeeded := 0
	for range workers {
		if err := <-results; err == nil {
			succeeded++
		} else if !errors.Is(err, errWrappedBufferMemoryLimit) {
			t.Fatalf("WrapBuffer returned unexpected error: %v", err)
		}
	}
	if succeeded != allowedFrames {
		t.Fatalf("successful concurrent wraps = %d, want %d", succeeded, allowedFrames)
	}
	usage := WrappedBufferMemoryUsage()
	if usage.PinnedBytes != baseline.PinnedBytes+allowedFrames*frameBytes {
		t.Fatalf("pinned bytes under contention = %d, want %d", usage.PinnedBytes, baseline.PinnedBytes+allowedFrames*frameBytes)
	}
	if usage.PinnedBuffers != baseline.PinnedBuffers+allowedFrames {
		t.Fatalf("pinned buffers under contention = %d, want %d", usage.PinnedBuffers, baseline.PinnedBuffers+allowedFrames)
	}

	releaseAll()
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("FrameFree failed: %v", err)
		}
	}
	if final := WrappedBufferMemoryUsage(); final != baseline {
		t.Fatalf("pinned usage after concurrent release = %v, want %v", final, baseline)
	}
}
