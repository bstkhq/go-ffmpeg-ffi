//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"sync"
	"testing"
	"unsafe"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestBitstreamFilterStateDrainsAndRetriesSameInput(t *testing.T) {
	packetStorage := byte(0)
	input := avcodec.Packet(unsafe.Pointer(&packetStorage))
	sendCalls := 0
	receiveResults := []error{
		nil,
		avutil.NewError(avutil.AVERROR_EAGAIN, "receive"),
		nil,
		avutil.NewError(avutil.AVERROR_EAGAIN, "receive"),
	}
	writes := 0
	state := bitstreamFilterState{
		sendPacket: func(_ bsfContext, got avcodec.Packet) error {
			sendCalls++
			if got != input {
				t.Fatalf("sent packet = %p, want %p", got, input)
			}
			if sendCalls == 1 {
				return avutil.NewError(avutil.AVERROR_EAGAIN, "send")
			}
			return nil
		},
		receivePacket: func(bsfContext, avcodec.Packet) error {
			result := receiveResults[0]
			receiveResults = receiveResults[1:]
			return result
		},
	}

	err := state.filter(nil, input, nil, func(avcodec.Packet) error {
		writes++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sendCalls != 2 || writes != 2 {
		t.Fatalf("sends=%d writes=%d, want 2 and 2", sendCalls, writes)
	}
}

func TestBitstreamFilterStateFlushesOnceAndDrainsToEOF(t *testing.T) {
	receiveResults := []error{
		nil,
		nil,
		avutil.NewError(avutil.AVERROR_EOF, "receive"),
	}
	sendCalls := 0
	writes := 0
	state := bitstreamFilterState{
		sendPacket: func(_ bsfContext, packet avcodec.Packet) error {
			sendCalls++
			if packet != nil {
				t.Fatalf("flush sent non-nil packet %p", packet)
			}
			return nil
		},
		receivePacket: func(bsfContext, avcodec.Packet) error {
			result := receiveResults[0]
			receiveResults = receiveResults[1:]
			return result
		},
	}
	write := func(avcodec.Packet) error {
		writes++
		return nil
	}

	if err := state.flush(nil, nil, write); err != nil {
		t.Fatal(err)
	}
	if err := state.flush(nil, nil, write); err != nil {
		t.Fatal(err)
	}
	if sendCalls != 1 || writes != 2 || !state.drained {
		t.Fatalf("sends=%d writes=%d drained=%v", sendCalls, writes, state.drained)
	}
}

func TestBitstreamFilterStateRejectsImpossibleDoubleEAGAIN(t *testing.T) {
	packetStorage := byte(0)
	state := bitstreamFilterState{
		sendPacket: func(bsfContext, avcodec.Packet) error {
			return avutil.NewError(avutil.AVERROR_EAGAIN, "send")
		},
		receivePacket: func(bsfContext, avcodec.Packet) error {
			return avutil.NewError(avutil.AVERROR_EAGAIN, "receive")
		},
	}
	err := state.filter(nil, avcodec.Packet(unsafe.Pointer(&packetStorage)), nil, func(avcodec.Packet) error {
		return nil
	})
	if !errors.Is(err, errBitstreamFilterProtocolStalled) {
		t.Fatalf("filter error = %v, want %v", err, errBitstreamFilterProtocolStalled)
	}
}

func TestBitstreamFilterStatePropagatesOutputError(t *testing.T) {
	want := errors.New("retain output")
	packetStorage := byte(0)
	state := bitstreamFilterState{
		sendPacket: func(bsfContext, avcodec.Packet) error { return nil },
		receivePacket: func(bsfContext, avcodec.Packet) error {
			return nil
		},
	}
	err := state.filter(nil, avcodec.Packet(unsafe.Pointer(&packetStorage)), nil, func(avcodec.Packet) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("filter error = %v, want %v", err, want)
	}
}

func TestBitstreamFilterBindingsConcurrent(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	const workers = 32
	var wg sync.WaitGroup
	results := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- BitstreamFilterExists(BSFNameNull)
		}()
	}
	wg.Wait()
	close(results)
	for exists := range results {
		if !exists {
			t.Fatal("null bitstream filter was not found")
		}
	}
}

func TestBitstreamFilterNullPreservesPacketCount(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	decoder, err := NewDecoder(createTestVideo(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()

	filter, err := NewBitstreamFilter(BSFNameNull)
	if err != nil {
		t.Fatal(err)
	}
	defer filter.Close()

	decoder.mu.Lock()
	stream := avformat.GetStream(decoder.formatCtx, decoder.videoStreamIdx)
	parameters := avformat.GetStreamCodecPar(stream)
	timeBaseNum, timeBaseDen := avformat.GetStreamTimeBase(stream)
	err = filter.SetInputCodecParameters(parameters)
	decoder.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	filter.SetInputTimeBase(timeBaseNum, timeBaseDen)
	if err := filter.Init(); err != nil {
		t.Fatal(err)
	}

	const wantedPackets = 10
	inputPackets := 0
	outputPackets := 0
	for inputPackets < wantedPackets {
		packet, err := decoder.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if packet == nil {
			break
		}
		if packet.StreamIndex() != decoder.videoStreamIdx {
			continue
		}

		inputPackets++
		output, err := filter.Filter(packet.Raw())
		if err != nil {
			t.Fatal(err)
		}
		if output != nil {
			outputPackets++
		}
	}
	for {
		output, err := filter.Flush()
		if err != nil {
			t.Fatal(err)
		}
		if output == nil {
			break
		}
		outputPackets++
	}

	if inputPackets != wantedPackets {
		t.Fatalf("read %d video packets, want %d", inputPackets, wantedPackets)
	}
	if outputPackets != inputPackets {
		t.Fatalf("null filter produced %d packets for %d inputs", outputPackets, inputPackets)
	}
}
