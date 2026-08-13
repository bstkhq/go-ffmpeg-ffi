//go:build windows && (amd64 || arm64)

package avutil

// FFmpeg uses the Windows C runtime errno values. Go's syscall package exposes
// unrelated synthetic errno values on Windows, so they cannot define AVERROR.
const (
	AVERROR_EAGAIN int32 = -11
	AVERROR_EINVAL int32 = -22
	AVERROR_ENOMEM int32 = -12
)
