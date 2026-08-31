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

var hwDeviceCreationMu sync.Mutex

// NewHWDevice creates a hardware device context for the given type. Device is
// an optional implementation-specific identifier; pass an empty string to use
// FFmpeg's default device.
func NewHWDevice(deviceType HWDeviceType, device string) (*HWDevice, error) {
	if err := bindings.Load(); err != nil {
		return nil, err
	}
	// Some FFmpeg hardware backends are not safe to initialize concurrently.
	// Serialize creation even when callers use separate managers.
	hwDeviceCreationMu.Lock()
	defer hwDeviceCreationMu.Unlock()
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

type hwDeviceKey struct {
	deviceType HWDeviceType
	device     string
}

type hwDeviceFactory func(HWDeviceType, string) (*HWDevice, error)

// HWDeviceManager creates hardware devices once and reuses them across
// decoders. Successful devices and creation failures are cached by device type
// and identifier. A manager is safe for concurrent use.
//
// Close the manager after every decoder using it has been closed. Decoder
// borrows manager-owned devices and never closes them.
type HWDeviceManager struct {
	mu         sync.Mutex
	create     hwDeviceFactory
	devices    map[hwDeviceKey]*HWDevice
	failures   map[hwDeviceKey]error
	selections map[hardwareDecoderSelectionKey]hardwareDecoderPreference
	closed     bool
}

// NewHWDeviceManager returns an empty reusable hardware-device cache.
func NewHWDeviceManager() *HWDeviceManager {
	return newHWDeviceManager(NewHWDevice)
}

func newHWDeviceManager(create hwDeviceFactory) *HWDeviceManager {
	return &HWDeviceManager{
		create:     create,
		devices:    make(map[hwDeviceKey]*HWDevice),
		failures:   make(map[hwDeviceKey]error),
		selections: make(map[hardwareDecoderSelectionKey]hardwareDecoderPreference),
	}
}

func (m *HWDeviceManager) initLocked() {
	if m.create == nil {
		m.create = NewHWDevice
	}
	if m.devices == nil {
		m.devices = make(map[hwDeviceKey]*HWDevice)
	}
	if m.failures == nil {
		m.failures = make(map[hwDeviceKey]error)
	}
	if m.selections == nil {
		m.selections = make(map[hardwareDecoderSelectionKey]hardwareDecoderPreference)
	}
}

// Device returns a borrowed cached hardware device, creating it on the first
// request. The caller must not close the returned device. Creation errors are
// cached, so unavailable backends are not probed again by later decoders using
// the same manager.
func (m *HWDeviceManager) Device(deviceType HWDeviceType, device string) (*HWDevice, error) {
	if m == nil {
		return nil, errors.New("ffmpeg: hardware device manager is nil")
	}
	key := hwDeviceKey{deviceType: deviceType, device: device}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, closedError("hardware device manager")
	}
	m.initLocked()
	if cached := m.devices[key]; cached != nil {
		return cached, nil
	}
	if cached := m.failures[key]; cached != nil {
		return nil, cached
	}
	created, err := m.create(deviceType, device)
	if err != nil {
		m.failures[key] = err
		return nil, err
	}
	if created == nil {
		err = errors.New("ffmpeg: hardware device factory returned nil")
		m.failures[key] = err
		return nil, err
	}
	m.devices[key] = created
	return created, nil
}

func (m *HWDeviceManager) preferredSelection(key hardwareDecoderSelectionKey) (hardwareDecoderPreference, bool) {
	if m == nil {
		return hardwareDecoderPreference{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return hardwareDecoderPreference{}, false
	}
	m.initLocked()
	preference, ok := m.selections[key]
	return preference, ok
}

func (m *HWDeviceManager) rememberSelection(key hardwareDecoderSelectionKey, preference hardwareDecoderPreference) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.initLocked()
		m.selections[key] = preference
	}
}

// Close releases every cached device and prevents further use of the manager.
// It is idempotent.
func (m *HWDeviceManager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	devices := make([]*HWDevice, 0, len(m.devices))
	for _, device := range m.devices {
		devices = append(devices, device)
	}
	m.devices = nil
	m.failures = nil
	m.selections = nil
	m.mu.Unlock()

	var closeErrors []error
	for _, device := range devices {
		if err := device.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}
	return errors.Join(closeErrors...)
}

// HWDecoderConfig configures Decoder's video hardware acceleration. The zero
// value requests automatic selection. HWDevice and devices supplied by
// DeviceManager are borrowed and never closed by Decoder; devices created from
// DeviceType and Device without a manager are decoder-owned.
type HWDecoderConfig struct {
	Mode          HardwareAccelerationMode
	DeviceType    HWDeviceType
	Device        string
	HWDevice      *HWDevice
	DeviceManager *HWDeviceManager
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
		if config.DeviceManager != nil {
			return errors.New("ffmpeg: hardware HWDevice cannot be combined with DeviceManager")
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
		device, err := NewHWDevice(deviceType, "")
		if err == nil && device != nil {
			types = append(types, deviceType)
			_ = device.Close()
		}
	}
	return types
}

// GetHWDeviceTypeName returns FFmpeg's name for a hardware device type.
func GetHWDeviceTypeName(deviceType HWDeviceType) string {
	return avutil.HWDeviceGetTypeName(deviceType)
}
