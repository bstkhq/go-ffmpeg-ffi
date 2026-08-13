//go:build !windows && (amd64 || arm64)

package avutil

import "syscall"

// FFmpeg's AVERROR macro negates errno values from the target C runtime.
const (
	AVERROR_EAGAIN int32 = -int32(syscall.EAGAIN)
	AVERROR_EINVAL int32 = -int32(syscall.EINVAL)
	AVERROR_ENOMEM int32 = -int32(syscall.ENOMEM)
)
