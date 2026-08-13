//go:build windows && (amd64 || arm64)

package avutil

import "testing"

func TestWindowsAVErrorErrnoValuesUseCRuntimeABI(t *testing.T) {
	if AVERROR_EAGAIN != -11 {
		t.Fatalf("AVERROR_EAGAIN = %d, want -11", AVERROR_EAGAIN)
	}
	if AVERROR_EINVAL != -22 {
		t.Fatalf("AVERROR_EINVAL = %d, want -22", AVERROR_EINVAL)
	}
	if AVERROR_ENOMEM != -12 {
		t.Fatalf("AVERROR_ENOMEM = %d, want -12", AVERROR_ENOMEM)
	}
	if err := NewError(-11, "receive"); !IsAgain(err) {
		t.Fatalf("IsAgain(%v) = false, want true", err)
	}
}
