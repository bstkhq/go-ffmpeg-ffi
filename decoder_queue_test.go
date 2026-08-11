//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"testing"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func TestDecoderPacketQueuePreservesDemuxOrder(t *testing.T) {
	if !requireFFmpeg(t) {
		return
	}

	d := Decoder{
		packet:         avcodec.PacketAlloc(),
		frame:          avutil.FrameAlloc(),
		videoStreamIdx: 0,
		audioStreamIdx: 1,
	}
	if d.packet == nil || d.frame == nil {
		t.Fatal("failed to allocate decoder test data")
	}
	t.Cleanup(func() {
		d.clearDecodeStateLocked()
		avcodec.PacketFree(&d.packet)
		avutil.FrameFree(&d.frame)
	})

	for _, streamIndex := range []int32{0, 1, 0} {
		avcodec.SetPacketStreamIndex(d.packet, streamIndex)
		queued, err := d.queueCurrentPacketLocked()
		if err != nil {
			t.Fatal(err)
		}
		if !queued {
			t.Fatalf("stream %d was not queued", streamIndex)
		}
	}

	mediaType, packet := d.popFirstQueuedPacketLocked()
	if mediaType != MediaTypeVideo || avcodec.GetPacketStreamIndex(packet) != 0 {
		t.Fatalf("first packet = (%v, %d), want video stream 0", mediaType, avcodec.GetPacketStreamIndex(packet))
	}
	avcodec.PacketFree(&packet)

	packet = d.popQueuedPacketLocked(MediaTypeVideo)
	if packet == nil || avcodec.GetPacketStreamIndex(packet) != 0 {
		t.Fatal("targeted video pop did not return the remaining video packet")
	}
	avcodec.PacketFree(&packet)

	mediaType, packet = d.popFirstQueuedPacketLocked()
	if mediaType != MediaTypeAudio || avcodec.GetPacketStreamIndex(packet) != 1 {
		t.Fatalf("remaining packet = (%v, %d), want audio stream 1", mediaType, avcodec.GetPacketStreamIndex(packet))
	}
	avcodec.PacketFree(&packet)

	if len(d.packetQueue) != 0 {
		t.Fatalf("queue length = %d, want 0", len(d.packetQueue))
	}
}
