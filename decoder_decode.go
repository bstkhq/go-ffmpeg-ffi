//go:build amd64 || arm64

package ffgo

import (
	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func (d *Decoder) nextFrameLocked(mediaType MediaType) (Frame, error) {
	if ready, err := d.takePrefetchedFrameLocked(mediaType); err != nil {
		return Frame{}, err
	} else if ready {
		return Frame{ptr: d.frame, owned: false}, nil
	}

	state, ctx, err := d.codecStateLocked(mediaType)
	if err != nil {
		return Frame{}, err
	}

	for {
		ready, err := state.next(ctx, d.frame)
		if err != nil {
			return Frame{}, err
		}
		if ready {
			return Frame{ptr: d.frame, owned: false}, nil
		}
		if state.drained {
			return Frame{}, nil
		}

		if packet := d.popQueuedPacketLocked(mediaType); packet != nil {
			if err := d.enqueueOwnedPacketLocked(mediaType, packet); err != nil {
				return Frame{}, err
			}
			continue
		}
		if d.demuxEOF {
			state.requestFlush()
			continue
		}
		if _, err := d.readPacketIntoQueueLocked(); err != nil {
			return Frame{}, err
		}
	}
}

func (d *Decoder) readFrameLocked() (*FrameWrapper, error) {
	for {
		if d.prefetchedFrame != nil {
			mediaType := d.prefetchedMedia
			ready, err := d.takePrefetchedFrameLocked(mediaType)
			if err != nil {
				return nil, err
			}
			if ready {
				return WrapFrame(Frame{ptr: d.frame, owned: false}, mediaType), nil
			}
		}

		if d.activeMedia != MediaTypeUnknown {
			state, ctx, err := d.codecStateLocked(d.activeMedia)
			if err != nil {
				return nil, err
			}
			ready, err := state.next(ctx, d.frame)
			if err != nil {
				return nil, err
			}
			if ready {
				return WrapFrame(Frame{ptr: d.frame, owned: false}, d.activeMedia), nil
			}
			d.activeMedia = MediaTypeUnknown
		}

		mediaType, packet := d.popFirstQueuedPacketLocked()
		if packet != nil {
			if err := d.enqueueOwnedPacketLocked(mediaType, packet); err != nil {
				return nil, err
			}
			d.activeMedia = mediaType
			continue
		}

		if !d.demuxEOF {
			if _, err := d.readPacketIntoQueueLocked(); err != nil {
				return nil, err
			}
			continue
		}

		if d.activateFlushLocked(MediaTypeVideo) || d.activateFlushLocked(MediaTypeAudio) {
			continue
		}
		return nil, nil
	}
}

func (d *Decoder) activateFlushLocked(mediaType MediaType) bool {
	state, _, err := d.codecStateLocked(mediaType)
	if err != nil || state.drained {
		return false
	}
	state.requestFlush()
	d.activeMedia = mediaType
	return true
}

func (d *Decoder) readPacketLocked() (*Packet, error) {
	avcodec.PacketUnref(d.packet)
	if d.packetQueue.len() > 0 {
		_, packet := d.popFirstQueuedPacketLocked()
		err := avcodec.PacketRef(d.packet, packet)
		d.recyclePacketLocked(&packet)
		if err != nil {
			return nil, err
		}
		return &Packet{ptr: d.packet, owned: false}, nil
	}
	if d.demuxEOF {
		return nil, nil
	}

	if err := d.readInputPacketLocked(d.packet); err != nil {
		if avutil.IsEOF(err) {
			d.demuxEOF = true
			return nil, nil
		}
		return nil, err
	}
	return &Packet{ptr: d.packet, owned: false}, nil
}
