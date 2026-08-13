//go:build amd64 || arm64

package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/abi"
)

func TestFindShimLibrary_RespectsFFmpegShimDir(t *testing.T) {
	dir := t.TempDir()

	var name string
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		name = "libffshim.so"
	case "darwin":
		name = "libffshim.dylib"
	case "ios":
		name = "libffshim.dylib"
	case "windows":
		name = "ffshim.dll"
	default:
		t.Logf("unsupported OS for this test: %s", runtime.GOOS)
		return
	}

	fake := filepath.Join(dir, name)
	if err := os.WriteFile(fake, []byte("not a real shim"), 0o644); err != nil {
		t.Fatalf("write fake shim: %v", err)
	}

	t.Setenv("FFMPEG_SHIM_DIR", dir)

	got, err := findShimLibrary()
	if err != nil {
		t.Fatalf("findShimLibrary error: %v", err)
	}
	if got != fake {
		t.Fatalf("expected %q, got %q", fake, got)
	}
}

func TestFindShimLibrary_FFmpegShimDirNotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FFMPEG_SHIM_DIR", dir)

	_, err := findShimLibrary()
	if err == nil {
		t.Fatal("expected error when FFMPEG_SHIM_DIR doesn't contain shim")
	}
	if !strings.Contains(err.Error(), "FFMPEG_SHIM_DIR") {
		t.Errorf("error should mention FFMPEG_SHIM_DIR: %v", err)
	}
}

func TestFindShimLibraryInPathsDoesNotUseWorkingDirectory(t *testing.T) {
	name := ExpectedLibraryName()
	cwd := t.TempDir()
	trusted := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, name), []byte("not a real shim"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	if path, err := findShimLibraryInPaths([]string{name}, []string{"", trusted}); err == nil {
		t.Fatalf("found untrusted working-directory shim at %q", path)
	}
}

func TestTrustedShimSearchPathsExcludeSourceAndWorkingDirectories(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	executableDir := filepath.Join(t.TempDir(), "bin")

	paths := trustedShimSearchPathsFor(runtime.GOOS, runtime.GOARCH, executableDir)
	for _, path := range paths {
		if path == cwd || path == moduleRoot || path == filepath.Join(moduleRoot, "shim") {
			t.Fatalf("untrusted implicit shim search path %q", path)
		}
	}
	if paths[len(paths)-1] != executableDir {
		t.Fatalf("executable directory missing from trusted paths: %v", paths)
	}
}

func TestExpectedLibraryName(t *testing.T) {
	name := ExpectedLibraryName()

	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		if name != "libffshim.so" {
			t.Errorf("expected libffshim.so on %s, got %s", runtime.GOOS, name)
		}
	case "darwin", "ios":
		if name != "libffshim.dylib" {
			t.Errorf("expected libffshim.dylib on Apple platform, got %s", name)
		}
	case "windows":
		if name != "ffshim.dll" {
			t.Errorf("expected ffshim.dll on windows, got %s", name)
		}
	}
}

func TestCodecCtxHWSettersRetainAndReplaceReferences(t *testing.T) {
	if testing.Short() {
		t.Skip("hardware reference ownership requires FFmpeg and ffshim")
	}
	if err := Load(); err != nil {
		t.Fatal(err)
	}
	if !IsLoaded() {
		t.Skip("ffshim is not available")
	}

	tests := []struct {
		name string
		set  func(unsafe.Pointer, unsafe.Pointer) error
		get  func(unsafe.Pointer) (unsafe.Pointer, error)
	}{
		{name: "device", set: CodecCtxSetHWDeviceCtx, get: CodecCtxHWDeviceCtx},
		{name: "frames", set: CodecCtxSetHWFramesCtx, get: CodecCtxHWFramesCtx},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := avcodec.AllocContext3(nil)
			if ctx == nil {
				t.Fatal("allocate codec context")
			}
			defer avcodec.FreeContext(&ctx)

			data := avutil.Malloc(1)
			if data == nil {
				t.Fatal("av_malloc failed")
			}
			source := avutil.BufferCreate(data, 1, 0, 0, 0)
			if source == nil {
				avutil.Free(data)
				t.Fatal("av_buffer_create failed")
			}
			defer avutil.FreeBufferRef(&source)

			if err := tt.set(ctx, source); err != nil {
				t.Fatal(err)
			}
			stored, err := tt.get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if stored == nil {
				t.Fatal("codec context did not retain the source")
			}
			if stored == source {
				t.Fatal("codec context borrowed the caller's AVBufferRef instead of retaining it")
			}

			avutil.FreeBufferRef(&source)
			if err := tt.set(ctx, nil); err != nil {
				t.Fatal(err)
			}
			stored, err = tt.get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if stored != nil {
				t.Fatal("nil assignment did not release the retained reference")
			}
		})
	}
}

func TestValidateVersionInfoSupportedFamilies(t *testing.T) {
	tests := []struct {
		ffmpeg                    int
		avutil, avcodec, avformat uint32
	}{
		{ffmpeg: 6, avutil: 58, avcodec: 60, avformat: 60},
		{ffmpeg: 7, avutil: 59, avcodec: 61, avformat: 61},
		{ffmpeg: 8, avutil: 60, avcodec: 62, avformat: 62},
		{ffmpeg: 9, avutil: 61, avcodec: 63, avformat: 63},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("FFmpeg %d", tt.ffmpeg), func(t *testing.T) {
			core, ok := abi.ForFFmpegMajor(tt.ffmpeg)
			if !ok {
				t.Fatal("missing core layout")
			}
			info := VersionInfo{
				API:                APIVersion,
				BuildAVUtilMajor:   tt.avutil,
				BuildAVCodecMajor:  tt.avcodec,
				BuildAVFormatMajor: tt.avformat,
				RuntimeAVUtil:      shimTestVersion(tt.avutil, 1, 0),
				RuntimeAVCodec:     shimTestVersion(tt.avcodec, 1, 0),
				RuntimeAVFormat:    shimTestVersion(tt.avformat, 1, 0),
			}
			got, err := validateVersionInfo(core, info)
			if err != nil {
				t.Fatal(err)
			}
			if got.FFmpegMajor != tt.ffmpeg {
				t.Fatalf("FFmpeg major = %d, want %d", got.FFmpegMajor, tt.ffmpeg)
			}
		})
	}
}

func TestValidateVersionInfoRejectsMismatches(t *testing.T) {
	core, ok := abi.ForFFmpegMajor(7)
	if !ok {
		t.Fatal("missing FFmpeg 7 layout")
	}
	valid := VersionInfo{
		API:                APIVersion,
		BuildAVUtilMajor:   59,
		BuildAVCodecMajor:  61,
		BuildAVFormatMajor: 61,
		RuntimeAVUtil:      shimTestVersion(59, 1, 0),
		RuntimeAVCodec:     shimTestVersion(61, 1, 0),
		RuntimeAVFormat:    shimTestVersion(61, 1, 0),
	}

	tests := []struct {
		name string
		edit func(*VersionInfo)
	}{
		{name: "shim API", edit: func(info *VersionInfo) { info.API++ }},
		{name: "mixed build tuple", edit: func(info *VersionInfo) { info.BuildAVUtilMajor = 58 }},
		{name: "mixed runtime tuple", edit: func(info *VersionInfo) { info.RuntimeAVCodec = shimTestVersion(60, 1, 0) }},
		{name: "different build and runtime", edit: func(info *VersionInfo) {
			info.BuildAVUtilMajor = 58
			info.BuildAVCodecMajor = 60
			info.BuildAVFormatMajor = 60
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := valid
			tt.edit(&info)
			if _, err := validateVersionInfo(core, info); err == nil {
				t.Fatal("validateVersionInfo unexpectedly succeeded")
			}
		})
	}

	ffmpeg8, _ := abi.ForFFmpegMajor(8)
	if _, err := validateVersionInfo(ffmpeg8, valid); err == nil {
		t.Fatal("shim for FFmpeg 7 was accepted with an FFmpeg 8 core")
	}
}

func shimTestVersion(major, minor, patch uint32) uint32 {
	return major<<16 | minor<<8 | patch
}

func TestBuildInstructions(t *testing.T) {
	instructions := BuildInstructions()

	if instructions == "" {
		t.Error("BuildInstructions should not be empty")
	}

	// Should contain platform-specific guidance
	switch runtime.GOOS {
	case "linux":
		if !strings.Contains(instructions, "apt") && !strings.Contains(instructions, "libav") {
			t.Error("Linux instructions should mention apt or libav packages")
		}
	case "darwin":
		if !strings.Contains(instructions, "brew") && !strings.Contains(instructions, "Homebrew") {
			t.Error("macOS instructions should mention Homebrew")
		}
	case "windows":
		if !strings.Contains(instructions, "MSYS2") && !strings.Contains(instructions, "MinGW") {
			t.Error("Windows instructions should mention MSYS2 or MinGW")
		}
	}
}

func TestStatus_BeforeLoad(t *testing.T) {
	// Create a fresh state by resetting (this is just for testing)
	loadMu.Lock()
	wasLoaded := loaded.Load()
	wasErr := loadErr
	wasPath := shimPath
	loaded.Store(false)
	loadErr = nil
	shimPath = ""
	loadMu.Unlock()

	// Restore after test
	defer func() {
		loadMu.Lock()
		loaded.Store(wasLoaded)
		loadErr = wasErr
		shimPath = wasPath
		loadMu.Unlock()
	}()

	status := Status()
	if !strings.Contains(status, "not loaded") {
		t.Errorf("Status should indicate not loaded: %s", status)
	}
}

func TestIsLoaded_Initial(t *testing.T) {
	// This test just verifies the function doesn't panic
	_ = IsLoaded()
}

func TestPath_WhenNotLoaded(t *testing.T) {
	// If shim is not loaded, Path should return empty string
	loadMu.Lock()
	wasLoaded := loaded.Load()
	wasPath := shimPath
	if !loaded.Load() {
		shimPath = ""
	}
	loadMu.Unlock()

	defer func() {
		loadMu.Lock()
		loaded.Store(wasLoaded)
		shimPath = wasPath
		loadMu.Unlock()
	}()

	if !IsLoaded() {
		path := Path()
		if path != "" && !IsLoaded() {
			t.Error("Path should be empty when shim is not loaded")
		}
	}
}

func TestSetLogCallback_WithoutShim(t *testing.T) {
	// Test that calling SetLogCallback without shim returns appropriate error
	loadMu.Lock()
	wasLoaded := loaded.Load()
	loaded.Store(false)
	loadMu.Unlock()

	defer func() {
		loadMu.Lock()
		loaded.Store(wasLoaded)
		loadMu.Unlock()
	}()

	err := SetLogCallback(0)
	if err == nil {
		t.Error("SetLogCallback should fail when shim is not loaded")
	}
	if !strings.Contains(err.Error(), "shim") {
		t.Errorf("error should mention shim: %v", err)
	}
}

func TestSetLogLevel_WithoutShim(t *testing.T) {
	loadMu.Lock()
	wasLoaded := loaded.Load()
	loaded.Store(false)
	loadMu.Unlock()

	defer func() {
		loadMu.Lock()
		loaded.Store(wasLoaded)
		loadMu.Unlock()
	}()

	err := SetLogLevel(32)
	if err == nil {
		t.Error("SetLogLevel should fail when shim is not loaded")
	}
}

func TestLog_WithoutShim(t *testing.T) {
	loadMu.Lock()
	wasLoaded := loaded.Load()
	loaded.Store(false)
	loadMu.Unlock()

	defer func() {
		loadMu.Lock()
		loaded.Store(wasLoaded)
		loadMu.Unlock()
	}()

	err := Log(nil, 32, "test message")
	if err == nil {
		t.Error("Log should fail when shim is not loaded")
	}
}

func TestNewChapter_WithoutShim(t *testing.T) {
	loadMu.Lock()
	wasLoaded := loaded.Load()
	loaded.Store(false)
	loadMu.Unlock()

	defer func() {
		loadMu.Lock()
		loaded.Store(wasLoaded)
		loadMu.Unlock()
	}()

	_, err := NewChapter(nil, 1, 1, 1000, 0, 1000, nil)
	if err == nil {
		t.Error("NewChapter should fail when shim is not loaded")
	}
}

func TestAVDeviceListInputSources_WithoutShim(t *testing.T) {
	loadMu.Lock()
	wasLoaded := loaded.Load()
	loaded.Store(false)
	loadMu.Unlock()

	defer func() {
		loadMu.Lock()
		loaded.Store(wasLoaded)
		loadMu.Unlock()
	}()

	_, _, _, err := AVDeviceListInputSources("v4l2", "", nil)
	if err == nil {
		t.Error("AVDeviceListInputSources should fail when shim is not loaded")
	}
}

func TestAVFrameColorOffsets_WithoutShim(t *testing.T) {
	loadMu.Lock()
	wasLoaded := loaded.Load()
	loaded.Store(false)
	loadMu.Unlock()

	defer func() {
		loadMu.Lock()
		loaded.Store(wasLoaded)
		loadMu.Unlock()
	}()

	_, _, _, _, err := AVFrameColorOffsets()
	if err == nil {
		t.Error("AVFrameColorOffsets should fail when shim is not loaded")
	}
}

func TestConcurrentLoadAndFunctionCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shim load test in short mode")
	}

	requireShim := os.Getenv("FFMPEG_SHIM_DIR") != ""
	const readerCount = 16
	start := make(chan struct{})
	stop := make(chan struct{})
	var ready sync.WaitGroup
	var readers sync.WaitGroup
	ready.Add(readerCount)
	readers.Add(readerCount)

	callShim := func() {
		_ = IsLoaded()
		_ = SetLogLevel(32)
	}

	for range readerCount {
		go func() {
			defer readers.Done()
			<-start
			callShim()
			ready.Done()
			for {
				select {
				case <-stop:
					callShim()
					return
				default:
					callShim()
				}
			}
		}()
	}

	close(start)
	ready.Wait()
	err := Load()
	close(stop)
	readers.Wait()

	if err != nil && requireShim {
		t.Fatalf("loading shim: %v", err)
	}
	if !IsLoaded() {
		if requireShim {
			t.Fatal("shim configured through FFMPEG_SHIM_DIR was not loaded")
		}
		if err != nil {
			t.Logf("shim unavailable: %v", err)
		}
		t.Log("shim not available; concurrent calls stayed in the unloaded path")
		return
	}
	if err := SetLogLevel(32); err != nil {
		t.Fatalf("calling published shim function: %v", err)
	}
	if info := Info(); info.API == 0 || info.FFmpegMajor == 0 {
		t.Fatalf("successful load did not publish shim version info: %+v", info)
	}
}

// Integration test - only runs if shim is available
func TestLoad_Integration(t *testing.T) {
	if testing.Short() {
		t.Log("skipping integration test in short mode")
		return
	}

	err := Load()
	if err != nil {
		t.Logf("Load returned error (expected if shim not built): %v", err)
	}

	// After Load, we should be able to check status
	status := Status()
	t.Logf("Shim status: %s", status)

	if IsLoaded() {
		path := Path()
		t.Logf("Shim loaded from: %s", path)

		// Test that we can call shim functions without panic
		// (actual functionality depends on FFmpeg being available)
	} else {
		t.Log("Shim not loaded - this is OK, core functionality works without it")
		t.Logf("To enable logging, %s", BuildInstructions())
	}
}

// Test that SearchError provides useful information
func TestSearchError(t *testing.T) {
	// Force a load attempt
	_ = Load()

	if !IsLoaded() {
		searchErr := SearchError()
		if searchErr == "" {
			t.Log("SearchError is empty (shim might have loaded)")
		} else {
			t.Logf("SearchError: %s", searchErr)
			// Should mention something useful
			if !strings.Contains(searchErr, "shim") && !strings.Contains(searchErr, "not found") && !strings.Contains(searchErr, "FFMPEG_SHIM_DIR") {
				t.Error("SearchError should contain useful diagnostic information")
			}
		}
	}
}
