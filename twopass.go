//go:build !ios && !android && (amd64 || arm64)

package ffgo

import (
	"errors"
	"os"
	"path/filepath"
)

// TwoPassTranscode performs a simple 2-pass video transcode using the provided encoder options.
//
// Notes:
// - This helper currently transcodes video only (audio is ignored).
// - Input must be seekable (regular files are fine).
func TwoPassTranscode(input, output string, opts *EncoderOptions) error {
	if input == "" || output == "" {
		return errors.New("ffgo: input and output are required")
	}
	if opts == nil || opts.Video == nil {
		return errors.New("ffgo: EncoderOptions.Video is required")
	}

	dec, err := NewDecoder(input)
	if err != nil {
		return err
	}
	defer dec.Close()

	if !dec.HasVideo() {
		return errors.New("ffgo: input has no video stream")
	}
	if err := dec.OpenVideoDecoder(); err != nil {
		return err
	}
	videoInfo := dec.VideoStream()
	if videoInfo == nil {
		return errors.New("ffgo: video stream info not available")
	}

	// Fill common defaults from input if unset.
	if opts.Video.Width <= 0 {
		opts.Video.Width = videoInfo.Width
	}
	if opts.Video.Height <= 0 {
		opts.Video.Height = videoInfo.Height
	}
	if opts.Video.PixelFormat == PixelFormatNone {
		// A safe default for most H.264 encoders.
		opts.Video.PixelFormat = PixelFormatYUV420P
	}

	workspace, err := newTwoPassWorkspace(output, opts)
	if err != nil {
		return err
	}
	defer workspace.cleanup()

	if err := runPass(dec, videoInfo, workspace.passOutput, opts, 1, workspace.passLogFile); err != nil {
		return err
	}

	// Seek back to start for pass 2.
	if err := dec.SeekTimestamp(0); err != nil {
		return err
	}

	if err := runPass(dec, videoInfo, output, opts, 2, workspace.passLogFile); err != nil {
		return err
	}
	return nil
}

type twoPassWorkspace struct {
	passLogFile string
	passOutput  string
	tempDir     string
}

func newTwoPassWorkspace(output string, opts *EncoderOptions) (twoPassWorkspace, error) {
	workspace := twoPassWorkspace{
		passLogFile: opts.PassLogFile,
		passOutput:  opts.PassOutput,
	}
	if workspace.passLogFile != "" && workspace.passOutput != "" {
		return workspace, nil
	}

	tempDir, err := os.MkdirTemp("", "ffgo-twopass-*")
	if err != nil {
		return twoPassWorkspace{}, err
	}
	workspace.tempDir = tempDir
	if workspace.passLogFile == "" {
		workspace.passLogFile = filepath.Join(tempDir, "passlog")
	}
	if workspace.passOutput == "" {
		ext := filepath.Ext(output)
		if ext == "" {
			ext = ".mp4"
		}
		workspace.passOutput = filepath.Join(tempDir, "pass1"+ext)
	}
	return workspace, nil
}

func (w twoPassWorkspace) cleanup() {
	if w.tempDir != "" {
		_ = os.RemoveAll(w.tempDir)
	}
}

func runPass(dec *Decoder, videoInfo *StreamInfo, output string, baseOpts *EncoderOptions, pass int, passBase string) error {
	// Clone options for this pass
	passOpts := *baseOpts
	passOpts.Pass = pass
	passOpts.PassLogFile = passBase
	// PassOutput is only meaningful to the helper, not encoder creation.

	enc, err := NewEncoderWithOptions(output, &passOpts)
	if err != nil {
		return err
	}
	defer enc.Close()

	// Scaler if needed
	var scaler *Scaler
	if videoInfo.PixelFmt != passOpts.Video.PixelFormat && passOpts.Video.PixelFormat != PixelFormatNone {
		s, err := NewScalerWithConfig(ScalerConfig{
			SrcWidth:  videoInfo.Width,
			SrcHeight: videoInfo.Height,
			SrcFormat: videoInfo.PixelFmt,
			DstWidth:  passOpts.Video.Width,
			DstHeight: passOpts.Video.Height,
			DstFormat: passOpts.Video.PixelFormat,
			Flags:     ScaleBilinear,
		})
		if err != nil {
			return err
		}
		defer s.Close()
		scaler = s
	}

	for {
		frame, err := dec.DecodeVideo()
		if err != nil {
			if IsEOF(err) {
				break
			}
			return err
		}
		if frame.IsNil() {
			break
		}

		outFrame := frame
		if scaler != nil {
			sf, err := scaler.Scale(frame)
			if err != nil {
				return err
			}
			outFrame = sf
		}

		if err := enc.WriteVideoFrame(outFrame); err != nil {
			return err
		}
	}

	// Flush + trailer
	if err := enc.Close(); err != nil {
		return err
	}
	return nil
}
