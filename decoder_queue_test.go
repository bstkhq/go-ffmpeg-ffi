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

	for _, packet := range []struct {
		streamIndex int32
		pts         int64
	}{
		{streamIndex: 0, pts: 10},
		{streamIndex: 1, pts: 20},
		{streamIndex: 0, pts: 30},
		{streamIndex: 1, pts: 40},
	} {
		avcodec.SetPacketStreamIndex(d.packet, packet.streamIndex)
		avcodec.SetPacketPTS(d.packet, packet.pts)
		queued, err := d.queueCurrentPacketLocked()
		if err != nil {
			t.Fatal(err)
		}
		if !queued {
			t.Fatalf("stream %d was not queued", packet.streamIndex)
		}
	}

	packet := d.popQueuedPacketLocked(MediaTypeVideo)
	if packet == nil || avcodec.GetPacketPTS(packet) != 10 {
		t.Fatal("targeted video pop did not return the first video packet")
	}
	avcodec.PacketFree(&packet)

	mediaType, packet := d.popFirstQueuedPacketLocked()
	if mediaType != MediaTypeAudio || avcodec.GetPacketPTS(packet) != 20 {
		t.Fatalf("first remaining packet = (%v, %d), want audio PTS 20", mediaType, avcodec.GetPacketPTS(packet))
	}
	avcodec.PacketFree(&packet)

	mediaType, packet = d.popFirstQueuedPacketLocked()
	if mediaType != MediaTypeVideo || avcodec.GetPacketPTS(packet) != 30 {
		t.Fatalf("second remaining packet = (%v, %d), want video PTS 30", mediaType, avcodec.GetPacketPTS(packet))
	}
	avcodec.PacketFree(&packet)

	packet = d.popQueuedPacketLocked(MediaTypeAudio)
	if packet == nil || avcodec.GetPacketPTS(packet) != 40 {
		t.Fatal("targeted audio pop did not return the remaining audio packet")
	}
	avcodec.PacketFree(&packet)

	if d.packetQueue.len() != 0 {
		t.Fatalf("queue length = %d, want 0", d.packetQueue.len())
	}
}
