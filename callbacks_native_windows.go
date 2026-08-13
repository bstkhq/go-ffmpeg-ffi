//go:build windows && (amd64 || arm64)

package ffmpeg

import "github.com/ebitengine/purego"

// Windows callbacks registered through PureGo use syscall.NewCallbackCDecl,
// which requires exactly one uintptr-sized result. Native callers still read
// the C return type, so preserve the low 32 bits for int callbacks and all 64
// bits for int64 callbacks. Native void callbacks ignore the zero result.
func nativeDecoderInterruptCallback(cdecl purego.CDecl, opaque uintptr) uintptr {
	return uintptr(uint32(decoderInterruptCallback(cdecl, opaque)))
}

func nativeCustomIOReadCallback(cdecl purego.CDecl, opaque uintptr, buf *byte, bufSize int32) uintptr {
	return uintptr(uint32(customIOReadCallback(cdecl, opaque, buf, bufSize)))
}

func nativeCustomIOWriteCallback(cdecl purego.CDecl, opaque uintptr, buf *byte, bufSize int32) uintptr {
	return uintptr(uint32(customIOWriteCallback(cdecl, opaque, buf, bufSize)))
}

func nativeCustomIOSeekCallback(cdecl purego.CDecl, opaque uintptr, offset int64, whence int32) uintptr {
	return uintptr(uint64(customIOSeekCallback(cdecl, opaque, offset, whence)))
}

func nativeLogCallbackTrampoline(cdecl purego.CDecl, avcl uintptr, level int32, msg *byte) uintptr {
	logCallbackTrampoline(cdecl, avcl, level, msg)
	return 0
}

func nativeWrappedBufferFreeCallback(cdecl purego.CDecl, opaque uintptr, data *byte) uintptr {
	wrappedBufferFreeCallback(cdecl, opaque, data)
	return 0
}
