//go:build amd64 || arm64

package ffgo

import (
	"errors"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

var (
	errBitstreamFilterProtocolStalled = errors.New("ffgo: bitstream filter send/receive protocol stalled")
	errBitstreamFilterFlushed         = errors.New("ffgo: bitstream filter already flushed")
)

// bitstreamFilterState implements FFmpeg's send/receive protocol independently
// from the public one-packet-at-a-time API. A successful send consumes the
// input. EAGAIN does not, so pending output must be drained before retrying the
// exact same packet.
type bitstreamFilterState struct {
	flushSent bool
	drained   bool

	sendPacket    func(bsfContext, avcodec.Packet) error
	receivePacket func(bsfContext, avcodec.Packet) error
	unrefPacket   func(avcodec.Packet)
}

func (s *bitstreamFilterState) filter(
	ctx bsfContext,
	input avcodec.Packet,
	output avcodec.Packet,
	writePacket func(avcodec.Packet) error,
) error {
	if s.flushSent || s.drained {
		return errBitstreamFilterFlushed
	}

	for {
		err := s.send(ctx, input)
		switch {
		case err == nil:
			_, _, err = s.drain(ctx, output, writePacket)
			return err
		case avutil.IsAgain(err):
			produced, eof, drainErr := s.drain(ctx, output, writePacket)
			if drainErr != nil {
				return drainErr
			}
			if eof {
				return errBitstreamFilterFlushed
			}
			if !produced {
				return errBitstreamFilterProtocolStalled
			}
		case avutil.IsEOF(err):
			s.drained = true
			return errBitstreamFilterFlushed
		default:
			return err
		}
	}
}

func (s *bitstreamFilterState) flush(
	ctx bsfContext,
	output avcodec.Packet,
	writePacket func(avcodec.Packet) error,
) error {
	if s.drained {
		return nil
	}

	for !s.flushSent {
		err := s.send(ctx, nil)
		switch {
		case err == nil:
			s.flushSent = true
		case avutil.IsAgain(err):
			produced, eof, drainErr := s.drain(ctx, output, writePacket)
			if drainErr != nil {
				return drainErr
			}
			if eof {
				s.flushSent = true
				return nil
			}
			if !produced {
				return errBitstreamFilterProtocolStalled
			}
		case avutil.IsEOF(err):
			s.flushSent = true
			s.drained = true
			return nil
		default:
			return err
		}
	}

	_, eof, err := s.drain(ctx, output, writePacket)
	if err != nil {
		return err
	}
	if !eof {
		return errBitstreamFilterProtocolStalled
	}
	return nil
}

func (s *bitstreamFilterState) drain(
	ctx bsfContext,
	packet avcodec.Packet,
	writePacket func(avcodec.Packet) error,
) (produced, eof bool, err error) {
	for {
		s.unref(packet)
		err := s.receive(ctx, packet)
		switch {
		case err == nil:
			produced = true
			if err := writePacket(packet); err != nil {
				return produced, false, err
			}
		case avutil.IsAgain(err):
			return produced, false, nil
		case avutil.IsEOF(err):
			s.drained = true
			return produced, true, nil
		default:
			return produced, false, err
		}
	}
}

func (s *bitstreamFilterState) send(ctx bsfContext, packet avcodec.Packet) error {
	if s.sendPacket != nil {
		return s.sendPacket(ctx, packet)
	}
	ret := avBsfSendPacket(uintptr(ctx), uintptr(packet))
	return avutil.NewError(ret, "av_bsf_send_packet")
}

func (s *bitstreamFilterState) receive(ctx bsfContext, packet avcodec.Packet) error {
	if s.receivePacket != nil {
		return s.receivePacket(ctx, packet)
	}
	ret := avBsfReceivePacket(uintptr(ctx), uintptr(packet))
	return avutil.NewError(ret, "av_bsf_receive_packet")
}

func (s *bitstreamFilterState) unref(packet avcodec.Packet) {
	if s.unrefPacket != nil {
		s.unrefPacket(packet)
		return
	}
	avcodec.PacketUnref(packet)
}
