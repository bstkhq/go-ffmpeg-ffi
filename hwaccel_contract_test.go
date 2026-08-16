//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"io"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestTransferHWFrameToSystemPropagatesFailure(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	dst := avutil.FrameAlloc()
	if dst == nil {
		t.Fatal("allocate destination frame")
	}
	defer avutil.FrameFree(&dst)

	want := errors.New("transfer failed")
	calls := 0
	err := transferHWFrameToSystem(dst, nil, func(_, _ avutil.Frame, _ int32) error {
		calls++
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("transfer error = %v, want wrapped %v", err, want)
	}
	if calls != 1 {
		t.Fatalf("transfer calls = %d, want 1", calls)
	}
}

func TestTransferHWFrameToSystemRejectsMissingDestination(t *testing.T) {
	called := false
	err := transferHWFrameToSystem(nil, nil, func(_, _ avutil.Frame, _ int32) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrOutOfMemory) {
		t.Fatalf("transfer error = %v, want %v", err, ErrOutOfMemory)
	}
	if called {
		t.Fatal("transfer called without a destination frame")
	}
}

func TestHWDeviceAttachRejectsClosedDevice(t *testing.T) {
	device := &HWDevice{closed: true}
	err := device.attachToCodecContext(nil)
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("attach error = %v, want %v", err, ErrClosed)
	}
}

func TestValidateHWDecoderConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *HWDecoderConfig
		ok     bool
	}{
		{name: "nil", config: nil, ok: true},
		{name: "auto", config: &HWDecoderConfig{}, ok: true},
		{name: "explicit type", config: &HWDecoderConfig{DeviceType: HWDeviceTypeVAAPI}, ok: true},
		{name: "device needs type", config: &HWDecoderConfig{Device: "/dev/dri/renderD128"}},
		{name: "invalid mode", config: &HWDecoderConfig{Mode: HardwareAccelerationMode(99)}},
		{name: "device and borrowed device", config: &HWDecoderConfig{Device: "gpu", DeviceType: HWDeviceTypeVAAPI, HWDevice: &HWDevice{deviceType: HWDeviceTypeVAAPI}}},
		{name: "mismatched borrowed device", config: &HWDecoderConfig{DeviceType: HWDeviceTypeCUDA, HWDevice: &HWDevice{deviceType: HWDeviceTypeVAAPI}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateHWDecoderConfig(test.config)
			if (err == nil) != test.ok {
				t.Fatalf("validate error = %v, want success %v", err, test.ok)
			}
		})
	}
}

func TestDecoderAutoHardwareFallbackIsObservable(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	input := createTestVideo(t)
	if input == "" {
		return
	}
	decoder, err := NewDecoder(input, &DecoderOptions{
		Streams: []MediaType{MediaTypeVideo},
		Hardware: &HWDecoderConfig{
			Mode:       HardwareAccelerationAuto,
			DeviceType: HWDeviceTypeOHCodec,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	if got := decoder.VideoDecoderInfo().HardwareState; got != HardwareStatePending {
		t.Fatalf("state before open = %s, want pending", got)
	}
	if err := decoder.OpenVideoDecoder(); err != nil {
		t.Fatal(err)
	}
	info := decoder.VideoDecoderInfo()
	if info.HardwareState != HardwareStateFallback || info.FallbackReason == "" {
		t.Fatalf("decoder info after fallback = %#v", info)
	}
	frame, err := decoder.DecodeVideo()
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if err == nil && frame.IsNil() {
		t.Fatal("software fallback returned a nil frame")
	}
}

func TestDecoderRequiredHardwareDoesNotFallback(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	input := createTestVideo(t)
	if input == "" {
		return
	}
	decoder, err := NewDecoder(input, &DecoderOptions{
		Streams: []MediaType{MediaTypeVideo},
		Hardware: &HWDecoderConfig{
			Mode:       HardwareAccelerationRequired,
			DeviceType: HWDeviceTypeOHCodec,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	if err := decoder.OpenVideoDecoder(); !errors.Is(err, ErrHardwareAccelerationUnavailable) {
		t.Fatalf("open error = %v, want ErrHardwareAccelerationUnavailable", err)
	}
}

func TestRequiredHardwareRejectsLazySoftwareFallback(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	frame := avutil.FrameAlloc()
	if frame == nil {
		t.Fatal("allocate frame")
	}
	defer avutil.FrameFree(&frame)
	avutil.SetFrameFormat(frame, int32(PixelFormatYUV420P))

	decoder := newDecoder(nil)
	decoder.frame = frame
	decoder.hardwareConfig = &HWDecoderConfig{Mode: HardwareAccelerationRequired}
	decoder.hardwarePixelFormat = int32(PixelFormatNone)
	decoder.videoDecoderInfo.HardwareState = HardwareStateSelected
	if _, err := decoder.prepareDecodedFrameLocked(MediaTypeVideo); !errors.Is(err, ErrHardwareAccelerationUnavailable) {
		t.Fatalf("prepare error = %v, want ErrHardwareAccelerationUnavailable", err)
	}
	if decoder.videoDecoderInfo.HardwareState != HardwareStateFallback {
		t.Fatalf("state = %s, want fallback", decoder.videoDecoderInfo.HardwareState)
	}
}
