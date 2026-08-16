//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"fmt"
	"sync"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
)

// HWDeviceType represents a hardware accelerator type.
type HWDeviceType = avutil.HWDeviceType

// Hardware device type constants re-exported from avutil.
const (
	HWDeviceTypeNone         = avutil.HWDeviceTypeNone
	HWDeviceTypeVDPAU        = avutil.HWDeviceTypeVDPAU
	HWDeviceTypeCUDA         = avutil.HWDeviceTypeCUDA
	HWDeviceTypeVAAPI        = avutil.HWDeviceTypeVAAPI
	HWDeviceTypeDXVA2        = avutil.HWDeviceTypeDXVA2
	HWDeviceTypeQSV          = avutil.HWDeviceTypeQSV
	HWDeviceTypeVideoToolbox = avutil.HWDeviceTypeVideoToolbox
	HWDeviceTypeD3D11VA      = avutil.HWDeviceTypeD3D11VA
	HWDeviceTypeDRM          = avutil.HWDeviceTypeDRM
	HWDeviceTypeOpenCL       = avutil.HWDeviceTypeOpenCL
	HWDeviceTypeMediaCodec   = avutil.HWDeviceTypeMediaCodec
	HWDeviceTypeVulkan       = avutil.HWDeviceTypeVulkan
	HWDeviceTypeD3D12VA      = avutil.HWDeviceTypeD3D12VA
	HWDeviceTypeAMF          = avutil.HWDeviceTypeAMF
	HWDeviceTypeOHCodec      = avutil.HWDeviceTypeOHCodec
)

// HardwareAccelerationMode controls hardware-decoder fallback behavior.
type HardwareAccelerationMode uint8

const (
	// HardwareAccelerationAuto tries compatible hardware decoders in platform
	// preference order and falls back to the regular software decoder.
	HardwareAccelerationAuto HardwareAccelerationMode = iota

	// HardwareAccelerationRequired returns an error instead of falling back to
	// software when no compatible hardware decoder can be opened.
	HardwareAccelerationRequired

	// HardwareAccelerationDisabled forces the regular software decoder. A nil
	// DecoderOptions.Hardware has the same effect.
	HardwareAccelerationDisabled
)

// HardwareAccelerationState describes the resolved video decoder path.
type HardwareAccelerationState uint8

const (
	HardwareStateDisabled HardwareAccelerationState = iota
	HardwareStatePending
	HardwareStateSelected
	HardwareStateActive
	HardwareStateFallback
)

func (s HardwareAccelerationState) String() string {
	switch s {
	case HardwareStatePending:
		return "pending"
	case HardwareStateSelected:
		return "selected"
	case HardwareStateActive:
		return "active"
	case HardwareStateFallback:
		return "fallback"
	default:
		return "disabled"
	}
}

// VideoDecoderInfo reports which decoder backs the selected video stream.
// HardwareState can change from selected to active or fallback after the first
// decoded frame because some FFmpeg decoders initialize acceleration lazily.
type VideoDecoderInfo struct {
	CodecName      string
	HardwareState  HardwareAccelerationState
	HWDeviceType   HWDeviceType
	HWDeviceName   string
	FallbackReason string
}

// HWDevice represents an FFmpeg hardware device context.
type HWDevice struct {
	mu         sync.Mutex
	deviceCtx  avutil.HWDeviceContext
	deviceType HWDeviceType
	closed     bool
}

// NewHWDevice creates a hardware device context for the given type. Device is
// an optional implementation-specific identifier; pass an empty string to use
// FFmpeg's default device.
func NewHWDevice(deviceType HWDeviceType, device string) (*HWDevice, error) {
	if err := bindings.Load(); err != nil {
		return nil, err
	}
	ctx, err := avutil.HWDeviceCtxCreate(deviceType, device)
	if err != nil {
		return nil, err
	}
	return &HWDevice{deviceCtx: ctx, deviceType: deviceType}, nil
}

// NewHWDeviceByName creates a hardware device context by FFmpeg device name.
func NewHWDeviceByName(name, device string) (*HWDevice, error) {
	deviceType := avutil.HWDeviceFindTypeByName(name)
	if deviceType == HWDeviceTypeNone {
		return nil, errors.New("ffmpeg: unknown hardware device type: " + name)
	}
	return NewHWDevice(deviceType, device)
}

// Type returns the hardware device type.
func (d *HWDevice) Type() HWDeviceType {
	if d == nil {
		return HWDeviceTypeNone
	}
	return d.deviceType
}

// TypeName returns FFmpeg's name for the hardware device type.
func (d *HWDevice) TypeName() string {
	if d == nil {
		return ""
	}
	return avutil.HWDeviceGetTypeName(d.deviceType)
}

// Context returns the underlying hardware device context. The pointer remains
// borrowed from HWDevice and must not be freed by the caller.
func (d *HWDevice) Context() avutil.HWDeviceContext {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deviceCtx
}

func (d *HWDevice) attachToCodecContext(codecCtx avcodec.Context) error {
	if d == nil {
		return errors.New("ffmpeg: hardware device is nil")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.deviceCtx == nil {
		return closedError("hardware device")
	}
	if err := avcodec.SetCtxHWDeviceCtx(codecCtx, d.deviceCtx); err != nil {
		return fmt.Errorf("ffmpeg: attach hardware device: %w", err)
	}
	return nil
}

// Close releases the device. Codec contexts to which it was already attached
// retain their own FFmpeg reference.
func (d *HWDevice) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	if d.deviceCtx != nil {
		avutil.FreeBufferRef(&d.deviceCtx)
	}
	return nil
}

// HWDecoderConfig configures Decoder's video hardware acceleration. The zero
// value requests automatic selection. HWDevice is borrowed and is never closed
// by Decoder; devices created from DeviceType and Device are decoder-owned.
type HWDecoderConfig struct {
	Mode       HardwareAccelerationMode
	DeviceType HWDeviceType
	Device     string
	HWDevice   *HWDevice
}

func cloneHWDecoderConfig(config *HWDecoderConfig) *HWDecoderConfig {
	if config == nil {
		return nil
	}
	clone := *config
	return &clone
}

func validateHWDecoderConfig(config *HWDecoderConfig) error {
	if config == nil || config.Mode == HardwareAccelerationDisabled {
		return nil
	}
	if config.Mode > HardwareAccelerationDisabled {
		return fmt.Errorf("ffmpeg: invalid hardware acceleration mode %d", config.Mode)
	}
	if config.HWDevice != nil {
		if config.Device != "" {
			return errors.New("ffmpeg: hardware Device cannot be combined with HWDevice")
		}
		if config.DeviceType != HWDeviceTypeNone && config.DeviceType != config.HWDevice.Type() {
			return errors.New("ffmpeg: hardware DeviceType does not match HWDevice")
		}
	}
	if config.Device != "" && config.DeviceType == HWDeviceTypeNone {
		return errors.New("ffmpeg: hardware Device requires an explicit DeviceType")
	}
	return nil
}

// AvailableHWDeviceTypes returns device types that FFmpeg can create on the
// current system. It probes only types known by the loaded FFmpeg build.
func AvailableHWDeviceTypes() []HWDeviceType {
	types := make([]HWDeviceType, 0, 8)
	for deviceType := avutil.HWDeviceIterateTypes(HWDeviceTypeNone); deviceType != HWDeviceTypeNone; deviceType = avutil.HWDeviceIterateTypes(deviceType) {
		ctx, err := avutil.HWDeviceCtxCreate(deviceType, "")
		if err == nil && ctx != nil {
			types = append(types, deviceType)
			avutil.FreeBufferRef(&ctx)
		}
	}
	return types
}

// GetHWDeviceTypeName returns FFmpeg's name for a hardware device type.
func GetHWDeviceTypeName(deviceType HWDeviceType) string {
	return avutil.HWDeviceGetTypeName(deviceType)
}
