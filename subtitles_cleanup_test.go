//go:build amd64 || arm64

package ffmpeg

import (
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
	"github.com/bstkhq/go-ffmpeg-ffi/internal/bindings"
)

func TestSubtitleDecoderCloseClearsSubtitleAllocation(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	subtitle := avutil.Malloc(bindings.ABI().Subtitle.Size)
	if subtitle == nil {
		t.Fatal("failed to allocate subtitle storage")
	}
	clearSubtitle(subtitle)

	decoder := &SubtitleDecoder{subtitle: subtitle}
	if err := decoder.Close(); err != nil {
		t.Fatal(err)
	}
	if decoder.subtitle != nil {
		t.Fatal("Close retained freed subtitle storage")
	}
	if err := decoder.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
