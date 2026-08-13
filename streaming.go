//go:build amd64 || arm64

package ffmpeg

import (
	"errors"
	"net/url"
	"strings"
)

// NewStreamingEncoder creates an encoder configured for network streaming outputs (RTMP/UDP/RTP/etc).
//
// ffmpeg selects a sensible default muxer based on the URL scheme:
//   - rtmp/rtmps -> flv
//   - udp/srt    -> mpegts
//   - rtp        -> rtp
//   - rtsp       -> rtsp
//
// You can override the muxer via EncoderOptions.Format. Protocol and muxer
// options use the same raw FFmpeg dictionaries as NewEncoder.
func NewStreamingEncoder(outURL string, opts *EncoderOptions) (*Encoder, error) {
	if strings.TrimSpace(outURL) == "" {
		return nil, errors.New("ffmpeg: output url cannot be empty")
	}
	u, err := url.Parse(outURL)
	if err != nil || u.Scheme == "" {
		return nil, errors.New("ffmpeg: invalid streaming url")
	}

	encOpts := cloneEncoderOptions(opts)
	if encOpts == nil {
		return nil, errors.New("ffmpeg: EncoderOptions is required")
	}

	if encOpts.Format == "" {
		switch strings.ToLower(u.Scheme) {
		case "rtmp", "rtmps":
			encOpts.Format = "flv"
		case "udp", "srt":
			encOpts.Format = "mpegts"
		case "rtp":
			encOpts.Format = "rtp"
		case "rtsp":
			encOpts.Format = "rtsp"
		default:
			return nil, errors.New("ffmpeg: unsupported streaming scheme")
		}
	}

	return NewEncoder(outURL, encOpts)
}
