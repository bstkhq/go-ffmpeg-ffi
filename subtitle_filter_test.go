//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestSubtitleFilterSpecPreservesUserValues(t *testing.T) {
	subtitlePath := filepath.Join("Movie, The [2020] 'cut':1", "sub, title's [final].srt")
	fontsDir := filepath.Join("fonts, selected", "reader's [fonts]:1")
	spec := subtitleFilterSpec(subtitlePath, 1920, 1080, &SubtitleRendererOptions{
		FontName:     "Reader's Serif:null[alt],null",
		FontSize:     24,
		CharEncoding: "UTF-8",
		FontsDir:     fontsDir,
		OriginalSize: true,
	})
	if spec.name != "subtitles" {
		t.Fatalf("filter name = %q, want subtitles", spec.name)
	}
	if spec.args != "" {
		t.Fatalf("subtitle user values unexpectedly serialized as filter arguments: %q", spec.args)
	}

	wantSubtitlePath, _ := filepath.Abs(subtitlePath)
	wantFontsDir, _ := filepath.Abs(fontsDir)
	want := map[string]string{
		"filename":      wantSubtitlePath,
		"original_size": "1920x1080",
		"charenc":       "UTF-8",
		"fontsdir":      wantFontsDir,
		"force_style":   "FontName=Reader's Serif:null[alt],null,FontSize=24",
	}
	got := make(map[string]string, len(spec.options))
	for _, option := range spec.options {
		got[option.name] = option.value
	}
	if len(got) != len(want) {
		t.Fatalf("filter options = %#v, want %#v", got, want)
	}
	for name, value := range want {
		if got[name] != value {
			t.Fatalf("filter option %q = %q, want %q", name, got[name], value)
		}
	}
}

func TestSubtitleRendererAcceptsSpecialCharacters(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	dir := filepath.Join(t.TempDir(), "Movie, The [2020] 'cut':1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	srtPath := filepath.Join(dir, "sub, title's [final].srt")
	const srt = "1\n00:00:00,000 --> 00:00:01,000\nTest\n"
	if err := os.WriteFile(srtPath, []byte(srt), 0o644); err != nil {
		t.Fatal(err)
	}
	fontsDir := filepath.Join(dir, "fonts, reader's [fonts]:1")
	if err := os.MkdirAll(fontsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	renderer, err := NewSubtitleRendererWithOptions(srtPath, 320, 240, &SubtitleRendererOptions{
		FontName:     "DejaVu Serif':null[alt],null",
		FontSize:     24,
		CharEncoding: "UTF-8",
		FontsDir:     fontsDir,
		OriginalSize: true,
	})
	if err != nil {
		if isFilterUnavailable(err, "subtitles") {
			t.Skipf("subtitles filter unavailable: %v", err)
		}
		t.Fatalf("create renderer: %v", err)
	}
	defer renderer.Close()

	frame := FrameAlloc()
	if frame.IsNil() {
		t.Fatal("allocate frame")
	}
	defer func() { _ = FrameFree(&frame) }()
	avutil.SetFrameWidth(frame.ptr, 320)
	avutil.SetFrameHeight(frame.ptr, 240)
	avutil.SetFrameFormat(frame.ptr, int32(PixelFormatYUV420P))
	avutil.SetFramePTS(frame.ptr, 0)
	if err := avutil.FrameGetBufferErr(frame.ptr, 0); err != nil {
		t.Fatal(err)
	}

	out, err := renderer.Render(frame)
	if err != nil {
		t.Fatalf("render frame: %v", err)
	}
	defer func() { _ = FrameFree(&out) }()
	if out.IsNil() {
		t.Fatal("renderer returned a nil frame")
	}
}

func isFilterUnavailable(err error, name string) bool {
	return err != nil && strings.Contains(err.Error(), `filter "`+name+`" not found`)
}
