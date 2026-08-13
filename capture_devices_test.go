//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avdevice"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
)

func TestNewCaptureRejectsUnsupportedPixelFormatBeforeOpeningDevice(t *testing.T) {
	_, err := NewCapture(CaptureConfig{
		Device:      "unused",
		DeviceType:  DeviceTypeVideo,
		PixelFormat: PixelFormat(123456),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported capture pixel format") {
		t.Fatalf("NewCapture error = %v, want unsupported pixel format", err)
	}
}

func TestGetInputFormat_Defaults(t *testing.T) {
	switch runtime.GOOS {
	case "linux":
		if got := getInputFormat(DeviceTypeVideo); got != "v4l2" {
			t.Fatalf("linux video: expected v4l2, got %q", got)
		}
		if got := getInputFormat(DeviceTypeAudio); got != "alsa" {
			t.Fatalf("linux audio: expected alsa, got %q", got)
		}
	case "darwin":
		if got := getInputFormat(DeviceTypeVideo); got != "avfoundation" {
			t.Fatalf("darwin video: expected avfoundation, got %q", got)
		}
		if got := getInputFormat(DeviceTypeAudio); got != "avfoundation" {
			t.Fatalf("darwin audio: expected avfoundation, got %q", got)
		}
	case "windows":
		if got := getInputFormat(DeviceTypeVideo); got != "dshow" {
			t.Fatalf("windows video: expected dshow, got %q", got)
		}
		if got := getInputFormat(DeviceTypeAudio); got != "dshow" {
			t.Fatalf("windows audio: expected dshow, got %q", got)
		}
	default:
		if got := getInputFormat(DeviceTypeVideo); got != "" {
			t.Fatalf("other OS: expected empty, got %q", got)
		}
	}
}

func TestListDevices_Smoke(t *testing.T) {
	devs, err := ListDevices(DeviceTypeVideo)
	if err == nil {
		_ = devs
		return
	}
	// Any meaningful error is OK (missing libs, permissions, unsupported platform),
	// but it must be typed and not the old stub message.
	if errors.Is(err, ErrAVDeviceUnavailable) || errors.Is(err, ErrDeviceEnumerationUnavailable) {
		return
	}
	// Allow other FFmpeg/platform errors too.
}

func TestCaptureDecoderOwnsPacketAndFrameBeforeFirstUse(t *testing.T) {
	if testing.Short() {
		t.Skip("capture integration requires FFmpeg device libraries")
	}
	if !requireFFmpeg(t) {
		return
	}
	if err := avdevice.RegisterAll(); err != nil {
		t.Skipf("libavdevice unavailable: %v", err)
	}

	input := avformat.FindInputFormat("lavfi")
	if input == nil {
		t.Skip("FFmpeg build has no lavfi input device")
	}

	d := newDecoder(newDecoderInterrupt())
	if err := avformat.OpenInput(
		&d.formatCtx,
		"testsrc=duration=1:size=16x16:rate=5",
		input,
		nil,
	); err != nil {
		t.Fatalf("open virtual capture input: %v", err)
	}
	d.interrupt.attach(d.formatCtx)
	if err := d.initializeCaptureDecoder(); err != nil {
		_ = d.Close()
		t.Fatalf("initialize capture decoder: %v", err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Errorf("close capture decoder: %v", err)
		}
	}()

	if d.packet == nil || d.frame == nil {
		t.Fatalf("capture decoder published incomplete resources: packet=%p frame=%p", d.packet, d.frame)
	}
	packet, err := d.ReadPacket()
	if err != nil {
		t.Fatalf("first capture ReadPacket: %v", err)
	}
	if packet == nil || packet.IsNil() {
		t.Fatal("first capture ReadPacket returned no packet")
	}
	frame, err := d.DecodeVideo()
	if err != nil {
		t.Fatalf("first capture DecodeVideo: %v", err)
	}
	if frame.IsNil() {
		t.Fatal("first capture DecodeVideo returned no frame")
	}
}
