//go:build amd64 || arm64

package bindings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/abi"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/platform"
)

func TestLibrarySearchPaths(t *testing.T) {
	paths := LibrarySearchPaths()
	if runtime.GOOS == "android" {
		if len(paths) != 0 {
			t.Fatalf("Android should rely on its linker namespace, got desktop paths: %v", paths)
		}
		return
	}
	if len(paths) == 0 {
		t.Error("LibrarySearchPaths should return at least one path")
	}
}

func version(major, minor, patch uint32) uint32 {
	return major<<16 | minor<<8 | patch
}

func TestFindLibraryVersions(t *testing.T) {
	// This test may fail if FFmpeg is not installed
	// We just test that the function doesn't panic

	// Try to find avutil (most basic FFmpeg library)
	versions := []int{61, 60, 59, 58}
	_, err := FindLibrary("avutil", versions)

	// We don't fail if FFmpeg isn't installed - just log
	if err != nil {
		t.Logf("FFmpeg not found (expected if not installed): %v", err)
	}
}

func TestSelectCoreLibrariesPrefersNewestCompleteFamily(t *testing.T) {
	fake := newFakeDynamicLoader(map[string]uint32{
		platformLibraryName("avutil", 60):   version(60, 1, 0),
		platformLibraryName("avutil", 59):   version(59, 1, 0),
		platformLibraryName("avcodec", 61):  version(61, 1, 0),
		platformLibraryName("avformat", 61): version(61, 1, 0),
	})

	core, err := selectCoreLibraries(fake.loader())
	if err != nil {
		t.Fatal(err)
	}
	if core.layout.FFmpegMajor != 7 {
		t.Fatalf("selected FFmpeg %d, want 7", core.layout.FFmpegMajor)
	}
	if !fake.wasClosed(platformLibraryName("avutil", 60)) {
		t.Fatal("partial FFmpeg 8 family was not closed before selecting FFmpeg 7")
	}
}

func TestSelectCoreLibrariesRejectsMixedUnversionedTuple(t *testing.T) {
	fake := newFakeDynamicLoader(map[string]uint32{
		platformLibraryName("avutil", 0):   version(60, 1, 0),
		platformLibraryName("avcodec", 0):  version(61, 1, 0),
		platformLibraryName("avformat", 0): version(62, 1, 0),
	})

	_, err := selectCoreLibraries(fake.loader())
	if !errors.Is(err, abi.ErrUnsupported) {
		t.Fatalf("selectCoreLibraries() error = %v, want ErrUnsupported", err)
	}
	for name := range fake.versions {
		if !fake.wasClosed(name) {
			t.Fatalf("rejected library %s was not closed", name)
		}
	}
}

func TestSelectCoreLibrariesPrefersFirstCompleteSearchPath(t *testing.T) {
	bundled := filepath.Join("application", "ffmpeg-6")
	system := filepath.Join("system", "ffmpeg-9")
	fake := newFakeDynamicLoader(map[string]uint32{
		libraryPath(bundled, "avutil", 58):   version(58, 1, 0),
		libraryPath(bundled, "avcodec", 60):  version(60, 1, 0),
		libraryPath(bundled, "avformat", 60): version(60, 1, 0),
		libraryPath(system, "avutil", 61):    version(61, 1, 0),
		libraryPath(system, "avcodec", 63):   version(63, 1, 0),
		libraryPath(system, "avformat", 63):  version(63, 1, 0),
	})

	core, err := selectCoreLibrariesFromSearchPaths(fake.loader(), []string{bundled, system})
	if err != nil {
		t.Fatal(err)
	}
	if core.layout.FFmpegMajor != 6 {
		t.Fatalf("selected FFmpeg %d, want bundled FFmpeg 6", core.layout.FFmpegMajor)
	}
	if fake.wasOpened(libraryPath(system, "avutil", 61)) {
		t.Fatal("opened the lower-priority system FFmpeg before completing the bundled family")
	}
}

func TestSelectCoreLibrariesSkipsPartialSearchPath(t *testing.T) {
	partial := filepath.Join("application", "partial")
	fallback := filepath.Join("system", "ffmpeg-7")
	partialAVUtil := libraryPath(partial, "avutil", 58)
	fake := newFakeDynamicLoader(map[string]uint32{
		partialAVUtil:                         version(58, 1, 0),
		libraryPath(fallback, "avutil", 59):   version(59, 1, 0),
		libraryPath(fallback, "avcodec", 61):  version(61, 1, 0),
		libraryPath(fallback, "avformat", 61): version(61, 1, 0),
	})

	core, err := selectCoreLibrariesFromSearchPaths(fake.loader(), []string{partial, fallback})
	if err != nil {
		t.Fatal(err)
	}
	if core.layout.FFmpegMajor != 7 {
		t.Fatalf("selected FFmpeg %d, want complete fallback FFmpeg 7", core.layout.FFmpegMajor)
	}
	if !fake.wasClosed(partialAVUtil) {
		t.Fatal("partial higher-priority FFmpeg library was not closed")
	}
}

func TestLibraryCandidatesTryUnversionedLast(t *testing.T) {
	versioned := platformLibraryName("avutil", 60)
	unversioned := platformLibraryName("avutil", 0)
	candidates := libraryCandidates("avutil", []int{60}, true)
	if len(candidates) == 0 {
		t.Fatal("libraryCandidates returned no candidates")
	}
	seenUnversioned := false
	for _, candidate := range candidates {
		switch filepath.Base(candidate) {
		case unversioned:
			seenUnversioned = true
		case versioned:
			if seenUnversioned {
				t.Fatalf("versioned candidate %q appears after an unversioned candidate", candidate)
			}
		}
	}
	if !seenUnversioned {
		t.Fatal("libraryCandidates omitted the requested unversioned fallback")
	}
}

func TestValidateLibraryVersionFFmpeg8(t *testing.T) {
	layout, ok := abi.ForFFmpegMajor(8)
	if !ok {
		t.Fatal("FFmpeg 8 layout is missing")
	}
	if err := validateLibraryVersion(layout, "swresample", version(6, 1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := validateLibraryVersion(layout, "swresample", version(5, 1, 0)); !errors.Is(err, abi.ErrUnsupported) {
		t.Fatalf("wrong swresample major error = %v, want ErrUnsupported", err)
	}
}

func TestValidateLibraryVersionFFmpeg9(t *testing.T) {
	layout, ok := abi.ForFFmpegMajor(9)
	if !ok {
		t.Fatal("FFmpeg 9 layout is missing")
	}
	if err := validateLibraryVersion(layout, "swresample", version(7, 1, 101)); err != nil {
		t.Fatal(err)
	}
	if err := validateLibraryVersion(layout, "swresample", version(6, 1, 0)); !errors.Is(err, abi.ErrUnsupported) {
		t.Fatalf("wrong swresample major error = %v, want ErrUnsupported", err)
	}
}

func platformLibraryName(name string, major int) string {
	return filepath.Base(libraryCandidates(name, []int{major}, major == 0)[0])
}

func libraryPath(dir, name string, major int) string {
	return filepath.Join(dir, platform.FormatLibraryName(name, major))
}

type fakeDynamicLoader struct {
	versions   map[string]uint32
	handles    map[uintptr]string
	opened     map[string]bool
	closed     map[string]bool
	nextHandle uintptr
}

func newFakeDynamicLoader(versions map[string]uint32) *fakeDynamicLoader {
	return &fakeDynamicLoader{
		versions: versions,
		handles:  make(map[uintptr]string),
		opened:   make(map[string]bool),
		closed:   make(map[string]bool),
	}
}

func (f *fakeDynamicLoader) loader() dynamicLoader {
	return dynamicLoader{
		open: func(path string) (uintptr, error) {
			name := path
			if _, ok := f.versions[name]; !ok {
				name = filepath.Base(path)
			}
			if _, ok := f.versions[name]; !ok {
				return 0, fmt.Errorf("not found: %s", name)
			}
			f.nextHandle++
			f.handles[f.nextHandle] = name
			f.opened[name] = true
			return f.nextHandle, nil
		},
		close: func(handle uintptr) error {
			if name, ok := f.handles[handle]; ok {
				f.closed[name] = true
			}
			return nil
		},
		version: func(handle uintptr, _ string) (func() uint32, error) {
			name, ok := f.handles[handle]
			if !ok {
				return nil, fmt.Errorf("unknown handle %d", handle)
			}
			value := f.versions[name]
			return func() uint32 { return value }, nil
		},
	}
}

func (f *fakeDynamicLoader) wasClosed(name string) bool {
	return f.closed[name]
}

func (f *fakeDynamicLoader) wasOpened(name string) bool {
	return f.opened[name]
}

func TestErrNotLoaded(t *testing.T) {
	// Before loading, IsLoaded should be false
	if IsLoaded() {
		t.Error("IsLoaded should be false before Load is called")
	}
}

func TestConcurrentLoadAndStateReads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FFmpeg load test in short mode")
	}

	const readerCount = 16
	start := make(chan struct{})
	stop := make(chan struct{})
	var ready sync.WaitGroup
	var readers sync.WaitGroup
	ready.Add(readerCount)
	readers.Add(readerCount)

	readState := func() {
		_ = IsLoaded()
		_ = AVUtilVersion()
		_ = AVCodecVersion()
		_ = AVFormatVersion()
		_ = SWScaleVersion()
		_ = ABI()
		_ = LibAVUtil()
		_ = LibAVCodec()
		_ = LibAVFormat()
		_ = LibSWScale()
		_ = HasSWScale()
	}

	for range readerCount {
		go func() {
			defer readers.Done()
			<-start
			readState()
			ready.Done()
			for {
				select {
				case <-stop:
					readState()
					return
				default:
					readState()
				}
			}
		}()
	}

	close(start)
	ready.Wait()
	err := Load()
	close(stop)
	readers.Wait()

	if err != nil {
		if errors.Is(err, abi.ErrUnsupported) {
			t.Skipf("installed FFmpeg is outside the supported ABI matrix: %v", err)
		}
		t.Fatalf("loading FFmpeg: %v", err)
	}
	if !IsLoaded() || ABI().FFmpegMajor == 0 {
		t.Fatal("successful load did not publish the selected FFmpeg ABI")
	}
	if LibAVUtil() == 0 || LibAVCodec() == 0 || LibAVFormat() == 0 {
		t.Fatal("successful load did not publish the core library handles")
	}
}

// Integration test - only runs if FFmpeg is available
func TestLoadFFmpeg(t *testing.T) {
	if testing.Short() {
		t.Log("Skipping FFmpeg load test in short mode")
		return
	}

	err := Load()
	if err != nil {
		if errors.Is(err, abi.ErrUnsupported) {
			if IsLoaded() {
				t.Fatal("bindings reported loaded after rejecting the FFmpeg ABI")
			}
			t.Skipf("installed FFmpeg is outside the supported ABI matrix: %v", err)
		}
		t.Fatalf("loading FFmpeg: %v", err)
	}

	if !IsLoaded() {
		t.Error("IsLoaded should be true after successful Load")
	}

	// Verify we can get version
	ver := AVUtilVersion()
	if ver == 0 {
		t.Error("AVUtilVersion should return non-zero after Load")
	}

	t.Logf("FFmpeg loaded: avutil version %d.%d.%d",
		ver>>16, (ver>>8)&0xFF, ver&0xFF)
}

// TestConfiguredFFmpegFamily turns an explicit CI expectation into a check of
// the libraries PureGo actually loaded. It is inactive for users and normal
// test runs; CI sets FFMPEG_EXPECTED_MAJOR for its supported-version matrix.
func TestConfiguredFFmpegFamily(t *testing.T) {
	expectedText := os.Getenv("FFMPEG_EXPECTED_MAJOR")
	if expectedText == "" {
		t.Skip("FFMPEG_EXPECTED_MAJOR is not set")
	}
	expected, err := strconv.Atoi(expectedText)
	if err != nil || expected <= 0 {
		t.Fatalf("invalid FFMPEG_EXPECTED_MAJOR=%q", expectedText)
	}
	if err := Load(); err != nil {
		t.Fatalf("loading configured FFmpeg runtime: %v", err)
	}
	if got := ABI().FFmpegMajor; got != expected {
		t.Fatalf("loaded FFmpeg %d, want configured FFmpeg %d", got, expected)
	}
	avutil, avcodec, avformat := AVUtilVersion(), AVCodecVersion(), AVFormatVersion()
	t.Logf("loaded configured FFmpeg %d: avutil=%d.%d.%d avcodec=%d.%d.%d avformat=%d.%d.%d",
		expected,
		avutil>>16, (avutil>>8)&0xFF, avutil&0xFF,
		avcodec>>16, (avcodec>>8)&0xFF, avcodec&0xFF,
		avformat>>16, (avformat>>8)&0xFF, avformat&0xFF)
}
