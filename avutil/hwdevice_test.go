//go:build amd64 || arm64

package avutil

import (
	"testing"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/cstr"
)

func TestHWDeviceCtxCreateDeviceArgument(t *testing.T) {
	original := avHWDeviceCtxCreate
	t.Cleanup(func() { avHWDeviceCtxCreate = original })

	var marker byte
	var gotDevice unsafe.Pointer
	var gotDeviceName string
	avHWDeviceCtxCreate = func(ctx *unsafe.Pointer, _ int32, device unsafe.Pointer, _ uintptr, _ int32) int32 {
		gotDevice = device
		gotDeviceName = cstr.String(device, 256)
		*ctx = unsafe.Pointer(&marker)
		return 0
	}

	t.Run("default device", func(t *testing.T) {
		ctx, err := HWDeviceCtxCreate(HWDeviceTypeDRM, "")
		if err != nil {
			t.Fatal(err)
		}
		if ctx != unsafe.Pointer(&marker) {
			t.Fatalf("context = %p, want %p", ctx, &marker)
		}
		if gotDevice != nil {
			t.Fatalf("device pointer = %p, want nil", gotDevice)
		}
	})

	t.Run("explicit device", func(t *testing.T) {
		const device = "/dev/dri/card0"
		ctx, err := HWDeviceCtxCreate(HWDeviceTypeDRM, device)
		if err != nil {
			t.Fatal(err)
		}
		if ctx != unsafe.Pointer(&marker) {
			t.Fatalf("context = %p, want %p", ctx, &marker)
		}
		if gotDevice == nil {
			t.Fatal("device pointer is nil")
		}
		if gotDeviceName != device {
			t.Fatalf("device = %q, want %q", gotDeviceName, device)
		}
	})
}
