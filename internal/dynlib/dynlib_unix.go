//go:build !ios && !windows && (amd64 || arm64)

// Package dynlib provides the operating-system boundary for loading shared
// libraries used by go-ffmpeg-ffi.
package dynlib

import "github.com/ebitengine/purego"

// Open loads a shared library and exposes its symbols to dependent FFmpeg
// libraries. Android implements these calls through PureGo's bionic loader.
func Open(path string) (uintptr, error) {
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

// Close releases a handle returned by Open.
func Close(handle uintptr) error {
	return purego.Dlclose(handle)
}
