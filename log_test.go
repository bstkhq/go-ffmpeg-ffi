//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"testing"

	"github.com/ebitengine/purego"
)

func TestLogCallbackRecoversPanic(t *testing.T) {
	wantErr := errors.New("log callback panic")
	logCallbackMu.Lock()
	previous := logCallback
	logCallback = func(LogLevel, string) { panic(wantErr) }
	logCallbackErr = nil
	logCallbackMu.Unlock()
	t.Cleanup(func() {
		logCallbackMu.Lock()
		logCallback = previous
		logCallbackErr = nil
		logCallbackMu.Unlock()
	})

	logCallbackTrampoline(purego.CDecl{}, 0, int32(LogError), nil)
	if err := TakeLogCallbackError(); !errors.Is(err, wantErr) {
		t.Fatalf("TakeLogCallbackError() = %v, want panic cause %v", err, wantErr)
	}
	if err := TakeLogCallbackError(); err != nil {
		t.Fatalf("second TakeLogCallbackError() = %v, want nil", err)
	}
}
