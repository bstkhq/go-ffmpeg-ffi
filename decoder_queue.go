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

func (q *decoderPacketFIFO) clear(release func(*avcodec.Packet)) {
	for i := q.head; i < len(q.packets); i++ {
		release(&q.packets[i].packet)
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

func (q *decoderPacketQueue) clear(release func(*avcodec.Packet)) {
	q.video.clear(release)
	q.audio.clear(release)
	q.nextSequence = 0
}

const decoderPacketPoolLimit = 32

type decoderPacketPool struct {
	packets []avcodec.Packet
}

func (p *decoderPacketPool) acquire() (avcodec.Packet, error) {
	if count := len(p.packets); count > 0 {
		packet := p.packets[count-1]
		p.packets[count-1] = nil
		p.packets = p.packets[:count-1]
		return packet, nil
	}

	packet := avcodec.PacketAlloc()
	if packet == nil {
		return nil, ErrOutOfMemory
	}
	return packet, nil
}

func (p *decoderPacketPool) clone(source avcodec.Packet, cache bool) (avcodec.Packet, error) {
	packet, err := p.acquire()
	if err != nil {
		return nil, err
	}
	if err := avcodec.PacketRef(packet, source); err != nil {
		p.recycle(&packet, cache)
		return nil, err
	}
	return packet, nil
}

func (p *decoderPacketPool) recycle(packet *avcodec.Packet, cache bool) {
	if packet == nil || *packet == nil {
		return
	}
	avcodec.PacketUnref(*packet)
	if !cache || len(p.packets) >= decoderPacketPoolLimit {
		avcodec.PacketFree(packet)
		return
	}
	p.packets = append(p.packets, *packet)
	*packet = nil
}

func (p *decoderPacketPool) clear() {
	for i := range p.packets {
		avcodec.PacketFree(&p.packets[i])
	}
	p.packets = nil
}

func (p *decoderPacketPool) len() int {
	return len(p.packets)
}

func (d *Decoder) clonePacketLocked(source avcodec.Packet) (avcodec.Packet, error) {
	return d.packetPool.clone(source, !d.closed)
}

func (d *Decoder) recyclePacketLocked(packet *avcodec.Packet) {
	d.packetPool.recycle(packet, !d.closed)
}

func (d *Decoder) clearPacketPoolLocked() {
	d.packetPool.clear()
}

func (d *Decoder) queuePacketRefLocked(packet avcodec.Packet, mediaType MediaType) error {
	clone, err := d.clonePacketLocked(packet)
	if err != nil {
		return err
	}
	if !d.packetQueue.push(mediaType, clone) {
		d.recyclePacketLocked(&clone)
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
	d.packetQueue.clear(d.recyclePacketLocked)
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
		if d.videoState.freePacket == nil {
			d.videoState.freePacket = d.recyclePacketLocked
		}
		return &d.videoState, d.videoCodecCtx, nil
	case MediaTypeAudio:
		if !d.audioDecoderOpen || d.audioCodecCtx == nil {
			return nil, nil, errors.New("ffgo: audio decoder not opened; call OpenAudioDecoder first")
		}
		if d.audioState.freePacket == nil {
			d.audioState.freePacket = d.recyclePacketLocked
		}
		return &d.audioState, d.audioCodecCtx, nil
	default:
		return nil, nil, errors.New("ffgo: unsupported decoder media type")
	}
}

func (d *Decoder) enqueueOwnedPacketLocked(mediaType MediaType, packet avcodec.Packet) error {
	state, _, err := d.codecStateLocked(mediaType)
	if err != nil {
		d.recyclePacketLocked(&packet)
		return err
	}
	if err := state.enqueueOwned(packet); err != nil {
		d.recyclePacketLocked(&packet)
		return err
	}
	return nil
}
