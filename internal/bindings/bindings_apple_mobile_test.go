//go:build ios && (amd64 || arm64)

package bindings

import (
	"slices"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/dynlib"
)

func TestIOSLibraryCandidatesIncludeSignedFrameworksAndProcessImage(t *testing.T) {
	candidates := libraryCandidates("avcodec", []int{63}, false)
	for _, want := range []string{
		"@rpath/libavcodec.framework/libavcodec",
		"@rpath/avcodec.framework/avcodec",
		dynlib.ProcessImage,
	} {
		if !slices.Contains(candidates, want) {
			t.Errorf("iOS candidates omit %q: %v", want, candidates)
		}
	}
	if got := candidates[len(candidates)-1]; got != dynlib.ProcessImage {
		t.Fatalf("last iOS fallback = %q, want process image", got)
	}
}
