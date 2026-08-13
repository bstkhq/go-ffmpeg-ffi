//go:build amd64 || arm64

package ffgo

import (
	"errors"
	"io"
	"time"

	"github.com/bstkhq/go-ffmpeg-ffi/avcodec"
	"github.com/bstkhq/go-ffmpeg-ffi/avformat"
	"github.com/bstkhq/go-ffmpeg-ffi/avutil"
)

// SeekPrecise performs frame-accurate seeking to the specified timestamp.
// Unlike Seek which seeks to the nearest keyframe, SeekPrecise decodes
// frames from the keyframe until reaching the exact target frame.
// This is slower but guarantees frame-accurate positioning.
func (d *Decoder) SeekPrecise(ts time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return errDecoderClosed
	}
	if err := d.openVideoDecoderLocked(); err != nil {
		return err
	}

	// Get video stream time base for conversion
	stream := avformat.GetStream(d.formatCtx, d.videoStreamIdx)
	if stream == nil {
		return errors.New("ffgo: failed to get video stream")
	}
	tbNum, tbDen := avformat.GetStreamTimeBase(stream)
	if tbNum <= 0 || tbDen <= 0 {
		return errors.New("ffgo: invalid video stream time base")
	}

	// Convert target to stream time base
	// targetTS is in microseconds (AV_TIME_BASE = 1000000)
	targetTS := ts.Microseconds()
	targetPTS := targetTS * int64(tbDen) / (int64(tbNum) * 1000000)

	// Seek to keyframe before target
	if err := d.seekInputLocked(-1, targetTS, avformat.SeekFlagBackward); err != nil {
		return err
	}

	// Flush decoder buffers
	if d.videoCodecCtx != nil {
		avcodec.FlushBuffers(d.videoCodecCtx)
	}
	if d.audioCodecCtx != nil {
		avcodec.FlushBuffers(d.audioCodecCtx)
	}
	d.clearDecodeStateLocked()

	// Decode frames until we reach or pass the target PTS.
	for {
		frame, err := d.nextFrameLocked(MediaTypeVideo)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errors.New("ffgo: reached end of video before precise seek target")
			}
			return err
		}
		pts := avutil.GetFramePTS(frame.ptr)
		if pts == avutil.NoPTSValue {
			return errors.New("ffgo: precise seek requires frame timestamps")
		}
		if pts >= targetPTS {
			return d.prefetchCurrentFrameLocked(MediaTypeVideo)
		}
	}
}

// SeekToFrame seeks to a specific frame number.
// frameNum is 0-based (first frame is 0).
// This method uses frame-accurate seeking internally.
func (d *Decoder) SeekToFrame(frameNum int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return errDecoderClosed
	}

	if d.videoStreamIdx < 0 {
		return ErrNoVideoStream
	}

	// Get frame rate to calculate timestamp
	stream := avformat.GetStream(d.formatCtx, d.videoStreamIdx)
	if stream == nil {
		return errors.New("ffgo: failed to get video stream")
	}

	fpsNum, fpsDen := avformat.GetStreamAvgFrameRate(stream)
	if fpsNum == 0 {
		return errors.New("ffgo: cannot determine frame rate")
	}

	// Calculate timestamp in microseconds
	// timestamp = frameNum * (1 / fps) = frameNum * fpsDen / fpsNum
	timestampUS := frameNum * int64(fpsDen) * 1000000 / int64(fpsNum)

	d.mu.Unlock()
	err := d.SeekPrecise(time.Duration(timestampUS) * time.Microsecond)
	d.mu.Lock()
	return err
}

// ExtractThumbnail extracts a single frame at the specified timestamp.
// Returns the decoded frame or an error.
// The returned frame must be freed by the caller when done.
func (d *Decoder) ExtractThumbnail(ts time.Duration) (Frame, error) {
	// Ensure video decoder is open
	if err := d.OpenVideoDecoder(); err != nil {
		return Frame{}, err
	}

	// Seek to the target position
	if err := d.SeekPrecise(ts); err != nil {
		return Frame{}, err
	}

	// Decode the next frame
	// Return an owned frame (safe for caller to free).
	return d.DecodeVideoCopy()
}

// ExtractThumbnailAtFrame extracts a frame at the specified frame number.
// Returns the decoded frame or an error.
// The returned frame must be freed by the caller when done.
func (d *Decoder) ExtractThumbnailAtFrame(frameNum int64) (Frame, error) {
	// Ensure video decoder is open
	if err := d.OpenVideoDecoder(); err != nil {
		return Frame{}, err
	}

	// Seek to the frame
	if err := d.SeekToFrame(frameNum); err != nil {
		return Frame{}, err
	}

	// Decode the next frame
	// Return an owned frame (safe for caller to free).
	return d.DecodeVideoCopy()
}

// ExtractThumbnails extracts multiple frames at evenly spaced intervals.
// count is the number of thumbnails to extract.
// Returns a slice of frames or an error.
// The returned frames must be freed by the caller when done.
func (d *Decoder) ExtractThumbnails(count int) ([]Frame, error) {
	if count <= 0 {
		return nil, errors.New("ffgo: count must be positive")
	}

	// Ensure video decoder is open
	if err := d.OpenVideoDecoder(); err != nil {
		return nil, err
	}

	duration := d.Duration()
	if duration <= 0 {
		return nil, errors.New("ffgo: cannot determine duration")
	}

	frames := make([]Frame, 0, count)
	interval := duration / time.Duration(count+1)

	for i := 1; i <= count; i++ {
		ts := interval * time.Duration(i)

		frame, err := d.ExtractThumbnail(ts)
		if err != nil {
			// Free already extracted frames
			for _, f := range frames {
				_ = FrameFree(&f)
			}
			return nil, err
		}
		// ExtractThumbnail already returns an owned frame.
		frames = append(frames, frame)
	}

	return frames, nil
}

// SeekKeyframe seeks to the nearest keyframe at or before the specified timestamp.
// This is faster than SeekPrecise but may not land exactly on the target.
func (d *Decoder) SeekKeyframe(ts time.Duration) error {
	return d.Seek(ts)
}

// SeekAny seeks to any frame (not just keyframes) near the specified timestamp.
// This uses av_seek_frame with AVSEEK_FLAG_ANY, which may result in
// corrupted frames if not at a keyframe. Use with caution.
func (d *Decoder) SeekAny(ts time.Duration) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return errDecoderClosed
	}

	timestamp := ts.Microseconds()
	if err := d.seekInputLocked(-1, timestamp, avformat.SeekFlagAny); err != nil {
		return err
	}

	// Flush decoder buffers
	if d.videoCodecCtx != nil {
		avcodec.FlushBuffers(d.videoCodecCtx)
	}
	if d.audioCodecCtx != nil {
		avcodec.FlushBuffers(d.audioCodecCtx)
	}
	d.clearDecodeStateLocked()

	return nil
}

// SeekByBytes seeks by byte position in the file.
// This is useful for formats that don't have proper timestamps.
func (d *Decoder) SeekByBytes(bytePos int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return errDecoderClosed
	}

	if err := d.seekInputLocked(-1, bytePos, avformat.SeekFlagByte); err != nil {
		return err
	}

	// Flush decoder buffers
	if d.videoCodecCtx != nil {
		avcodec.FlushBuffers(d.videoCodecCtx)
	}
	if d.audioCodecCtx != nil {
		avcodec.FlushBuffers(d.audioCodecCtx)
	}
	d.clearDecodeStateLocked()

	return nil
}

// SeekToByte seeks to a byte position in the file.
// This is an alias for SeekByBytes for API consistency.
func (d *Decoder) SeekToByte(bytePos int64) error {
	return d.SeekByBytes(bytePos)
}

// TotalFrames returns an estimate of the total number of video frames.
// This is calculated from duration and frame rate, so may not be exact.
func (d *Decoder) TotalFrames() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.formatCtx == nil || d.videoStreamIdx < 0 {
		return 0
	}

	stream := avformat.GetStream(d.formatCtx, d.videoStreamIdx)
	if stream == nil {
		return 0
	}

	fpsNum, fpsDen := avformat.GetStreamAvgFrameRate(stream)
	if fpsNum == 0 {
		return 0
	}

	durationUS := d.durationMicrosecondsLocked()
	return durationUS * int64(fpsNum) / (int64(fpsDen) * 1000000)
}
