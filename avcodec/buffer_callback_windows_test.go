//go:build windows && (amd64 || arm64)

package avcodec

import "github.com/ebitengine/purego"

func newCountedBufferCallback() uintptr {
	return purego.NewCallback(func(_ purego.CDecl, _ uintptr, data *byte) uintptr {
		countedBufferRelease(data)
		return 0
	})
}
