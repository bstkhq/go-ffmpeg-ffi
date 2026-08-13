//go:build amd64 || arm64

package avformat

import (
	"os"
	"path/filepath"
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

func testInputVideo(t *testing.T) string {
	t.Helper()
	testFile := filepath.Join("..", "testdata", "test.mp4")
	if _, err := os.Stat(testFile); err != nil {
		t.Fatalf("test input missing: %v", err)
	}
	return testFile
}

func TestAllocContext(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	ctx := AllocContext()
	if ctx == nil {
		t.Fatal("AllocContext returned nil")
	}
	FreeContext(ctx)
}

func TestReadFrameRejectsNilPointers(t *testing.T) {
	valid := unsafe.Pointer(new(byte))
	for _, tt := range []struct {
		name string
		ctx  FormatContext
		pkt  unsafe.Pointer
	}{
		{name: "context", ctx: nil, pkt: valid},
		{name: "packet", ctx: valid, pkt: nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if code := avutil.Code(ReadFrame(tt.ctx, tt.pkt)); code != avutil.AVERROR_EINVAL {
				t.Fatalf("ReadFrame error code = %d, want %d", code, avutil.AVERROR_EINVAL)
			}
		})
	}
}

func TestIOContextFreeOwnsCurrentBuffer(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	original := avutil.Malloc(1024)
	if original == nil {
		t.Fatal("allocate original I/O buffer")
	}
	ctx := IOAllocContext(original, 1024, true, 0, 0, 0, 0)
	if ctx == nil {
		avutil.Free(original)
		t.Fatal("allocate I/O context")
	}

	replacement := avutil.Malloc(2048)
	if replacement == nil {
		IOContextFree(&ctx)
		t.Fatal("allocate replacement I/O buffer")
	}
	buffer := (*unsafe.Pointer)(unsafe.Pointer(uintptr(ctx) + offsetIOContextBuffer))
	*buffer = replacement
	avutil.Free(original)

	IOContextFree(&ctx)
	if ctx != nil {
		t.Fatal("I/O context was not cleared")
	}
}

func TestOpenInput(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	testFile := testInputVideo(t)

	var ctx FormatContext
	err := OpenInput(&ctx, testFile, nil, nil)
	if err != nil {
		t.Fatalf("OpenInput failed: %v", err)
	}
	defer CloseInput(&ctx)

	if ctx == nil {
		t.Error("Context should not be nil after OpenInput")
	}
}

func TestFindStreamInfo(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	testFile := testInputVideo(t)

	var ctx FormatContext
	if err := OpenInput(&ctx, testFile, nil, nil); err != nil {
		t.Fatalf("OpenInput failed: %v", err)
	}
	defer CloseInput(&ctx)

	err := FindStreamInfo(ctx, nil)
	if err != nil {
		t.Fatalf("FindStreamInfo failed: %v", err)
	}

	// Should have at least one stream
	numStreams := GetNbStreams(ctx)
	if numStreams < 1 {
		t.Errorf("Expected at least 1 stream, got %d", numStreams)
	}
	t.Logf("Found %d streams", numStreams)
}

func TestFindBestStream(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	testFile := testInputVideo(t)

	var ctx FormatContext
	if err := OpenInput(&ctx, testFile, nil, nil); err != nil {
		t.Fatalf("OpenInput failed: %v", err)
	}
	defer CloseInput(&ctx)

	if err := FindStreamInfo(ctx, nil); err != nil {
		t.Fatalf("FindStreamInfo failed: %v", err)
	}

	// Find video stream
	videoIdx := FindBestStream(ctx, avutil.MediaTypeVideo, -1, -1, nil, 0)
	if videoIdx < 0 {
		t.Error("No video stream found")
	} else {
		t.Logf("Video stream index: %d", videoIdx)
	}

	// Find audio stream
	audioIdx := FindBestStream(ctx, avutil.MediaTypeAudio, -1, -1, nil, 0)
	if audioIdx < 0 {
		t.Error("No audio stream found")
	} else {
		t.Logf("Audio stream index: %d", audioIdx)
	}
}

func TestReadFrame(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	testFile := testInputVideo(t)

	var ctx FormatContext
	if err := OpenInput(&ctx, testFile, nil, nil); err != nil {
		t.Fatalf("OpenInput failed: %v", err)
	}
	defer CloseInput(&ctx)

	if err := FindStreamInfo(ctx, nil); err != nil {
		t.Fatalf("FindStreamInfo failed: %v", err)
	}

	// Read a few frames
	pkt := AllocPacket()
	if pkt == nil {
		t.Fatal("AllocPacket returned nil")
	}
	defer FreePacket(&pkt)

	frameCount := 0
	for i := 0; i < 10; i++ {
		err := ReadFrame(ctx, pkt)
		if err != nil {
			break
		}
		frameCount++
		PacketUnref(pkt)
	}

	if frameCount == 0 {
		t.Error("No frames read")
	} else {
		t.Logf("Read %d packets", frameCount)
	}
}

func TestVersion(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	ver := bindings.AVFormatVersion()
	if ver == 0 {
		t.Error("AVFormatVersion returned 0")
	}
	t.Logf("avformat version: %d.%d.%d", ver>>16, (ver>>8)&0xFF, ver&0xFF)
}
