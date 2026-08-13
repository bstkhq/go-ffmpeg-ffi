//go:build amd64 || arm64

package ffgo

import (
	"errors"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

var errDecoderProtocolStalled = errors.New("ffgo: decoder send/receive protocol stalled")

// decoderCodecState owns packets until FFmpeg accepts them and tracks the
// one-way transition from regular input to draining at end of stream.
type decoderCodecState struct {
	pending        []avcodec.Packet
	flushRequested bool
	flushSent      bool
	drained        bool

	sendPacket   func(avcodec.Context, avcodec.Packet) error
	receiveFrame func(avcodec.Context, avutil.Frame) error
	unrefFrame   func(avutil.Frame)
	freePacket   func(*avcodec.Packet)
}

func (s *decoderCodecState) enqueueOwned(packet avcodec.Packet) error {
	if packet == nil {
		return nil
	}
	if s.flushRequested || s.flushSent || s.drained {
		return errors.New("ffgo: cannot submit a packet after decoder flush")
	}
	s.pending = append(s.pending, packet)
	return nil
}

func (s *decoderCodecState) requestFlush() {
	s.flushRequested = true
}

func (s *decoderCodecState) next(ctx avcodec.Context, frame avutil.Frame) (bool, error) {
	if s.drained {
		return false, nil
	}

	for {
		s.unref(frame)
		err := s.receive(ctx, frame)
		if err == nil {
			return true, nil
		}
		if avutil.IsEOF(err) {
			s.drained = true
			s.clearPending()
			return false, nil
		}
		if !avutil.IsAgain(err) {
			return false, err
		}

		if len(s.pending) > 0 {
			err = s.send(ctx, s.pending[0])
			switch {
			case err == nil:
				s.releaseFirst()
				continue
			case avutil.IsAgain(err):
				return false, errDecoderProtocolStalled
			case avutil.IsEOF(err):
				s.drained = true
				s.clearPending()
				return false, err
			default:
				return false, err
			}
		}

		if !s.flushRequested {
			return false, nil
		}
		if s.flushSent {
			return false, errDecoderProtocolStalled
		}

		err = s.send(ctx, nil)
		switch {
		case err == nil:
			s.flushSent = true
			continue
		case avutil.IsAgain(err):
			return false, errDecoderProtocolStalled
		case avutil.IsEOF(err):
			s.flushSent = true
			s.drained = true
			return false, nil
		default:
			return false, err
		}
	}
}

func (s *decoderCodecState) reset() {
	s.clearPending()
	s.flushRequested = false
	s.flushSent = false
	s.drained = false
}

func (s *decoderCodecState) hasPending() bool {
	return len(s.pending) > 0
}

func (s *decoderCodecState) releaseFirst() {
	packet := s.pending[0]
	copy(s.pending, s.pending[1:])
	s.pending[len(s.pending)-1] = nil
	s.pending = s.pending[:len(s.pending)-1]
	s.free(&packet)
}

func (s *decoderCodecState) clearPending() {
	for i := range s.pending {
		s.free(&s.pending[i])
	}
	s.pending = nil
}

func (s *decoderCodecState) send(ctx avcodec.Context, packet avcodec.Packet) error {
	if s.sendPacket != nil {
		return s.sendPacket(ctx, packet)
	}
	return avcodec.SendPacket(ctx, packet)
}

func (s *decoderCodecState) receive(ctx avcodec.Context, frame avutil.Frame) error {
	if s.receiveFrame != nil {
		return s.receiveFrame(ctx, frame)
	}
	return avcodec.ReceiveFrame(ctx, frame)
}

func (s *decoderCodecState) unref(frame avutil.Frame) {
	if s.unrefFrame != nil {
		s.unrefFrame(frame)
		return
	}
	avutil.FrameUnref(frame)
}

func (s *decoderCodecState) free(packet *avcodec.Packet) {
	if s.freePacket != nil {
		s.freePacket(packet)
		return
	}
	avcodec.PacketFree(packet)
}
