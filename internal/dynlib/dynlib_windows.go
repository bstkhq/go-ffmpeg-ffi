//go:build windows && (amd64 || arm64)

// Package dynlib provides the operating-system boundary for loading shared
// libraries used by go-ffmpeg-ffi.
package dynlib

import "syscall"

// Open loads a DLL through the Windows loader. PureGo resolves symbols from
// this handle with GetProcAddress when RegisterLibFunc is called.
func Open(path string) (uintptr, error) {
	handle, err := syscall.LoadLibrary(path)
	return uintptr(handle), err
}

// Close releases a handle returned by Open.
func Close(handle uintptr) error {
	return syscall.FreeLibrary(syscall.Handle(handle))
}
