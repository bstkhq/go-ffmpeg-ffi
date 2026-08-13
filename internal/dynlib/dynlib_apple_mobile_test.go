//go:build ios && (amd64 || arm64)

package dynlib

import "testing"

func TestProcessImageHandleDoesNotClose(t *testing.T) {
	handle, err := Open(ProcessImage)
	if err != nil {
		t.Fatal(err)
	}
	if handle == 0 {
		t.Fatal("process image returned a zero handle")
	}
	if err := Close(handle); err != nil {
		t.Fatalf("close process pseudo-handle: %v", err)
	}
}
