//go:build !windows && (amd64 || arm64)

package ffmpeg

import "github.com/ebitengine/purego"

func nativeDecoderInterruptCallback(cdecl purego.CDecl, opaque uintptr) int32 {
	return decoderInterruptCallback(cdecl, opaque)
}

func nativeCustomIOReadCallback(cdecl purego.CDecl, opaque uintptr, buf *byte, bufSize int32) int32 {
	return customIOReadCallback(cdecl, opaque, buf, bufSize)
}

func nativeCustomIOWriteCallback(cdecl purego.CDecl, opaque uintptr, buf *byte, bufSize int32) int32 {
	return customIOWriteCallback(cdecl, opaque, buf, bufSize)
}

func nativeCustomIOSeekCallback(cdecl purego.CDecl, opaque uintptr, offset int64, whence int32) int64 {
	return customIOSeekCallback(cdecl, opaque, offset, whence)
}

func nativeLogCallbackTrampoline(cdecl purego.CDecl, avcl uintptr, level int32, msg *byte) {
	logCallbackTrampoline(cdecl, avcl, level, msg)
}

func nativeWrappedBufferFreeCallback(cdecl purego.CDecl, opaque uintptr, data *byte) {
	wrappedBufferFreeCallback(cdecl, opaque, data)
}
