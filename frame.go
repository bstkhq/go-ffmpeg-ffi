//go:build amd64 || arm64

package ffmpeg

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

// Raw returns the underlying AVFrame* pointer for passing into low-level APIs.
func (f Frame) Raw() avutil.Frame {
	if f.IsNil() {
		return nil
	}
	return f.ptr
}

// PTS returns the presentation timestamp of the frame.
func (f Frame) PTS() int64 {
	if f.IsNil() {
		return avutil.AV_NOPTS_VALUE
	}
	return avutil.GetFramePTS(f.ptr)
}

// SetPTS sets the presentation timestamp of the frame.
func (f Frame) SetPTS(pts int64) {
	if !f.IsNil() {
		avutil.SetFramePTS(f.ptr, pts)
	}
}

// MediaType returns the media type represented by the AVFrame fields.
func (f Frame) MediaType() MediaType {
	if f.IsNil() {
		return MediaTypeUnknown
	}
	switch {
	case f.Width() > 0 && f.Height() > 0:
		return MediaTypeVideo
	case f.NbSamples() > 0 || f.SampleRate() > 0:
		return MediaTypeAudio
	default:
		return MediaTypeUnknown
	}
}

// Width returns the frame width (video only).
func (f Frame) Width() int {
	if f.IsNil() {
		return 0
	}
	return int(avutil.GetFrameWidth(f.ptr))
}

// SetWidth sets the frame width.
func (f Frame) SetWidth(width int) {
	if !f.IsNil() {
		avutil.SetFrameWidth(f.ptr, int32(width))
	}
}

// Height returns the frame height (video only).
func (f Frame) Height() int {
	if f.IsNil() {
		return 0
	}
	return int(avutil.GetFrameHeight(f.ptr))
}

// SetHeight sets the frame height.
func (f Frame) SetHeight(height int) {
	if !f.IsNil() {
		avutil.SetFrameHeight(f.ptr, int32(height))
	}
}

// Format returns the AVPixelFormat or AVSampleFormat value stored in the frame.
func (f Frame) Format() int32 {
	if f.IsNil() {
		return -1
	}
	return avutil.GetFrameFormat(f.ptr)
}

// SetFormat sets the raw AVPixelFormat or AVSampleFormat value.
func (f Frame) SetFormat(format int32) {
	if !f.IsNil() {
		avutil.SetFrameFormat(f.ptr, format)
	}
}

// PixelFormat returns the frame format as an AVPixelFormat.
func (f Frame) PixelFormat() PixelFormat { return PixelFormat(f.Format()) }

// SetPixelFormat sets the frame's AVPixelFormat.
func (f Frame) SetPixelFormat(format PixelFormat) { f.SetFormat(int32(format)) }

// SampleFormat returns the frame format as an AVSampleFormat.
func (f Frame) SampleFormat() SampleFormat { return SampleFormat(f.Format()) }

// SetSampleFormat sets the frame's AVSampleFormat.
func (f Frame) SetSampleFormat(format SampleFormat) { f.SetFormat(int32(format)) }

// NbSamples returns AVFrame.nb_samples (audio only).
func (f Frame) NbSamples() int {
	if f.IsNil() {
		return 0
	}
	return int(avutil.GetFrameNbSamples(f.ptr))
}

// SetNbSamples sets AVFrame.nb_samples.
func (f Frame) SetNbSamples(samples int) {
	if !f.IsNil() {
		avutil.SetFrameNbSamples(f.ptr, int32(samples))
	}
}

// SampleRate returns the sample rate (audio only).
func (f Frame) SampleRate() int {
	if f.IsNil() {
		return 0
	}
	return int(avutil.GetFrameSampleRate(f.ptr))
}

// SetSampleRate sets the sample rate.
func (f Frame) SetSampleRate(sampleRate int) {
	if !f.IsNil() {
		avutil.SetFrameSampleRate(f.ptr, int32(sampleRate))
	}
}

// Channels returns the number of audio channels.
func (f Frame) Channels() int {
	if f.IsNil() {
		return 0
	}
	return int(avutil.GetFrameChannels(f.ptr))
}

// SetChannels sets the default channel layout for the requested channel count.
func (f Frame) SetChannels(channels int) {
	if !f.IsNil() {
		avutil.FrameSetChannels(f.ptr, int32(channels))
	}
}

// GetBuffer allocates buffers according to the frame's format and dimensions.
func (f Frame) GetBuffer(align int32) error {
	if err := f.useError(); err != nil {
		return err
	}
	return avutil.FrameGetBufferErr(f.ptr, align)
}

// MakeWritable ensures that the frame data can be modified in place.
func (f Frame) MakeWritable() error {
	if err := f.useError(); err != nil {
		return err
	}
	return avutil.FrameMakeWritable(f.ptr)
}

// IsKeyFrame reports whether the frame is marked as a keyframe.
func (f Frame) IsKeyFrame() bool {
	return !f.IsNil() && avutil.GetFrameKeyFrame(f.ptr) != 0
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
	if f == nil {
		return avutil.AV_NOPTS_VALUE
	}
	return f.frame.PTS()
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
	if f == nil {
		return 0
	}
	return f.frame.Width()
}

// Height returns the frame height (video only).
func (f *FrameWrapper) Height() int {
	if f == nil {
		return 0
	}
	return f.frame.Height()
}

// Format returns the pixel format (video) or sample format (audio).
func (f *FrameWrapper) Format() int32 {
	if f == nil {
		return -1
	}
	return f.frame.Format()
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
	if f == nil {
		return nil
	}
	return frameData(f.frame, f.mediaType, plane)
}

// Data returns a borrowed slice to the frame data for the specified plane.
// It infers whether the frame contains video or audio from its AVFrame fields.
// The slice is invalidated when the underlying frame is reused or released.
func (f Frame) Data(plane int) []byte {
	return frameData(f, f.MediaType(), plane)
}

func frameData(frame Frame, mediaType MediaType, plane int) []byte {
	if frame.IsNil() || plane < 0 {
		return nil
	}

	var data unsafe.Pointer
	var size uintptr
	switch mediaType {
	case MediaTypeVideo:
		if plane >= 4 {
			return nil
		}
		data = avutil.GetFrameDataPlane(frame.ptr, plane)
		if data == nil {
			return nil
		}
		linesizes := avutil.GetFrameLinesize(frame.ptr)
		planeSizes, err := avutil.ImagePlaneSizes(
			frame.PixelFormat(),
			frame.Height(),
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
		format := frame.SampleFormat()
		channels := frame.Channels()
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
		samples := frame.NbSamples()
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
		data = avutil.GetFrameExtendedDataPlane(frame.ptr, plane)
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
	if f == nil {
		return 0
	}
	return frameLinesize(f.frame, f.mediaType, plane)
}

// Linesize returns the line size (stride) for the specified plane.
func (f Frame) Linesize(plane int) int {
	return frameLinesize(f, f.MediaType(), plane)
}

func frameLinesize(frame Frame, mediaType MediaType, plane int) int {
	if frame.IsNil() || plane < 0 {
		return 0
	}
	linesize := avutil.GetFrameLinesize(frame.ptr)
	if mediaType == MediaTypeAudio {
		format := frame.SampleFormat()
		channels := frame.Channels()
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
	if f == nil {
		return 0
	}
	return f.frame.NbSamples()
}

// SampleRate returns the sample rate for audio frames.
func (f *FrameWrapper) SampleRate() int {
	if f == nil {
		return 0
	}
	return f.frame.SampleRate()
}

// IsKeyFrame returns true if this is a keyframe (video only).
func (f *FrameWrapper) IsKeyFrame() bool {
	return f != nil && f.frame.IsKeyFrame()
}

// Copy creates a reference to this frame.
// The returned frame shares the same data buffers.
func (f *FrameWrapper) Copy() (*FrameWrapper, error) {
	if f == nil || f.frame.IsNil() {
		return nil, nil
	}

	clone, err := f.frame.Clone()
	if err != nil {
		return nil, err
	}

	return &FrameWrapper{
		frame:     clone,
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
