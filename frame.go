//go:build amd64 || arm64

package ffgo

import (
	"math"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

// FrameWrapper provides a high-level interface to an FFmpeg AVFrame.
// It wraps the low-level Frame (unsafe.Pointer) with convenient methods.
type FrameWrapper struct {
	frame     Frame
	mediaType MediaType
}

// WrapFrame creates a FrameWrapper from a raw Frame.
func WrapFrame(frame Frame, mediaType MediaType) *FrameWrapper {
	if frame.IsNil() {
		return nil
	}
	return &FrameWrapper{
		frame:     frame,
		mediaType: mediaType,
	}
}

// Raw returns the underlying raw FFmpeg frame.
func (f *FrameWrapper) Raw() Frame {
	return f.frame
}

// PTS returns the presentation timestamp of the frame.
func (f *FrameWrapper) PTS() int64 {
	if f == nil || f.frame.IsNil() {
		return avutil.NoPTSValue
	}
	return avutil.GetFramePTS(f.frame.ptr)
}

// MediaType returns the type of media (video or audio).
func (f *FrameWrapper) MediaType() MediaType {
	if f == nil {
		return MediaTypeUnknown
	}
	return f.mediaType
}

// Width returns the frame width (video only).
func (f *FrameWrapper) Width() int {
	if f == nil || f.frame.IsNil() {
		return 0
	}
	return int(avutil.GetFrameWidth(f.frame.ptr))
}

// Height returns the frame height (video only).
func (f *FrameWrapper) Height() int {
	if f == nil || f.frame.IsNil() {
		return 0
	}
	return int(avutil.GetFrameHeight(f.frame.ptr))
}

// Format returns the pixel format (video) or sample format (audio).
func (f *FrameWrapper) Format() int32 {
	if f == nil || f.frame.IsNil() {
		return -1
	}
	return avutil.GetFrameFormat(f.frame.ptr)
}

// PixelFormat returns the pixel format for video frames.
func (f *FrameWrapper) PixelFormat() PixelFormat {
	return PixelFormat(f.Format())
}

// SampleFormat returns the sample format for audio frames.
func (f *FrameWrapper) SampleFormat() SampleFormat {
	return SampleFormat(f.Format())
}

// Data returns a borrowed slice to the frame data for the specified plane.
// For video: plane 0 = Y, plane 1 = U/Cb, plane 2 = V/Cr for YUV formats.
// For audio: plane 0 contains interleaved samples (packed) or plane N for channel N (planar).
// The slice is invalidated with the underlying frame, normally by the next
// decoder operation or by Free. Copy it if it must outlive the frame.
// Returns nil if the plane is not valid or not CPU-addressable.
func (f *FrameWrapper) Data(plane int) []byte {
	if f == nil || f.frame.IsNil() || plane < 0 {
		return nil
	}

	var data unsafe.Pointer
	var size uintptr
	switch f.mediaType {
	case MediaTypeVideo:
		if plane >= 4 {
			return nil
		}
		data = avutil.GetFrameDataPlane(f.frame.ptr, plane)
		if data == nil {
			return nil
		}
		linesizes := avutil.GetFrameLinesize(f.frame.ptr)
		planeSizes, err := avutil.ImagePlaneSizes(
			f.PixelFormat(),
			f.Height(),
			[4]int32{linesizes[0], linesizes[1], linesizes[2], linesizes[3]},
		)
		if err != nil {
			return nil
		}
		size = planeSizes[plane]
		if linesizes[plane] < 0 && size > 0 {
			stride := uintptr(-int64(linesizes[plane]))
			rows := size / stride
			if rows > 0 {
				data = unsafe.Add(data, -int(stride*(rows-1)))
			}
		}
	case MediaTypeAudio:
		format := f.SampleFormat()
		channels := int(avutil.GetFrameChannels(f.frame.ptr))
		if channels <= 0 {
			return nil
		}
		if avutil.SampleFormatIsPlanar(format) {
			if plane >= channels {
				return nil
			}
		} else if plane != 0 {
			return nil
		}
		bytesPerSample := avutil.BytesPerSample(format)
		samples := f.NumSamples()
		if bytesPerSample <= 0 || samples <= 0 {
			return nil
		}
		size64 := uint64(bytesPerSample) * uint64(samples)
		if !avutil.SampleFormatIsPlanar(format) {
			if size64 > uint64(math.MaxInt)/uint64(channels) {
				return nil
			}
			size64 *= uint64(channels)
		}
		if size64 > uint64(math.MaxInt) {
			return nil
		}
		size = uintptr(size64)
		data = avutil.GetFrameExtendedDataPlane(f.frame.ptr, plane)
	default:
		return nil
	}

	if data == nil || size == 0 || size > uintptr(math.MaxInt) {
		return nil
	}
	return unsafe.Slice((*byte)(data), int(size))
}

// Linesize returns the line size (stride) for the specified plane.
func (f *FrameWrapper) Linesize(plane int) int {
	if f == nil || f.frame.IsNil() || plane < 0 {
		return 0
	}
	linesize := avutil.GetFrameLinesize(f.frame.ptr)
	if f.mediaType == MediaTypeAudio {
		format := f.SampleFormat()
		channels := int(avutil.GetFrameChannels(f.frame.ptr))
		if channels <= 0 || (!avutil.SampleFormatIsPlanar(format) && plane != 0) || plane >= channels {
			return 0
		}
		return int(linesize[0])
	}
	if plane >= len(linesize) {
		return 0
	}
	return int(linesize[plane])
}

// NumSamples returns the number of audio samples in this frame (audio only).
func (f *FrameWrapper) NumSamples() int {
	if f == nil || f.frame.IsNil() {
		return 0
	}
	return int(avutil.GetFrameNbSamples(f.frame.ptr))
}

// SampleRate returns the sample rate for audio frames.
func (f *FrameWrapper) SampleRate() int {
	if f == nil || f.frame.IsNil() {
		return 0
	}
	return int(avutil.GetFrameSampleRate(f.frame.ptr))
}

// IsKeyFrame returns true if this is a keyframe (video only).
func (f *FrameWrapper) IsKeyFrame() bool {
	if f == nil || f.frame.IsNil() {
		return false
	}
	return avutil.GetFrameKeyFrame(f.frame.ptr) != 0
}

// Copy creates a reference to this frame.
// The returned frame shares the same data buffers.
func (f *FrameWrapper) Copy() (*FrameWrapper, error) {
	if f == nil || f.frame.IsNil() {
		return nil, nil
	}

	newFrame := avutil.FrameAlloc()
	if newFrame == nil {
		return nil, ErrOutOfMemory
	}

	if err := avutil.FrameRef(newFrame, f.frame.ptr); err != nil {
		avutil.FrameFree(&newFrame)
		return nil, err
	}

	return &FrameWrapper{
		frame:     Frame{ptr: newFrame, owned: true},
		mediaType: f.mediaType,
	}, nil
}

// Free releases the frame resources.
// After calling Free, the frame must not be used.
func (f *FrameWrapper) Free() error {
	if f == nil {
		return nil
	}
	return f.frame.Free()
}
