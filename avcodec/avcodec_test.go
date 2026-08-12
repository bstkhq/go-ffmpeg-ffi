//go:build !ios && (amd64 || arm64)

package avcodec

import (
	"os"
	"testing"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
)

var ffmpegAvailable bool

func TestMain(m *testing.M) {
	if err := bindings.Load(); err == nil {
		ffmpegAvailable = true
	}
	os.Exit(m.Run())
}

func requireFFmpeg(t *testing.T) bool {
	t.Helper()
	if !ffmpegAvailable {
		t.Log("FFmpeg not available")
		return false
	}
	return true
}

func TestFindDecoder(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	// H.264 decoder should always be available
	codec := FindDecoder(CodecIDH264)
	if codec == nil {
		t.Fatal("FindDecoder(H264) returned nil")
	}

	name := GetCodecName(codec)
	if name == "" {
		t.Error("GetCodecName returned empty string")
	}
	t.Logf("H.264 decoder: %s", name)
}

func TestFindEncoder(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	// Try to find an encoder - some systems may not have all encoders
	codecs := []struct {
		id   CodecID
		name string
	}{
		{CodecIDH264, "H.264"},
		{CodecIDMPEG4, "MPEG4"},
		{CodecIDMJPEG, "MJPEG"},
	}

	found := false
	for _, c := range codecs {
		codec := FindEncoder(c.id)
		if codec != nil {
			name := GetCodecName(codec)
			t.Logf("Found %s encoder: %s", c.name, name)
			found = true
			break
		}
	}

	if !found {
		t.Log("No common encoders found - this may be expected on some systems")
	}
}

func TestFindDecoderByName(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	codec := FindDecoderByName("h264")
	if codec == nil {
		t.Fatalf("h264 decoder not found by name")
	}

	name := GetCodecName(codec)
	t.Logf("Found decoder: %s", name)
}

func TestAllocContext3(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	codec := FindDecoder(CodecIDH264)
	if codec == nil {
		t.Fatalf("H264 decoder not found")
	}

	ctx := AllocContext3(codec)
	if ctx == nil {
		t.Fatal("AllocContext3 returned nil")
	}
	defer FreeContext(&ctx)

	if ctx == nil {
		t.Error("Context should still be valid before free")
	}
}

func TestFreeContext(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	codec := FindDecoder(CodecIDH264)
	if codec == nil {
		t.Fatalf("H264 decoder not found")
	}

	ctx := AllocContext3(codec)
	if ctx == nil {
		t.Fatal("AllocContext3 returned nil")
	}

	FreeContext(&ctx)

	if ctx != nil {
		t.Error("Context should be nil after free")
	}

	// Double free should be safe
	FreeContext(&ctx)
}

func TestFreeContextPropagatesNativePointerUpdate(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	originalFreeContext := avcodecFreeContext
	t.Cleanup(func() { avcodecFreeContext = originalFreeContext })

	nativeContext := allocNativeTestPointer(t)

	avcodecFreeContext = func(slot *unsafe.Pointer) {
		if slot == nil {
			t.Fatal("avcodec_free_context received a nil slot")
		}
		if *slot != nativeContext {
			t.Fatalf("avcodec_free_context received %p, want %p", *slot, nativeContext)
		}
		*slot = nil
	}

	ctx := Context(nativeContext)
	FreeContext(&ctx)
	if ctx != nil {
		t.Fatalf("FreeContext left context at %p", ctx)
	}
}

func TestOpen2PropagatesDictionaryUpdate(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	originalOpen2 := avcodecOpen2
	t.Cleanup(func() { avcodecOpen2 = originalOpen2 })

	initial := allocNativeTestPointer(t)
	replacement := allocNativeTestPointer(t)

	avcodecOpen2 = func(_, _ uintptr, slot *unsafe.Pointer) int32 {
		if slot == nil {
			t.Fatal("avcodec_open2 received a nil options slot")
		}
		if *slot != initial {
			t.Fatalf("avcodec_open2 received %p, want %p", *slot, initial)
		}
		*slot = replacement
		return 0
	}

	options := avutil.Dictionary(initial)
	if err := Open2(nil, nil, &options); err != nil {
		t.Fatalf("Open2 returned error: %v", err)
	}
	if options != replacement {
		t.Fatalf("Open2 left options at %p, want %p", options, replacement)
	}
}

func BenchmarkFreeContextPointerStaging(b *testing.B) {
	if !ffmpegAvailable {
		b.Skip("FFmpeg not available")
	}

	originalFreeContext := avcodecFreeContext
	b.Cleanup(func() { avcodecFreeContext = originalFreeContext })

	nativeContext := allocNativeTestPointer(b)

	avcodecFreeContext = func(slot *unsafe.Pointer) { *slot = nil }
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ctx := Context(nativeContext)
		FreeContext(&ctx)
	}
}

func BenchmarkOpen2PointerStaging(b *testing.B) {
	if !ffmpegAvailable {
		b.Skip("FFmpeg not available")
	}

	originalOpen2 := avcodecOpen2
	b.Cleanup(func() { avcodecOpen2 = originalOpen2 })

	nativeOptions := allocNativeTestPointer(b)

	avcodecOpen2 = func(_, _ uintptr, _ *unsafe.Pointer) int32 { return 0 }
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		options := avutil.Dictionary(nativeOptions)
		if err := Open2(nil, nil, &options); err != nil {
			b.Fatal(err)
		}
	}
}

func allocNativeTestPointer(tb testing.TB) unsafe.Pointer {
	tb.Helper()
	ptr := avutil.Malloc(1)
	if ptr == nil {
		tb.Fatal("avutil.Malloc returned nil")
	}
	tb.Cleanup(func() { avutil.Free(ptr) })
	return ptr
}

func TestPacketAllocFree(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	pkt := PacketAlloc()
	if pkt == nil {
		t.Fatal("PacketAlloc returned nil")
	}

	PacketFree(&pkt)

	if pkt != nil {
		t.Error("Packet should be nil after free")
	}

	// Double free should be safe
	PacketFree(&pkt)
}

func TestCodecIDConstants(t *testing.T) {
	// Verify codec IDs match FFmpeg constants
	if CodecIDH264 != 27 {
		t.Errorf("CodecIDH264: expected 27, got %d", CodecIDH264)
	}
	if CodecIDHEVC != 173 {
		t.Errorf("CodecIDHEVC: expected 173, got %d", CodecIDHEVC)
	}
	if CodecIDAV1 != 226 {
		t.Errorf("CodecIDAV1: expected 226, got %d", CodecIDAV1)
	}
}

func TestVersion(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	ver := bindings.AVCodecVersion()
	if ver == 0 {
		t.Error("AVCodecVersion returned 0")
	}
	t.Logf("avcodec version: %d.%d.%d", ver>>16, (ver>>8)&0xFF, ver&0xFF)
}

func TestSendPacketReturnsCodecFlowControl(t *testing.T) {
	original := avcodecSendPacket
	t.Cleanup(func() { avcodecSendPacket = original })

	for _, code := range []int32{avutil.AVERROR_EAGAIN, avutil.AVERROR_EOF} {
		avcodecSendPacket = func(_, _ uintptr) int32 { return code }

		err := SendPacket(nil, nil)
		if avutil.Code(err) != code {
			t.Fatalf("SendPacket error code = %d, want %d", avutil.Code(err), code)
		}
	}
}

func TestSendFrameReturnsCodecFlowControl(t *testing.T) {
	original := avcodecSendFrame
	t.Cleanup(func() { avcodecSendFrame = original })

	for _, code := range []int32{avutil.AVERROR_EAGAIN, avutil.AVERROR_EOF} {
		avcodecSendFrame = func(_, _ uintptr) int32 { return code }

		err := SendFrame(nil, nil)
		if avutil.Code(err) != code {
			t.Fatalf("SendFrame error code = %d, want %d", avutil.Code(err), code)
		}
	}
}
