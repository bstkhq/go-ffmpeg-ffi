//go:build !ios && (amd64 || arm64)

package dynlib

import "testing"

func TestOpenMissingLibrary(t *testing.T) {
	handle, err := Open("ffgo-library-that-does-not-exist-7b1fb3f2")
	if err == nil {
		if handle != 0 {
			_ = Close(handle)
		}
		t.Fatal("Open unexpectedly loaded a nonexistent library")
	}
	if handle != 0 {
		t.Fatalf("failed Open returned handle %#x", handle)
	}
}
