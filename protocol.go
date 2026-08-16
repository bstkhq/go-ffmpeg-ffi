//go:build amd64 || arm64

package ffmpeg

import (
	"context"
	"fmt"
	"time"
)

// ProtocolOptions configures network protocol behavior for streaming.
type ProtocolOptions struct {
	// Connection options
	Timeout        time.Duration // Connection timeout (default: 5s)
	ReconnectCount int           // Auto-reconnect attempts (0 = disabled)
	ReconnectDelay time.Duration // Delay between reconnects

	// Buffer options
	BufferSize int           // Protocol buffer size in bytes
	MaxDelay   time.Duration // Maximum delay for buffering

	// RTMP specific
	RTMPApp      string // RTMP application name
	RTMPPlayPath string // RTMP stream name
	RTMPLive     bool   // Live stream mode (disables seeking)

	// HTTP specific
	HTTPHeaders map[string]string // Custom HTTP headers
	HTTPCookies string            // HTTP cookies

	// TLS/SSL options
	TLSVerify bool   // Verify TLS certificates (default: true)
	TLSCert   string // Client certificate path
	TLSKey    string // Client key path

	// Additional FFmpeg options
	AVOptions map[string]string // Additional raw FFmpeg options
}

// NewNetworkDecoder opens a network stream with decoder and protocol-specific
// options.
// Supports RTMP, RTSP, HLS (HTTP), SRT, and other FFmpeg-supported protocols.
func NewNetworkDecoder(url string, decoderOpts *DecoderOptions, protocolOpts *ProtocolOptions) (*Decoder, error) {
	return NewNetworkDecoderContext(context.Background(), url, decoderOpts, protocolOpts)
}

// NewNetworkDecoderContext opens a network stream and allows connection,
// probing, and I/O to be canceled through ctx.
func NewNetworkDecoderContext(ctx context.Context, url string, decoderOpts *DecoderOptions, protocolOpts *ProtocolOptions) (*Decoder, error) {
	if ctx == nil {
		return nil, fmt.Errorf("ffmpeg: context cannot be nil")
	}
	return NewDecoderContext(ctx, url, mergeProtocolDecoderOptions(decoderOpts, protocolOpts))
}

func mergeProtocolDecoderOptions(decoderOpts *DecoderOptions, protocolOpts *ProtocolOptions) *DecoderOptions {
	if protocolOpts == nil {
		protocolOpts = &ProtocolOptions{}
	}
	decoderOpts = cloneDecoderOptions(decoderOpts)
	if decoderOpts.AVOptions == nil {
		decoderOpts.AVOptions = make(map[string]string)
	}

	// Protocol raw options override generic raw options. Typed protocol fields
	// are applied afterwards and therefore have the final say.
	for key, value := range protocolOpts.AVOptions {
		decoderOpts.AVOptions[key] = value
	}

	// Connection options
	if protocolOpts.Timeout > 0 {
		// FFmpeg uses microseconds for timeout
		decoderOpts.AVOptions["timeout"] = fmt.Sprintf("%d", protocolOpts.Timeout.Microseconds())
		// Also set connect timeout for TCP
		decoderOpts.AVOptions["stimeout"] = fmt.Sprintf("%d", protocolOpts.Timeout.Microseconds())
	}

	if protocolOpts.ReconnectCount > 0 {
		decoderOpts.AVOptions["reconnect"] = "1"
		decoderOpts.AVOptions["reconnect_streamed"] = "1"
		decoderOpts.AVOptions["reconnect_delay_max"] = fmt.Sprintf("%d", int(protocolOpts.ReconnectDelay.Seconds()))
		// Note: reconnect_at_eof and reconnect_on_network_error may also be useful
	}

	// Buffer options
	if protocolOpts.BufferSize > 0 {
		decoderOpts.AVOptions["buffer_size"] = fmt.Sprintf("%d", protocolOpts.BufferSize)
	}

	if protocolOpts.MaxDelay > 0 {
		decoderOpts.AVOptions["max_delay"] = fmt.Sprintf("%d", protocolOpts.MaxDelay.Microseconds())
	}

	// RTMP options
	if protocolOpts.RTMPApp != "" {
		decoderOpts.AVOptions["rtmp_app"] = protocolOpts.RTMPApp
	}
	if protocolOpts.RTMPPlayPath != "" {
		decoderOpts.AVOptions["rtmp_playpath"] = protocolOpts.RTMPPlayPath
	}
	if protocolOpts.RTMPLive {
		decoderOpts.AVOptions["rtmp_live"] = "live"
	}

	// HTTP options
	if len(protocolOpts.HTTPHeaders) > 0 {
		// Format headers as "Key: Value\r\nKey2: Value2\r\n"
		var headers string
		for k, v := range protocolOpts.HTTPHeaders {
			headers += fmt.Sprintf("%s: %s\r\n", k, v)
		}
		decoderOpts.AVOptions["headers"] = headers
	}
	if protocolOpts.HTTPCookies != "" {
		decoderOpts.AVOptions["cookies"] = protocolOpts.HTTPCookies
	}

	// TLS options
	if !protocolOpts.TLSVerify {
		// Only set if explicitly disabled - FFmpeg verifies by default
		decoderOpts.AVOptions["tls_verify"] = "0"
	}
	if protocolOpts.TLSCert != "" {
		decoderOpts.AVOptions["cert"] = protocolOpts.TLSCert
	}
	if protocolOpts.TLSKey != "" {
		decoderOpts.AVOptions["key"] = protocolOpts.TLSKey
	}

	return decoderOpts
}

// Common timeout presets for network streams
const (
	NetworkTimeoutShort  = 5 * time.Second
	NetworkTimeoutMedium = 15 * time.Second
	NetworkTimeoutLong   = 30 * time.Second
)
