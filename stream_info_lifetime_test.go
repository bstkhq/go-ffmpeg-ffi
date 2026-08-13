//go:build amd64 || arm64

package ffgo

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
)

func TestStreamInfoOwnsCodecParametersAfterDecoderClose(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	decoder, err := NewDecoder(createTestVideo(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if decoder != nil {
			_ = decoder.Close()
		}
	})

	info := decoder.VideoStream()
	if info == nil {
		t.Fatal("decoder has no video stream")
	}
	parameters := info.CodecParameters()
	if parameters == nil {
		t.Fatal("video stream has no codec parameters")
	}
	nativeParameters := avformat.GetStreamCodecPar(avformat.GetStream(decoder.formatCtx, info.Index))
	if parameters == nativeParameters {
		t.Fatal("StreamInfo retained codec parameters owned by AVFormatContext")
	}

	wantCodecID := avformat.GetCodecParCodecID(parameters)
	wantWidth := avformat.GetCodecParWidth(parameters)
	wantHeight := avformat.GetCodecParHeight(parameters)
	wantExtradata := avformat.GetCodecParExtradata(parameters)
	if len(wantExtradata) == 0 {
		t.Fatal("test stream has no codec extradata")
	}

	infoCopy := *info
	info = nil
	if err := decoder.Close(); err != nil {
		t.Fatal(err)
	}
	decoder = nil
	runtime.GC()
	runtime.Gosched()

	parameters = infoCopy.CodecParameters()
	if parameters == nil {
		t.Fatal("copied StreamInfo lost codec parameters after Decoder.Close")
	}
	if got := avformat.GetCodecParCodecID(parameters); got != wantCodecID {
		t.Fatalf("codec ID after Decoder.Close = %d, want %d", got, wantCodecID)
	}
	if got := avformat.GetCodecParWidth(parameters); got != wantWidth {
		t.Fatalf("width after Decoder.Close = %d, want %d", got, wantWidth)
	}
	if got := avformat.GetCodecParHeight(parameters); got != wantHeight {
		t.Fatalf("height after Decoder.Close = %d, want %d", got, wantHeight)
	}
	if got := avformat.GetCodecParExtradata(parameters); !bytes.Equal(got, wantExtradata) {
		t.Fatal("codec extradata changed after Decoder.Close")
	}

	codec := avcodec.FindDecoder(wantCodecID)
	if codec == nil {
		t.Fatal("decoder for copied codec parameters is unavailable")
	}
	codecContext := avcodec.AllocContext3(codec)
	if codecContext == nil {
		t.Fatal("failed to allocate codec context")
	}
	defer avcodec.FreeContext(&codecContext)
	if err := avcodec.ParametersToContext(codecContext, parameters); err != nil {
		t.Fatalf("codec parameters are unusable after Decoder.Close: %v", err)
	}
	runtime.KeepAlive(infoCopy)
}

func TestStreamCopySourceRetainsStreamInfoParameters(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	decoder, err := NewDecoder(createTestVideo(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if decoder != nil {
			_ = decoder.Close()
		}
	})

	info := decoder.VideoStream()
	if info == nil {
		t.Fatal("decoder has no video stream")
	}
	source := NewStreamCopySource(info, nil)
	wantCodecID := info.CodecID

	if err := decoder.Close(); err != nil {
		t.Fatal(err)
	}
	decoder = nil
	info = nil
	runtime.GC()
	runtime.Gosched()

	copy := avcodec.ParametersAlloc()
	if copy == nil {
		t.Fatal("failed to allocate destination codec parameters")
	}
	defer avcodec.ParametersFree(&copy)
	if err := avcodec.ParametersCopy(copy, source.VideoParams); err != nil {
		t.Fatalf("stream-copy source parameters are unusable after Decoder.Close: %v", err)
	}
	if got := avformat.GetCodecParCodecID(copy); got != wantCodecID {
		t.Fatalf("copied codec ID = %d, want %d", got, wantCodecID)
	}
	runtime.KeepAlive(source)
}
