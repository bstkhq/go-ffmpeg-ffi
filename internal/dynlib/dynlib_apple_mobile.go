//go:build ios && (amd64 || arm64)

// Package dynlib provides the operating-system boundary for loading shared
// libraries used by go-ffmpeg-ffi.
package dynlib

import "github.com/ebitengine/purego"

// Open loads a signed framework embedded in the application, or returns the
// process-wide symbol namespace for FFmpeg that the application linked itself.
// iOS still enforces its normal code-signing and library-validation rules.
func Open(path string) (uintptr, error) {
	if path == ProcessImage {
		return purego.RTLD_DEFAULT, nil
	}
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

// Close releases a framework handle. RTLD_DEFAULT is a pseudo-handle and must
// not be passed to dlclose.
func Close(handle uintptr) error {
	if handle == purego.RTLD_DEFAULT {
		return nil
	}
	return purego.Dlclose(handle)
}
