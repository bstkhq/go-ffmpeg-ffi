//go:build amd64 || arm64

package ffmpeg

import (
	"testing"
	"time"
)

func TestMergeProtocolDecoderOptionsPreservesGenericOptions(t *testing.T) {
	generic := &DecoderOptions{
		Format:          "mpegts",
		AVOptions:       map[string]string{"shared": "generic", "generic": "kept"},
		ProbeSizeBytes:  4096,
		FormatWhitelist: []string{"mpegts"},
		Streams:         []MediaType{MediaTypeVideo},
		ProgramID:       7,
		Hardware:        &HWDecoderConfig{Mode: HardwareAccelerationAuto},
	}
	protocol := &ProtocolOptions{
		Timeout:   3 * time.Second,
		AVOptions: map[string]string{"shared": "protocol", "protocol": "kept"},
	}

	got := mergeProtocolDecoderOptions(generic, protocol)
	if got == generic {
		t.Fatal("merge returned the caller-owned DecoderOptions")
	}
	if got.Format != generic.Format || got.ProbeSizeBytes != generic.ProbeSizeBytes || got.ProgramID != generic.ProgramID {
		t.Fatalf("generic options were not preserved: %#v", got)
	}
	if len(got.Streams) != 1 || got.Streams[0] != MediaTypeVideo {
		t.Fatalf("streams = %v, want video", got.Streams)
	}
	if got.Hardware == nil || got.Hardware == generic.Hardware {
		t.Fatal("hardware decoder config was not preserved as an independent copy")
	}
	if got.AVOptions["generic"] != "kept" || got.AVOptions["protocol"] != "kept" {
		t.Fatalf("merged AVOptions = %#v", got.AVOptions)
	}
	if got.AVOptions["shared"] != "protocol" {
		t.Fatalf("shared option = %q, want protocol", got.AVOptions["shared"])
	}
	if got.AVOptions["timeout"] != "3000000" || got.AVOptions["stimeout"] != "3000000" {
		t.Fatalf("typed timeout options = %#v", got.AVOptions)
	}

	got.AVOptions["generic"] = "changed"
	got.Streams[0] = MediaTypeAudio
	if generic.AVOptions["generic"] != "kept" || generic.Streams[0] != MediaTypeVideo {
		t.Fatal("merge mutated caller-owned decoder options")
	}
}

func TestImageSequenceDecoderOptionsPreservesGenericOptions(t *testing.T) {
	generic := &DecoderOptions{
		AVOptions:      map[string]string{"probesize": "8192", "framerate": "1/1"},
		Streams:        []MediaType{MediaTypeVideo},
		CodecWhitelist: []string{"png"},
		Hardware:       &HWDecoderConfig{DeviceType: HWDeviceTypeVAAPI},
	}
	config := ImageSequenceConfig{
		Pattern:     "frame-%03d.png",
		StartNumber: 4,
		FrameRate:   NewRational(60, 1),
	}

	got := imageSequenceDecoderOptions(config, generic)
	if got.Format != "image2" {
		t.Fatalf("format = %q, want image2", got.Format)
	}
	if got.AVOptions["probesize"] != "8192" {
		t.Fatalf("generic AVOptions = %#v", got.AVOptions)
	}
	if got.AVOptions["pattern_type"] != "sequence" || got.AVOptions["start_number"] != "4" || got.AVOptions["framerate"] != "60/1" {
		t.Fatalf("sequence AVOptions = %#v", got.AVOptions)
	}
	if len(got.CodecWhitelist) != 1 || got.CodecWhitelist[0] != "png" {
		t.Fatalf("codec whitelist = %v", got.CodecWhitelist)
	}
	if got.Hardware == nil || got.Hardware.DeviceType != HWDeviceTypeVAAPI || got.Hardware == generic.Hardware {
		t.Fatalf("hardware config = %#v", got.Hardware)
	}

	got.AVOptions["probesize"] = "changed"
	if generic.AVOptions["probesize"] != "8192" {
		t.Fatal("sequence options mutated caller-owned decoder options")
	}
}
