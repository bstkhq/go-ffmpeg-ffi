package ffmpeg_test

import (
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi"
)

func TestDefaultPackageName(t *testing.T) {
	if ffmpeg.ErrClosed == nil {
		t.Fatal("ffmpeg.ErrClosed must be defined")
	}
}
