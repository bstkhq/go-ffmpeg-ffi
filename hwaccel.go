//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"fmt"
	"runtime"
	"sort"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

type hardwareDecoderCandidate struct {
	codec  avcodec.Codec
	config avcodec.CodecHWConfig
	order  int
}

func hardwareDecoderCandidates(codecID avcodec.CodecID, config *HWDecoderConfig) []hardwareDecoderCandidate {
	defaultCodec := avcodec.FindDecoder(codecID)
	codecs := make([]avcodec.Codec, 0, 8)
	seen := make(map[uintptr]struct{})
	addCodec := func(codec avcodec.Codec) {
		key := uintptr(codec)
		if codec == nil {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		codecs = append(codecs, codec)
	}
	addCodec(defaultCodec)
	var opaque uintptr
	for {
		codec := avcodec.IterateCodecs(&opaque)
		if codec == nil {
			break
		}
		if avcodec.IsDecoder(codec) && avcodec.GetCodecID(codec) == codecID {
			addCodec(codec)
		}
	}

	requiredType := config.DeviceType
	if config.HWDevice != nil {
		requiredType = config.HWDevice.Type()
	}
	candidates := make([]hardwareDecoderCandidate, 0, len(codecs))
	for codecOrder, codec := range codecs {
		for i := 0; ; i++ {
			hwConfig, ok := avcodec.GetCodecHWConfig(codec, i)
			if !ok {
				break
			}
			if hwConfig.Methods&avcodec.HWConfigMethodDeviceContext == 0 {
				continue
			}
			if requiredType != HWDeviceTypeNone && hwConfig.DeviceType != requiredType {
				continue
			}
			candidates = append(candidates, hardwareDecoderCandidate{
				codec: codec, config: hwConfig, order: codecOrder,
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := hardwareDeviceRank(candidates[i].config.DeviceType)
		right := hardwareDeviceRank(candidates[j].config.DeviceType)
		if left != right {
			return left < right
		}
		return candidates[i].order < candidates[j].order
	})
	return candidates
}

func hardwareDeviceRank(deviceType HWDeviceType) int {
	var preferred []HWDeviceType
	switch runtime.GOOS {
	case "android":
		preferred = []HWDeviceType{HWDeviceTypeMediaCodec, HWDeviceTypeVulkan}
	case "darwin", "ios":
		preferred = []HWDeviceType{HWDeviceTypeVideoToolbox}
	case "windows":
		preferred = []HWDeviceType{HWDeviceTypeD3D12VA, HWDeviceTypeD3D11VA, HWDeviceTypeDXVA2, HWDeviceTypeQSV, HWDeviceTypeCUDA, HWDeviceTypeAMF}
	default:
		preferred = []HWDeviceType{HWDeviceTypeVAAPI, HWDeviceTypeQSV, HWDeviceTypeCUDA, HWDeviceTypeVulkan, HWDeviceTypeVDPAU, HWDeviceTypeDRM}
	}
	for index, candidate := range preferred {
		if candidate == deviceType {
			return index
		}
	}
	return len(preferred) + int(deviceType)
}

func (d *Decoder) openHardwareVideoDecoderLocked(codecPar avcodec.Parameters, codecID avcodec.CodecID) error {
	config := d.hardwareConfig
	if config == nil || config.Mode == HardwareAccelerationDisabled {
		return d.openSoftwareVideoDecoderLocked(codecPar, codecID, "")
	}
	if err := validateHWDecoderConfig(config); err != nil {
		return err
	}

	candidates := hardwareDecoderCandidates(codecID, config)
	var failures []error
	for _, candidate := range candidates {
		device := config.HWDevice
		owned := false
		if device == nil {
			var err error
			device, err = NewHWDevice(candidate.config.DeviceType, config.Device)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s device: %w", avutil.HWDeviceGetTypeName(candidate.config.DeviceType), err))
				continue
			}
			owned = true
		}

		codecCtx, err := openVideoCodecContext(codecPar, candidate.codec, device)
		if err != nil {
			if owned {
				_ = device.Close()
			}
			failures = append(failures, fmt.Errorf("%s with %s: %w", avcodec.GetCodecName(candidate.codec), device.TypeName(), err))
			continue
		}

		d.videoCodecCtx = codecCtx
		d.videoDecoderOpen = true
		d.hardwarePixelFormat = int32(candidate.config.PixelFormat)
		d.hardwareSoftwareOutput = candidate.config.Methods&avcodec.HWConfigMethodAdHoc != 0
		if owned {
			d.ownedHWDevice = device
		}
		d.videoDecoderInfo = VideoDecoderInfo{
			CodecName:     avcodec.GetCodecName(candidate.codec),
			HardwareState: HardwareStateSelected,
			HWDeviceType:  candidate.config.DeviceType,
			HWDeviceName:  device.TypeName(),
		}
		return nil
	}

	failure := errors.Join(failures...)
	if len(candidates) == 0 {
		failure = errors.New("no decoder advertises a compatible hardware device configuration")
	}
	if config.Mode == HardwareAccelerationRequired {
		return fmt.Errorf("%w: %v", ErrHardwareAccelerationUnavailable, failure)
	}
	return d.openSoftwareVideoDecoderLocked(codecPar, codecID, failure.Error())
}

func openVideoCodecContext(codecPar avcodec.Parameters, codec avcodec.Codec, device *HWDevice) (avcodec.Context, error) {
	codecCtx := avcodec.AllocContext3(codec)
	if codecCtx == nil {
		return nil, errors.New("ffmpeg: failed to allocate codec context")
	}
	if err := avcodec.ParametersToContext(codecCtx, codecPar); err != nil {
		avcodec.FreeContext(&codecCtx)
		return nil, err
	}
	if device != nil {
		if err := device.attachToCodecContext(codecCtx); err != nil {
			avcodec.FreeContext(&codecCtx)
			return nil, err
		}
	}
	if err := avcodec.Open2(codecCtx, codec, nil); err != nil {
		avcodec.FreeContext(&codecCtx)
		return nil, err
	}
	return codecCtx, nil
}

func (d *Decoder) openSoftwareVideoDecoderLocked(codecPar avcodec.Parameters, codecID avcodec.CodecID, fallbackReason string) error {
	codec := avcodec.FindDecoder(codecID)
	if codec == nil {
		return errors.New("ffmpeg: decoder not found")
	}
	codecCtx, err := openVideoCodecContext(codecPar, codec, nil)
	if err != nil {
		return err
	}
	d.videoCodecCtx = codecCtx
	d.videoDecoderOpen = true
	d.hardwarePixelFormat = int32(avutil.PixelFormatNone)
	state := HardwareStateDisabled
	if fallbackReason != "" {
		state = HardwareStateFallback
	}
	d.videoDecoderInfo = VideoDecoderInfo{
		CodecName:      avcodec.GetCodecName(codec),
		HardwareState:  state,
		FallbackReason: fallbackReason,
	}
	return nil
}

func (d *Decoder) prepareDecodedFrameLocked(mediaType MediaType) (Frame, error) {
	state := d.videoDecoderInfo.HardwareState
	if mediaType != MediaTypeVideo || (state != HardwareStateSelected && state != HardwareStateActive) {
		return Frame{ptr: d.frame, owned: false}, nil
	}

	frameFormat := avutil.GetFrameFormat(d.frame)
	if frameFormat != d.hardwarePixelFormat {
		// Wrapper decoders such as MediaCodec can return CPU-backed frames after
		// decoding in hardware when no rendering surface is attached.
		if d.hardwareSoftwareOutput {
			d.videoDecoderInfo.HardwareState = HardwareStateActive
		} else {
			d.videoDecoderInfo.HardwareState = HardwareStateFallback
			d.videoDecoderInfo.FallbackReason = "FFmpeg selected a software pixel format while initializing the hardware decoder"
			if d.hardwareConfig != nil && d.hardwareConfig.Mode == HardwareAccelerationRequired {
				return Frame{}, fmt.Errorf("%w: %s", ErrHardwareAccelerationUnavailable, d.videoDecoderInfo.FallbackReason)
			}
		}
		return Frame{ptr: d.frame, owned: false}, nil
	}

	if d.hardwareSoftwareFrame == nil {
		d.hardwareSoftwareFrame = avutil.FrameAlloc()
		if d.hardwareSoftwareFrame == nil {
			return Frame{}, ErrOutOfMemory
		}
	}
	if err := transferHWFrameToSystem(d.hardwareSoftwareFrame, d.frame, avutil.HWFrameTransferData); err != nil {
		return Frame{}, err
	}
	d.frame, d.hardwareSoftwareFrame = d.hardwareSoftwareFrame, d.frame
	d.videoDecoderInfo.HardwareState = HardwareStateActive
	return Frame{ptr: d.frame, owned: false}, nil
}

type hwFrameTransferFunc func(dst, src avutil.Frame, flags int32) error

func transferHWFrameToSystem(dst, src avutil.Frame, transfer hwFrameTransferFunc) error {
	if dst == nil {
		return ErrOutOfMemory
	}
	avutil.FrameUnref(dst)
	if err := transfer(dst, src, 0); err != nil {
		return fmt.Errorf("ffmpeg: transfer hardware frame to system memory: %w", err)
	}
	if err := avutil.FrameCopyProps(dst, src); err != nil {
		avutil.FrameUnref(dst)
		return fmt.Errorf("ffmpeg: copy hardware frame properties: %w", err)
	}
	return nil
}
