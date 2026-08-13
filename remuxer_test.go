//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
)

func TestRemuxerWritePacketPropagatesPacketRefError(t *testing.T) {
	want := errors.New("packet ref failed")
	packetStorage := byte(0)
	r := &Remuxer{
		streamMap:       map[int]int{0: 0},
		inputTimeBases:  make(map[int]Rational),
		outputTimeBases: make(map[int]Rational),
		headerWritten:   true,
		packetRef: func(avcodec.Packet, avcodec.Packet) error {
			return want
		},
	}

	err := r.WritePacket(avcodec.Packet(unsafe.Pointer(&packetStorage)), 0)
	if !errors.Is(err, want) {
		t.Fatalf("WritePacket error = %v, want %v", err, want)
	}
}

func TestRemuxerWritePacketRejectsNilPacket(t *testing.T) {
	r := &Remuxer{streamMap: map[int]int{0: 0}}
	if err := r.WritePacket(nil, 0); err == nil {
		t.Fatal("WritePacket(nil) succeeded")
	}
}

func TestRemuxerStreamMappingReturnsSnapshot(t *testing.T) {
	r := &Remuxer{streamMap: map[int]int{2: 0, 5: 1}}

	mapping := r.StreamMapping()
	mapping[2] = 99
	delete(mapping, 5)

	got := r.StreamMapping()
	if got[2] != 0 || got[5] != 1 || len(got) != 2 {
		t.Fatalf("internal stream mapping was mutated: %v", got)
	}
}

func TestRemuxerCopyDecoderStreamsRejectsClosedDecoder(t *testing.T) {
	r := &Remuxer{}
	d := &Decoder{closed: true}
	if err := r.copyDecoderStreams(d, nil); !errors.Is(err, errDecoderClosed) {
		t.Fatalf("copyDecoderStreams error = %v, want %v", err, errDecoderClosed)
	}
}

func TestNewRemuxerRejectsDuplicateStreams(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}
	decoder, err := NewDecoder(createTestVideo(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	outputPath := filepath.Join(t.TempDir(), "duplicate-streams.mkv")
	remuxer, err := NewRemuxer(outputPath, decoder, &RemuxerConfig{InputStreams: []int{0, 0}})
	if remuxer != nil {
		_ = remuxer.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "duplicate input stream index 0") {
		t.Fatalf("NewRemuxer error = %v, want duplicate stream rejection", err)
	}
}

func TestNewRemuxerConcurrentDecoderClose(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	inputPath := createTestVideo(t)
	outputDir := t.TempDir()
	const attempts = 8
	for attempt := 0; attempt < attempts; attempt++ {
		decoder, err := NewDecoder(inputPath, nil)
		if err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var remuxer *Remuxer
		var remuxErr, closeErr error
		go func() {
			defer wg.Done()
			<-start
			outputPath := filepath.Join(outputDir, strconv.Itoa(attempt)+".mkv")
			remuxer, remuxErr = NewRemuxer(outputPath, decoder, nil)
		}()
		go func() {
			defer wg.Done()
			<-start
			closeErr = decoder.Close()
		}()
		close(start)
		wg.Wait()

		if remuxer != nil {
			_ = remuxer.Close()
		}
		if closeErr != nil {
			t.Fatalf("attempt %d: Decoder.Close: %v", attempt, closeErr)
		}
		if remuxErr != nil && !errors.Is(remuxErr, errDecoderClosed) {
			t.Fatalf("attempt %d: NewRemuxer: %v", attempt, remuxErr)
		}
	}
}
