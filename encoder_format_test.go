//go:build !ios && (amd64 || arm64)

package ffgo

import "testing"

func TestGuessFormatFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "video.MP4", want: "mp4"},
		{path: "video.MkV", want: "matroska"},
		{path: `C:\exports\video.MOV`, want: "mov"},
		{path: "/exports/50%discount/video.mp4", want: "mp4"},
		{path: `C:\exports\frame-%04d\video.webm`, want: "webm"},
		{path: "/exports/frame-%04d.PNG", want: "image2"},
		{path: "frame-%d", want: "image2"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := guessFormatFromPath(test.path); got != test.want {
				t.Fatalf("guessFormatFromPath(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}
