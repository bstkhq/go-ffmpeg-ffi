//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"io"
	"testing"
)

func TestPublicErrorSentinels(t *testing.T) {
	closedErrors := []struct {
		name string
		err  error
	}{
		{name: "decoder", err: errDecoderClosed},
		{name: "encoder", err: ErrEncoderClosed},
		{name: "filter graph", err: ErrFilterGraphClosed},
		{name: "muxer", err: closedError("muxer")},
	}
	for _, test := range closedErrors {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, ErrClosed) {
				t.Fatalf("errors.Is(%v, ErrClosed) = false", test.err)
			}
		})
	}

	decoder := &Decoder{closed: true, videoStreamIdx: -1, audioStreamIdx: -1}
	if err := decoder.OpenVideoDecoder(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed OpenVideoDecoder error = %v, want ErrClosed", err)
	}
	if _, err := decoder.ReadFrame(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed ReadFrame error = %v, want ErrClosed", err)
	}

	decoder = &Decoder{videoStreamIdx: -1, audioStreamIdx: -1}
	if err := decoder.OpenVideoDecoder(); !errors.Is(err, ErrNoVideoStream) {
		t.Fatalf("OpenVideoDecoder error = %v, want ErrNoVideoStream", err)
	}
	if err := decoder.OpenAudioDecoder(); !errors.Is(err, ErrNoAudioStream) {
		t.Fatalf("OpenAudioDecoder error = %v, want ErrNoAudioStream", err)
	}
	if _, _, err := decoder.codecStateLocked(MediaTypeVideo); !errors.Is(err, ErrDecoderNotOpened) {
		t.Fatalf("codecStateLocked error = %v, want ErrDecoderNotOpened", err)
	}

	if !IsEOF(io.EOF) || !IsEOF(errors.Join(errors.New("context"), io.EOF)) {
		t.Fatal("IsEOF does not recognize standard wrapped io.EOF")
	}
}
