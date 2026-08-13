//go:build amd64 || arm64

package ffmpeg

import (
	"errors"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

var (
	errEncoderProtocolStalled = errors.New("ffmpeg: encoder send/receive protocol stalled")
	errEncoderFlushed         = errors.New("ffmpeg: encoder already flushed")
)

type encoderCodecState struct {
	flushSent bool
	drained   bool

	sendFrame     func(avcodec.Context, avutil.Frame) error
	receivePacket func(avcodec.Context, avcodec.Packet) error
	unrefPacket   func(avcodec.Packet)
}

func (s *encoderCodecState) encode(
	ctx avcodec.Context,
	frame avutil.Frame,
	packet avcodec.Packet,
	writePacket func(avcodec.Packet) error,
) error {
	if frame == nil {
		return s.flush(ctx, packet, writePacket)
	}
	if s.flushSent || s.drained {
		return errEncoderFlushed
	}

	for {
		err := s.send(ctx, frame)
		switch {
		case err == nil:
			_, _, err = s.drain(ctx, packet, writePacket)
			return err
		case avutil.IsAgain(err):
			produced, eof, drainErr := s.drain(ctx, packet, writePacket)
			if drainErr != nil {
				return drainErr
			}
			if eof {
				return errEncoderFlushed
			}
			if !produced {
				return errEncoderProtocolStalled
			}
		case avutil.IsEOF(err):
			s.drained = true
			return errEncoderFlushed
		default:
			return err
		}
	}
}

func (s *encoderCodecState) flush(
	ctx avcodec.Context,
	packet avcodec.Packet,
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
			produced, eof, drainErr := s.drain(ctx, packet, writePacket)
			if drainErr != nil {
				return drainErr
			}
			if eof {
				s.flushSent = true
				return nil
			}
			if !produced {
				return errEncoderProtocolStalled
			}
		case avutil.IsEOF(err):
			s.flushSent = true
			s.drained = true
			return nil
		default:
			return err
		}
	}

	_, eof, err := s.drain(ctx, packet, writePacket)
	if err != nil {
		return err
	}
	if !eof {
		return errEncoderProtocolStalled
	}
	return nil
}

func (s *encoderCodecState) drain(
	ctx avcodec.Context,
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

func (s *encoderCodecState) send(ctx avcodec.Context, frame avutil.Frame) error {
	if s.sendFrame != nil {
		return s.sendFrame(ctx, frame)
	}
	return avcodec.SendFrame(ctx, frame)
}

func (s *encoderCodecState) receive(ctx avcodec.Context, packet avcodec.Packet) error {
	if s.receivePacket != nil {
		return s.receivePacket(ctx, packet)
	}
	return avcodec.ReceivePacket(ctx, packet)
}

func (s *encoderCodecState) unref(packet avcodec.Packet) {
	if s.unrefPacket != nil {
		s.unrefPacket(packet)
		return
	}
	avcodec.PacketUnref(packet)
}
