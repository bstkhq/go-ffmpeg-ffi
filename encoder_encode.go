//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"fmt"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

func (e *Encoder) encodeVideoFrameLocked(frame Frame) error {
	if e.videoCodecCtx == nil || e.videoPacket == nil || e.videoStream == nil {
		return errors.New("ffgo: video encoder is not configured")
	}
	if frame.ptr != nil {
		avutil.SetFramePTS(frame.ptr, e.frameCount)
	}
	if err := e.videoState.encode(e.videoCodecCtx, frame.ptr, e.videoPacket, e.writeVideoPacketLocked); err != nil {
		return err
	}
	if frame.ptr != nil {
		e.frameCount++
	}
	return nil
}

func (e *Encoder) encodeAudioFrameLocked(frame Frame) error {
	if e.audioCodecCtx == nil || e.audioPacket == nil || e.audioStream == nil {
		return errors.New("ffgo: audio encoder is not configured")
	}
	if frame.ptr != nil {
		avutil.SetFramePTS(frame.ptr, e.audioFrameCnt)
	}
	if err := e.audioState.encode(e.audioCodecCtx, frame.ptr, e.audioPacket, e.writeAudioPacketLocked); err != nil {
		return err
	}
	if frame.ptr != nil {
		e.audioFrameCnt += int64(avutil.GetFrameNbSamples(frame.ptr))
	}
	return nil
}

func (e *Encoder) writeVideoPacketLocked(packet avcodec.Packet) error {
	avcodec.SetPacketStreamIndex(packet, avformat.GetStreamIndex(e.videoStream))
	streamNum, streamDen := avformat.GetStreamTimeBase(e.videoStream)
	avcodec.RescalePacketTS(packet,
		avcodec.GetCtxTimeBase(e.videoCodecCtx),
		NewRational(streamNum, streamDen))
	return avformat.InterleavedWriteFrame(e.formatCtx, packet)
}

func (e *Encoder) writeAudioPacketLocked(packet avcodec.Packet) error {
	avcodec.SetPacketStreamIndex(packet, avformat.GetStreamIndex(e.audioStream))
	streamNum, streamDen := avformat.GetStreamTimeBase(e.audioStream)
	avcodec.RescalePacketTS(packet,
		avcodec.GetCtxTimeBase(e.audioCodecCtx),
		NewRational(streamNum, streamDen))
	return avformat.InterleavedWriteFrame(e.formatCtx, packet)
}

func (e *Encoder) flushEncodersLocked() error {
	var flushErrors []error
	if e.videoCodecCtx != nil && e.videoPacket != nil {
		if err := e.videoState.encode(e.videoCodecCtx, nil, e.videoPacket, e.writeVideoPacketLocked); err != nil {
			flushErrors = append(flushErrors, fmt.Errorf("ffgo: flush video encoder: %w", err))
		}
	}
	if e.audioCodecCtx != nil && e.audioPacket != nil {
		if err := e.audioState.encode(e.audioCodecCtx, nil, e.audioPacket, e.writeAudioPacketLocked); err != nil {
			flushErrors = append(flushErrors, fmt.Errorf("ffgo: flush audio encoder: %w", err))
		}
	}
	return errors.Join(flushErrors...)
}
