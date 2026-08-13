//go:build amd64 || arm64

// Package platform provides platform detection and capabilities for ffmpeg.
// It determines what features are available based on the operating system and architecture.
package platform

import (
	"fmt"
	"runtime"
	"unsafe"
)

// SupportsStructByValue indicates whether the platform supports passing/returning
// structs by value through purego. PureGo currently enables this for macOS
// amd64/arm64, but not for GOOS=ios even though iOS also uses the Darwin ABI.
// On other platforms, struct-by-value operations will panic.
const SupportsStructByValue = runtime.GOOS == "darwin" &&
	(runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")

// Is64Bit indicates whether the platform is 64-bit.
// ffmpeg only supports 64-bit platforms due to purego limitations.
const Is64Bit = unsafe.Sizeof(uintptr(0)) == 8

// LibraryExtension is the file extension for shared libraries on this platform.
var LibraryExtension string

// LibraryPrefix is the prefix for shared library names on this platform.
var LibraryPrefix string

func init() {
	switch runtime.GOOS {
	case "darwin", "ios":
		LibraryExtension = ".dylib"
		LibraryPrefix = "lib"
	case "windows":
		LibraryExtension = ".dll"
		LibraryPrefix = ""
	default: // linux, freebsd, etc.
		LibraryExtension = ".so"
		LibraryPrefix = "lib"
	}
}

// FormatLibraryName returns the platform-specific library filename.
// If version is 0, returns the unversioned library name.
//
// Examples:
//   - Linux:   FormatLibraryName("avcodec", 60) -> "libavcodec.so.60"
//   - macOS:   FormatLibraryName("avcodec", 60) -> "libavcodec.60.dylib"
//   - Windows: FormatLibraryName("avcodec", 60) -> "avcodec-60.dll"
//   - Android: FormatLibraryName("avcodec", 60) -> "libavcodec.so"
func FormatLibraryName(name string, version int) string {
	switch runtime.GOOS {
	case "darwin", "ios":
		if version > 0 {
			return fmt.Sprintf("%s%s.%d%s", LibraryPrefix, name, version, LibraryExtension)
		}
		return fmt.Sprintf("%s%s%s", LibraryPrefix, name, LibraryExtension)
	case "windows":
		if version > 0 {
			return fmt.Sprintf("%s%s-%d%s", LibraryPrefix, name, version, LibraryExtension)
		}
		return fmt.Sprintf("%s%s%s", LibraryPrefix, name, LibraryExtension)
	case "android":
		// Android packages and nativeLibraryDir recognize unversioned lib*.so
		// filenames. The runtime ABI is validated immediately after loading, so
		// the filename does not need to encode the FFmpeg major.
		return fmt.Sprintf("%s%s%s", LibraryPrefix, name, LibraryExtension)
	default: // linux, freebsd
		if version > 0 {
			return fmt.Sprintf("%s%s%s.%d", LibraryPrefix, name, LibraryExtension, version)
		}
		return fmt.Sprintf("%s%s%s", LibraryPrefix, name, LibraryExtension)
	}
}

// GOOS returns the current operating system.
func GOOS() string {
	return runtime.GOOS
}

// GOARCH returns the current architecture.
func GOARCH() string {
	return runtime.GOARCH
}
