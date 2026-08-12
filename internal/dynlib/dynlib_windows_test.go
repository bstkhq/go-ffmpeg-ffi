//go:build windows && (amd64 || arm64)

package dynlib

import (
	"testing"

	"github.com/ebitengine/purego"
)

func TestWindowsHandleSupportsPureGoSymbols(t *testing.T) {
	handle, err := Open("kernel32.dll")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := Close(handle); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	var getCurrentProcessID func() uint32
	purego.RegisterLibFunc(&getCurrentProcessID, handle, "GetCurrentProcessId")
	if pid := getCurrentProcessID(); pid == 0 {
		t.Fatal("GetCurrentProcessId returned zero")
	}
}
