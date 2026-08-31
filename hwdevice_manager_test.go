//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
)

func TestHWDeviceManagerReusesDeviceConcurrently(t *testing.T) {
	var calls atomic.Int32
	manager := newHWDeviceManager(func(deviceType HWDeviceType, _ string) (*HWDevice, error) {
		calls.Add(1)
		return &HWDevice{deviceType: deviceType}, nil
	})
	t.Cleanup(func() { _ = manager.Close() })

	const workers = 16
	devices := make(chan *HWDevice, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			device, err := manager.Device(HWDeviceTypeCUDA, "gpu0")
			if err != nil {
				t.Errorf("Device: %v", err)
				return
			}
			devices <- device
		}()
	}
	group.Wait()
	close(devices)

	var first *HWDevice
	for device := range devices {
		if first == nil {
			first = device
		}
		if device != first {
			t.Fatal("manager returned different devices for the same key")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("device creations = %d, want 1", got)
	}
}

func TestHWDeviceManagerCachesCreationFailure(t *testing.T) {
	want := errors.New("backend unavailable")
	var calls atomic.Int32
	manager := newHWDeviceManager(func(HWDeviceType, string) (*HWDevice, error) {
		calls.Add(1)
		return nil, want
	})
	t.Cleanup(func() { _ = manager.Close() })

	for range 3 {
		if _, err := manager.Device(HWDeviceTypeVAAPI, ""); !errors.Is(err, want) {
			t.Fatalf("Device error = %v, want %v", err, want)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("device creations = %d, want 1", got)
	}
}

func TestHWDeviceManagerCachesKeysIndependently(t *testing.T) {
	var calls atomic.Int32
	manager := newHWDeviceManager(func(deviceType HWDeviceType, _ string) (*HWDevice, error) {
		calls.Add(1)
		return &HWDevice{deviceType: deviceType}, nil
	})
	t.Cleanup(func() { _ = manager.Close() })

	first, err := manager.Device(HWDeviceTypeCUDA, "gpu0")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Device(HWDeviceTypeCUDA, "gpu1")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("different device identifiers reused the same device")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("device creations = %d, want 2", got)
	}
}

func TestHWDeviceManagerClose(t *testing.T) {
	manager := newHWDeviceManager(func(deviceType HWDeviceType, _ string) (*HWDevice, error) {
		return &HWDevice{deviceType: deviceType}, nil
	})
	device, err := manager.Device(HWDeviceTypeCUDA, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	if !device.closed {
		t.Fatal("manager did not close its cached device")
	}
	if _, err := manager.Device(HWDeviceTypeCUDA, ""); !errors.Is(err, ErrClosed) {
		t.Fatalf("Device after Close error = %v, want ErrClosed", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestHWDeviceManagerCachesAutomaticSelection(t *testing.T) {
	manager := NewHWDeviceManager()
	key := hardwareDecoderSelectionKey{codecID: 12}
	want := hardwareDecoderPreference{codec: 42, deviceType: HWDeviceTypeCUDA, pixelFormat: 7}
	manager.rememberSelection(key, want)
	got, ok := manager.preferredSelection(key)
	if !ok || got != want {
		t.Fatalf("preferred selection = (%#v, %v), want (%#v, true)", got, ok, want)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCachedAutomaticSelectionIsPrioritized(t *testing.T) {
	candidates := []hardwareDecoderCandidate{
		{config: avcodec.CodecHWConfig{DeviceType: HWDeviceTypeVAAPI, PixelFormat: 1}},
		{config: avcodec.CodecHWConfig{DeviceType: HWDeviceTypeCUDA, PixelFormat: 2}},
		{config: avcodec.CodecHWConfig{DeviceType: HWDeviceTypeDRM, PixelFormat: 3}},
	}
	preferred := hardwareDecoderPreferenceFor(candidates[1])
	prioritizeHardwareDecoderCandidates(candidates, preferred)
	if candidates[0].config.DeviceType != HWDeviceTypeCUDA {
		t.Fatalf("first candidate = %v, want CUDA", candidates[0].config.DeviceType)
	}
}

func TestAutomaticHardwareSelectionExcludesVulkan(t *testing.T) {
	auto := &HWDecoderConfig{}
	explicit := &HWDecoderConfig{DeviceType: HWDeviceTypeVulkan}
	if automaticHardwareDeviceAllowed(auto, HWDeviceTypeVulkan) {
		t.Fatal("automatic selection includes Vulkan")
	}
	if !automaticHardwareDeviceAllowed(explicit, HWDeviceTypeVulkan) {
		t.Fatal("explicit selection excludes Vulkan")
	}
	if !automaticHardwareDeviceAllowed(auto, HWDeviceTypeCUDA) {
		t.Fatal("automatic selection excludes CUDA")
	}
}
