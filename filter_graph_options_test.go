//go:build amd64 || arm64

package ffgo

import "testing"

func TestVideoFilterGraphWithStructuredOptions(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	graph, err := newVideoFilterGraphWithSpecs([]filterSpec{{
		name: "scale",
		options: []filterOption{
			{name: "w", value: "160"},
			{name: "h", value: "120"},
		},
	}}, 320, 240, PixelFormatYUV420P)
	if err != nil {
		t.Fatalf("create structured filter: %v", err)
	}
	defer graph.Close()
}
