//go:build !ios && !android && (amd64 || arm64)

package bindings

import (
	"errors"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/abi"
)

func TestLibrarySearchPaths(t *testing.T) {
	paths := LibrarySearchPaths()
	if len(paths) == 0 {
		t.Error("LibrarySearchPaths should return at least one path")
	}
}

func TestFindLibraryVersions(t *testing.T) {
	// This test may fail if FFmpeg is not installed
	// We just test that the function doesn't panic

	// Try to find avutil (most basic FFmpeg library)
	versions := []int{61, 60, 59, 58, 57, 56}
	_, err := FindLibrary("avutil", versions)

	// We don't fail if FFmpeg isn't installed - just log
	if err != nil {
		t.Logf("FFmpeg not found (expected if not installed): %v", err)
	}
}

func TestErrNotLoaded(t *testing.T) {
	// Before loading, IsLoaded should be false
	if IsLoaded() {
		t.Error("IsLoaded should be false before Load is called")
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
