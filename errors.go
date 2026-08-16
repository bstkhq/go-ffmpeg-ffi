//go:build amd64 || arm64

package ffmpeg

import (
	"errors"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

type resourceClosedError string

func (e resourceClosedError) Error() string {
	return "ffmpeg: " + string(e) + " is closed"
}

func (e resourceClosedError) Is(target error) bool {
	return target == ErrClosed
}

func closedError(resource string) error {
	return resourceClosedError(resource)
}

// FFmpegError is an error from FFmpeg operations.
// It contains the raw FFmpeg error code and a human-readable message.
type FFmpegError = avutil.Error

// Common errors
var (
	// ErrOutOfMemory indicates memory allocation failed.
	ErrOutOfMemory = errors.New("ffmpeg: out of memory")

	// ErrNotLoaded indicates FFmpeg libraries are not loaded.
	ErrNotLoaded = errors.New("ffmpeg: FFmpeg libraries not loaded")

	// ErrClosed indicates the resource has been closed.
	ErrClosed = errors.New("ffmpeg: resource is closed")

	// ErrNoVideoStream indicates no video stream is present.
	ErrNoVideoStream = errors.New("ffmpeg: no video stream")

	// ErrNoAudioStream indicates no audio stream is present.
	ErrNoAudioStream = errors.New("ffmpeg: no audio stream")

	// ErrDecoderNotOpened indicates the decoder has not been opened.
	ErrDecoderNotOpened = errors.New("ffmpeg: decoder not opened")

	// ErrAVDeviceUnavailable indicates FFmpeg's libavdevice could not be loaded.
	ErrAVDeviceUnavailable = errors.New("ffmpeg: libavdevice not available")

	// ErrDeviceEnumerationUnavailable indicates device enumeration is not available
	// (e.g. missing shim wrappers, unsupported FFmpeg build, or platform constraints).
	ErrDeviceEnumerationUnavailable = errors.New("ffmpeg: device enumeration not available")

	// ErrHardwareAccelerationUnavailable indicates that hardware decoding was
	// required but no compatible decoder and device could be opened.
	ErrHardwareAccelerationUnavailable = errors.New("ffmpeg: hardware acceleration unavailable")

	// ErrFrameLeaseReturned indicates a copied pooled frame was used after its lease was returned.
	ErrFrameLeaseReturned = errors.New("ffmpeg: frame pool lease has already been returned")
)

// Error code constants re-exported from avutil
const (
	AVERROR_EOF               = avutil.AVERROR_EOF
	AVERROR_EAGAIN            = avutil.AVERROR_EAGAIN
	AVERROR_EINVAL            = avutil.AVERROR_EINVAL
	AVERROR_ENOMEM            = avutil.AVERROR_ENOMEM
	AVERROR_DECODER_NOT_FOUND = avutil.AVERROR_DECODER_NOT_FOUND
	AVERROR_ENCODER_NOT_FOUND = avutil.AVERROR_ENCODER_NOT_FOUND
)

// NewError creates an FFmpegError from an error code.
// Returns nil if code >= 0.
func NewError(code int32, op string) error {
	return avutil.NewError(code, op)
}

// ErrorCode returns the FFmpeg error code from an error, or 0 if not an FFmpeg error.
func ErrorCode(err error) int32 {
	return avutil.Code(err)
}
