//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

type decoderQueuedPacket struct {
	mediaType MediaType
	packet    avcodec.Packet
}

func cloneRawPacket(packet avcodec.Packet) (avcodec.Packet, error) {
	clone := avcodec.PacketAlloc()
	if clone == nil {
		return nil, ErrOutOfMemory
	}
	if err := avcodec.PacketRef(clone, packet); err != nil {
		avcodec.PacketFree(&clone)
		return nil, err
	}
	return clone, nil
}

func (d *Decoder) queuePacketRefLocked(packet avcodec.Packet, mediaType MediaType) error {
	clone, err := cloneRawPacket(packet)
	if err != nil {
		return err
	}
	d.packetQueue = append(d.packetQueue, decoderQueuedPacket{
		mediaType: mediaType,
		packet:    clone,
	})
	return nil
}

func (d *Decoder) queueCurrentPacketLocked() (bool, error) {
	streamIndex := int(avcodec.GetPacketStreamIndex(d.packet))
	switch streamIndex {
	case d.videoStreamIdx:
		if d.videoStreamIdx >= 0 {
			return true, d.queuePacketRefLocked(d.packet, MediaTypeVideo)
		}
	case d.audioStreamIdx:
		if d.audioStreamIdx >= 0 {
			return true, d.queuePacketRefLocked(d.packet, MediaTypeAudio)
		}
	}
	return false, nil
}

func (d *Decoder) readPacketIntoQueueLocked() (bool, error) {
	if d.demuxEOF {
		return false, nil
	}

	avcodec.PacketUnref(d.packet)
	if err := d.readInputPacketLocked(d.packet); err != nil {
		if avutil.IsEOF(err) {
			d.demuxEOF = true
			return false, nil
		}
		return false, err
	}
	return d.queueCurrentPacketLocked()
}

func (d *Decoder) popQueuedPacketLocked(mediaType MediaType) avcodec.Packet {
	for i := range d.packetQueue {
		if d.packetQueue[i].mediaType != mediaType {
			continue
		}
		return d.removeQueuedPacketLocked(i)
	}
	return nil
}

func (d *Decoder) popFirstQueuedPacketLocked() (MediaType, avcodec.Packet) {
	if len(d.packetQueue) == 0 {
		return MediaTypeUnknown, nil
	}
	mediaType := d.packetQueue[0].mediaType
	return mediaType, d.removeQueuedPacketLocked(0)
}

func (d *Decoder) removeQueuedPacketLocked(index int) avcodec.Packet {
	packet := d.packetQueue[index].packet
	copy(d.packetQueue[index:], d.packetQueue[index+1:])
	d.packetQueue[len(d.packetQueue)-1] = decoderQueuedPacket{}
	d.packetQueue = d.packetQueue[:len(d.packetQueue)-1]
	return packet
}

func (d *Decoder) clearDecodeStateLocked() {
	d.videoState.reset()
	d.audioState.reset()
	for i := range d.packetQueue {
		avcodec.PacketFree(&d.packetQueue[i].packet)
	}
	d.packetQueue = nil
	d.demuxEOF = false
	d.activeMedia = MediaTypeUnknown
	if d.prefetchedFrame != nil {
		avutil.FrameFree(&d.prefetchedFrame)
	}
	d.prefetchedMedia = MediaTypeUnknown
	avcodec.PacketUnref(d.packet)
	avutil.FrameUnref(d.frame)
}

func (d *Decoder) prefetchCurrentFrameLocked(mediaType MediaType) error {
	frame := avutil.FrameAlloc()
	if frame == nil {
		return ErrOutOfMemory
	}
	if err := avutil.FrameRef(frame, d.frame); err != nil {
		avutil.FrameFree(&frame)
		return err
	}
	if d.prefetchedFrame != nil {
		avutil.FrameFree(&d.prefetchedFrame)
	}
	d.prefetchedFrame = frame
	d.prefetchedMedia = mediaType
	return nil
}

func (d *Decoder) takePrefetchedFrameLocked(mediaType MediaType) (bool, error) {
	if d.prefetchedFrame == nil || d.prefetchedMedia != mediaType {
		return false, nil
	}
	avutil.FrameUnref(d.frame)
	if err := avutil.FrameRef(d.frame, d.prefetchedFrame); err != nil {
		return false, err
	}
	avutil.FrameFree(&d.prefetchedFrame)
	d.prefetchedMedia = MediaTypeUnknown
	return true, nil
}

func (d *Decoder) codecStateLocked(mediaType MediaType) (*decoderCodecState, avcodec.Context, error) {
	switch mediaType {
	case MediaTypeVideo:
		if !d.videoDecoderOpen || d.videoCodecCtx == nil {
			return nil, nil, errors.New("ffgo: video decoder not opened; call OpenVideoDecoder first")
		}
		return &d.videoState, d.videoCodecCtx, nil
	case MediaTypeAudio:
		if !d.audioDecoderOpen || d.audioCodecCtx == nil {
			return nil, nil, errors.New("ffgo: audio decoder not opened; call OpenAudioDecoder first")
		}
		return &d.audioState, d.audioCodecCtx, nil
	default:
		return nil, nil, errors.New("ffgo: unsupported decoder media type")
	}
}

func (d *Decoder) enqueueOwnedPacketLocked(mediaType MediaType, packet avcodec.Packet) error {
	state, _, err := d.codecStateLocked(mediaType)
	if err != nil {
		avcodec.PacketFree(&packet)
		return err
	}
	if err := state.enqueueOwned(packet); err != nil {
		avcodec.PacketFree(&packet)
		return err
	}
	return nil
}
