//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

type decoderQueuedPacket struct {
	sequence uint64
	packet   avcodec.Packet
}

type decoderPacketFIFO struct {
	packets []decoderQueuedPacket
	head    int
}

func (q *decoderPacketFIFO) push(packet decoderQueuedPacket) {
	q.packets = append(q.packets, packet)
}

func (q *decoderPacketFIFO) peek() (decoderQueuedPacket, bool) {
	if q.head >= len(q.packets) {
		return decoderQueuedPacket{}, false
	}
	return q.packets[q.head], true
}

func (q *decoderPacketFIFO) pop() avcodec.Packet {
	if q.head >= len(q.packets) {
		return nil
	}

	packet := q.packets[q.head].packet
	q.packets[q.head] = decoderQueuedPacket{}
	q.head++
	if q.head == len(q.packets) {
		q.packets = q.packets[:0]
		q.head = 0
	}
	return packet
}

func (q *decoderPacketFIFO) len() int {
	return len(q.packets) - q.head
}

func (q *decoderPacketFIFO) clear() {
	for i := q.head; i < len(q.packets); i++ {
		avcodec.PacketFree(&q.packets[i].packet)
	}
	q.packets = nil
	q.head = 0
}

type decoderPacketQueue struct {
	video        decoderPacketFIFO
	audio        decoderPacketFIFO
	nextSequence uint64
}

func (q *decoderPacketQueue) push(mediaType MediaType, packet avcodec.Packet) bool {
	queued := decoderQueuedPacket{
		sequence: q.nextSequence,
		packet:   packet,
	}

	switch mediaType {
	case MediaTypeVideo:
		q.video.push(queued)
	case MediaTypeAudio:
		q.audio.push(queued)
	default:
		return false
	}
	q.nextSequence++
	return true
}

func (q *decoderPacketQueue) pop(mediaType MediaType) avcodec.Packet {
	switch mediaType {
	case MediaTypeVideo:
		return q.video.pop()
	case MediaTypeAudio:
		return q.audio.pop()
	default:
		return nil
	}
}

func (q *decoderPacketQueue) popFirst() (MediaType, avcodec.Packet) {
	video, hasVideo := q.video.peek()
	audio, hasAudio := q.audio.peek()

	switch {
	case !hasVideo && !hasAudio:
		return MediaTypeUnknown, nil
	case !hasAudio || hasVideo && video.sequence < audio.sequence:
		return MediaTypeVideo, q.video.pop()
	default:
		return MediaTypeAudio, q.audio.pop()
	}
}

func (q *decoderPacketQueue) len() int {
	return q.video.len() + q.audio.len()
}

func (q *decoderPacketQueue) clear() {
	q.video.clear()
	q.audio.clear()
	q.nextSequence = 0
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
	if !d.packetQueue.push(mediaType, clone) {
		avcodec.PacketFree(&clone)
		return errors.New("ffgo: cannot queue unsupported decoder media type")
	}
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
	return d.packetQueue.pop(mediaType)
}

func (d *Decoder) popFirstQueuedPacketLocked() (MediaType, avcodec.Packet) {
	return d.packetQueue.popFirst()
}

func (d *Decoder) clearDecodeStateLocked() {
	d.videoState.reset()
	d.audioState.reset()
	d.packetQueue.clear()
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
