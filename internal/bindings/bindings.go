//go:build amd64 || arm64

// Package bindings handles loading FFmpeg shared libraries and registering
// function bindings using purego.
package bindings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/abi"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/dynlib"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/platform"
	"github.com/ebitengine/purego"
)

// ErrNotLoaded is returned when FFmpeg functions are called before Load().
var ErrNotLoaded = errors.New("ffmpeg: FFmpeg libraries not loaded; call ffmpeg.Init() first")

// ErrLibraryNotFound is returned when a required FFmpeg library cannot be found.
var ErrLibraryNotFound = errors.New("ffmpeg: FFmpeg library not found")

type loadedLibrary struct {
	handle uintptr
	path   string
}

type coreLibraries struct {
	avutil, avcodec, avformat loadedLibrary
	avutilVersion             func() uint32
	avcodecVersion            func() uint32
	avformatVersion           func() uint32
	layout                    abi.Layout
}

type dynamicLoader struct {
	open    func(string) (uintptr, error)
	close   func(uintptr) error
	version func(uintptr, string) (func() uint32, error)
}

var systemLoader = dynamicLoader{
	open:    tryOpen,
	close:   dynlib.Close,
	version: registerVersionFunction,
}

// Library handles and selected runtime state.
var (
	libAVUtil   uintptr
	libAVCodec  uintptr
	libAVFormat uintptr
	libSWScale  uintptr

	// loaded publishes the handles, version functions, and ABI written by
	// doLoad. Those fields are immutable after the release store succeeds.
	loaded     atomic.Bool
	loadOnce   sync.Once
	loadErr    error
	currentABI abi.Layout
)

// Version function bindings.
var (
	avutilVersion   func() uint32
	avcodecVersion  func() uint32
	avformatVersion func() uint32
	swscaleVersion  func() uint32
)

// IsLoaded returns true if FFmpeg libraries have been successfully loaded.
func IsLoaded() bool {
	return loaded.Load()
}

// Load loads a coherent set of FFmpeg libraries and registers their version
// functions. It is safe to call multiple times; subsequent calls are no-ops.
func Load() error {
	loadOnce.Do(func() {
		loadErr = doLoad()
		if loadErr == nil {
			loaded.Store(true)
		}
	})
	return loadErr
}

func doLoad() error {
	core, err := selectCoreLibraries(systemLoader)
	if err != nil {
		return err
	}

	libAVUtil = core.avutil.handle
	libAVCodec = core.avcodec.handle
	libAVFormat = core.avformat.handle
	avutilVersion = core.avutilVersion
	avcodecVersion = core.avcodecVersion
	avformatVersion = core.avformatVersion
	currentABI = core.layout

	// swscale is optional, but if present it must match the selected family.
	swscale, err := openLibrary(systemLoader, "swscale", []int{currentABI.SWScaleMajor}, true)
	if err == nil {
		versionFn, versionErr := systemLoader.version(swscale.handle, "swscale_version")
		if versionErr != nil {
			_ = systemLoader.close(swscale.handle)
			closeCoreLibraries(systemLoader, core)
			clearCoreState()
			return fmt.Errorf("loading libswscale: %w", versionErr)
		}
		if versionErr = validateLibraryVersion(currentABI, "swscale", versionFn()); versionErr != nil {
			_ = systemLoader.close(swscale.handle)
			closeCoreLibraries(systemLoader, core)
			clearCoreState()
			return versionErr
		}
		libSWScale = swscale.handle
		swscaleVersion = versionFn
	}

	return nil
}

func selectCoreLibraries(loader dynamicLoader) (coreLibraries, error) {
	var failures []string
	for _, layout := range abi.Supported() {
		core, err := openCoreFamily(loader, layout, false)
		if err == nil {
			return core, nil
		}
		failures = append(failures, fmt.Sprintf("FFmpeg %d: %v", layout.FFmpegMajor, err))
	}

	// Some custom installations expose only unversioned names. Probe them once,
	// then accept them only if the complete runtime tuple is supported.
	core, err := openCoreFamily(loader, abi.Layout{}, true)
	if err == nil {
		return core, nil
	}
	if errors.Is(err, abi.ErrUnsupported) {
		return coreLibraries{}, err
	}
	failures = append(failures, fmt.Sprintf("unversioned: %v", err))

	return coreLibraries{}, fmt.Errorf(
		"%w: no complete FFmpeg 6, 7, 8, or 9 core set found (%s)",
		ErrLibraryNotFound, strings.Join(failures, "; "),
	)
}

func openCoreFamily(loader dynamicLoader, wanted abi.Layout, unversioned bool) (coreLibraries, error) {
	versions := func(major int) []int {
		if unversioned {
			return nil
		}
		return []int{major}
	}

	var core coreLibraries
	var err error
	core.avutil, err = openLibrary(loader, "avutil", versions(wanted.AVUtilMajor), unversioned)
	if err != nil {
		return coreLibraries{}, fmt.Errorf("loading libavutil: %w", err)
	}
	defer func() {
		if err != nil {
			closeCoreLibraries(loader, core)
		}
	}()

	core.avcodec, err = openLibrary(loader, "avcodec", versions(wanted.AVCodecMajor), unversioned)
	if err != nil {
		return coreLibraries{}, fmt.Errorf("loading libavcodec: %w", err)
	}
	core.avformat, err = openLibrary(loader, "avformat", versions(wanted.AVFormatMajor), unversioned)
	if err != nil {
		return coreLibraries{}, fmt.Errorf("loading libavformat: %w", err)
	}

	core.avutilVersion, err = loader.version(core.avutil.handle, "avutil_version")
	if err != nil {
		return coreLibraries{}, fmt.Errorf("binding avutil_version: %w", err)
	}
	core.avcodecVersion, err = loader.version(core.avcodec.handle, "avcodec_version")
	if err != nil {
		return coreLibraries{}, fmt.Errorf("binding avcodec_version: %w", err)
	}
	core.avformatVersion, err = loader.version(core.avformat.handle, "avformat_version")
	if err != nil {
		return coreLibraries{}, fmt.Errorf("binding avformat_version: %w", err)
	}

	core.layout, err = abi.Detect(
		core.avutilVersion(),
		core.avcodecVersion(),
		core.avformatVersion(),
	)
	if err != nil {
		return coreLibraries{}, err
	}
	if !unversioned && core.layout.FFmpegMajor != wanted.FFmpegMajor {
		err = fmt.Errorf(
			"%w: FFmpeg %d library names resolved to FFmpeg %d",
			abi.ErrUnsupported, wanted.FFmpegMajor, core.layout.FFmpegMajor,
		)
		return coreLibraries{}, err
	}

	return core, nil
}

func closeCoreLibraries(loader dynamicLoader, core coreLibraries) {
	for _, lib := range []loadedLibrary{core.avformat, core.avcodec, core.avutil} {
		if lib.handle != 0 {
			_ = loader.close(lib.handle)
		}
	}
}

func clearCoreState() {
	libAVUtil = 0
	libAVCodec = 0
	libAVFormat = 0
	avutilVersion = nil
	avcodecVersion = nil
	avformatVersion = nil
	currentABI = abi.Layout{}
}

// openLibrary tries all versioned candidates before considering an
// unversioned name. This prevents an unrelated unversioned development symlink
// from pre-empting a supported version in a later search path.
func openLibrary(loader dynamicLoader, name string, versions []int, includeUnversioned bool) (loadedLibrary, error) {
	candidates := libraryCandidates(name, versions, includeUnversioned)
	for _, candidate := range candidates {
		handle, err := loader.open(candidate)
		if err == nil {
			if candidate == dynlib.ProcessImage {
				// RTLD_DEFAULT itself always exists. Treat it as a match only when
				// this specific FFmpeg library is actually linked or already loaded;
				// otherwise an absent optional library such as swscale would look
				// present and turn initialization into a false failure.
				if _, versionErr := loader.version(handle, name+"_version"); versionErr != nil {
					_ = loader.close(handle)
					continue
				}
			}
			return loadedLibrary{handle: handle, path: candidate}, nil
		}
	}
	return loadedLibrary{}, fmt.Errorf(
		"%w: %s (tried %d candidates)", ErrLibraryNotFound, name, len(candidates),
	)
}

func libraryCandidates(name string, versions []int, includeUnversioned bool) []string {
	paths := LibrarySearchPaths()
	seen := make(map[string]struct{})
	candidates := make([]string, 0, (len(paths)+1)*(len(versions)+1))
	appendCandidate := func(candidate string) {
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	for _, version := range versions {
		name := platform.FormatLibraryName(name, version)
		for _, searchPath := range paths {
			appendCandidate(filepath.Join(searchPath, name))
		}
		appendCandidate(name)
	}
	if includeUnversioned {
		name := platform.FormatLibraryName(name, 0)
		for _, searchPath := range paths {
			appendCandidate(filepath.Join(searchPath, name))
		}
		appendCandidate(name)
	}
	if runtime.GOOS == "ios" {
		// App Store applications may embed signed dynamic frameworks, but must
		// not rely on desktop library locations or executable code downloaded at
		// runtime. Accept the two framework naming conventions used by FFmpeg
		// distributors, then fall back to FFmpeg symbols already linked into the
		// process image (for example by a static XCFramework).
		for _, prefix := range []string{"@rpath", "@executable_path/Frameworks"} {
			appendCandidate(filepath.Join(prefix, "lib"+name+".framework", "lib"+name))
			appendCandidate(filepath.Join(prefix, name+".framework", name))
		}
		appendCandidate(dynlib.ProcessImage)
	}
	return candidates
}

// tryOpen delegates shared-library loading to the current operating system.
func tryOpen(path string) (uintptr, error) {
	return dynlib.Open(path)
}

func registerVersionFunction(handle uintptr, name string) (fn func() uint32, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fn = nil
			err = fmt.Errorf("required symbol %s is unavailable: %v", name, recovered)
		}
	}()
	purego.RegisterLibFunc(&fn, handle, name)
	return fn, nil
}

// FindLibrary searches for a library file and returns its full path. It does
// not load the library and is intended for diagnostics.
func FindLibrary(name string, versions []int) (string, error) {
	for _, candidate := range libraryCandidates(name, versions, true) {
		if filepath.IsAbs(candidate) {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("%w: %s", ErrLibraryNotFound, name)
}

// LibrarySearchPaths returns platform-specific library search paths.
func LibrarySearchPaths() []string {
	var paths []string

	switch runtime.GOOS {
	case "linux":
		if ldPath := os.Getenv("LD_LIBRARY_PATH"); ldPath != "" {
			paths = append(paths, filepath.SplitList(ldPath)...)
		}
		paths = append(paths,
			"/usr/lib/x86_64-linux-gnu",
			"/usr/lib/aarch64-linux-gnu",
			"/usr/local/lib",
			"/usr/lib",
			"/lib/x86_64-linux-gnu",
			"/lib/aarch64-linux-gnu",
			"/lib",
		)
	case "darwin":
		if dyldPath := os.Getenv("DYLD_LIBRARY_PATH"); dyldPath != "" {
			paths = append(paths, filepath.SplitList(dyldPath)...)
		}
		paths = append(paths,
			"/opt/homebrew/lib",
			"/usr/local/lib",
			"/opt/homebrew/opt/ffmpeg/lib",
			"/usr/local/opt/ffmpeg/lib",
		)
	case "ios":
		// Embedded frameworks normally resolve through @rpath. Also try the
		// concrete Frameworks directory for hosts that do not add that rpath.
		if exe, err := os.Executable(); err == nil {
			paths = append(paths, filepath.Join(filepath.Dir(exe), "Frameworks"))
		}
	case "windows":
		if winPath := os.Getenv("PATH"); winPath != "" {
			paths = append(paths, filepath.SplitList(winPath)...)
		}
		if exe, err := os.Executable(); err == nil {
			paths = append(paths, filepath.Dir(exe))
		}
		paths = append(paths,
			"C:\\ffmpeg\\bin",
			"C:\\Program Files\\ffmpeg\\bin",
		)
	case "freebsd":
		if ldPath := os.Getenv("LD_LIBRARY_PATH"); ldPath != "" {
			paths = append(paths, filepath.SplitList(ldPath)...)
		}
		paths = append(paths, "/usr/local/lib", "/usr/lib")
	case "android":
		// Android application native libraries are resolved by soname inside the
		// app linker namespace. Unqualified candidates are appended by
		// libraryCandidates, so desktop filesystem paths must not be searched.
	}

	return paths
}

// AVUtilVersion returns the avutil library version, or 0 before Load succeeds.
func AVUtilVersion() uint32 {
	if !loaded.Load() || avutilVersion == nil {
		return 0
	}
	return avutilVersion()
}

// AVCodecVersion returns the avcodec library version, or 0 before Load succeeds.
func AVCodecVersion() uint32 {
	if !loaded.Load() || avcodecVersion == nil {
		return 0
	}
	return avcodecVersion()
}

// AVFormatVersion returns the avformat library version, or 0 before Load succeeds.
func AVFormatVersion() uint32 {
	if !loaded.Load() || avformatVersion == nil {
		return 0
	}
	return avformatVersion()
}

// SWScaleVersion returns the swscale version, or 0 when it is unavailable.
func SWScaleVersion() uint32 {
	if !loaded.Load() || swscaleVersion == nil {
		return 0
	}
	return swscaleVersion()
}

// ABI returns the layout selected from the loaded FFmpeg libraries.
func ABI() abi.Layout {
	if !loaded.Load() {
		return abi.Layout{}
	}
	return currentABI
}

// ValidateLibraryVersion verifies that an optional FFmpeg library belongs to
// the same release family as the already loaded core libraries.
func ValidateLibraryVersion(name string, version uint32) error {
	if err := Load(); err != nil {
		return err
	}
	return validateLibraryVersion(currentABI, name, version)
}

func validateLibraryVersion(layout abi.Layout, name string, version uint32) error {
	expected, ok := layout.LibraryMajor(name)
	if !ok {
		return fmt.Errorf("ffmpeg: unknown FFmpeg library %q", name)
	}
	actual := int(version >> 16)
	if actual != expected {
		return fmt.Errorf(
			"%w: lib%s %d is incompatible with FFmpeg %d (expected %d)",
			abi.ErrUnsupported, name, actual, layout.FFmpegMajor, expected,
		)
	}
	return nil
}

// LibAVUtil returns the avutil library handle.
func LibAVUtil() uintptr {
	if !loaded.Load() {
		return 0
	}
	return libAVUtil
}

// LibAVCodec returns the avcodec library handle.
func LibAVCodec() uintptr {
	if !loaded.Load() {
		return 0
	}
	return libAVCodec
}

// LibAVFormat returns the avformat library handle.
func LibAVFormat() uintptr {
	if !loaded.Load() {
		return 0
	}
	return libAVFormat
}

// LibSWScale returns the swscale library handle.
func LibSWScale() uintptr {
	if !loaded.Load() {
		return 0
	}
	return libSWScale
}

// HasSWScale reports whether a compatible swscale library is available.
func HasSWScale() bool { return loaded.Load() && libSWScale != 0 }

// LoadOptionalLibrary loads the major of an optional library selected by the
// already loaded core ABI.
func LoadOptionalLibrary(name string) (uintptr, error) {
	if err := Load(); err != nil {
		return 0, err
	}
	expected, ok := currentABI.LibraryMajor(name)
	if !ok {
		return 0, fmt.Errorf("ffmpeg: unknown FFmpeg library %q", name)
	}
	library, err := openLibrary(systemLoader, name, []int{expected}, true)
	if err != nil {
		return 0, err
	}
	versionFn, err := systemLoader.version(library.handle, name+"_version")
	if err != nil {
		_ = systemLoader.close(library.handle)
		return 0, err
	}
	if err := validateLibraryVersion(currentABI, name, versionFn()); err != nil {
		_ = systemLoader.close(library.handle)
		return 0, err
	}
	return library.handle, nil
}
