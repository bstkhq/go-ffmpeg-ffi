//go:build amd64 || arm64

// Command check-layout compares layout_probe.c output with the Go ABI table.
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/bstkhq/go-ffmpeg-ffi/internal/abi"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	actual, err := readProbeOutput()
	if err != nil {
		return err
	}

	layout, err := abi.Detect(
		uint32(actual["libavutil"])<<16,
		uint32(actual["libavcodec"])<<16,
		uint32(actual["libavformat"])<<16,
	)
	if err != nil {
		return err
	}

	versions := map[string]uintptr{
		"libavfilter":   uintptr(layout.AVFilterMajor),
		"libavdevice":   uintptr(layout.AVDeviceMajor),
		"libswresample": uintptr(layout.SWResampleMajor),
		"libswscale":    uintptr(layout.SWScaleMajor),
	}
	for name, want := range versions {
		if got := actual[name]; got != want {
			return fmt.Errorf("%s major = %d, want %d for FFmpeg %d", name, got, want, layout.FFmpegMajor)
		}
	}

	expected := expectedOffsets(layout)
	const frameKeyFlagVersion = uintptr(58<<16 | 7<<8 | 100)
	if actual["libavutil_version"] >= frameKeyFlagVersion {
		delete(expected, "AVFrame.key_frame")
	}
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		got, ok := actual[key]
		if !ok {
			return fmt.Errorf("probe output is missing %s", key)
		}
		if got != expected[key] {
			return fmt.Errorf("FFmpeg %d layout mismatch: %s = %d, want %d", layout.FFmpegMajor, key, got, expected[key])
		}
	}

	fmt.Printf("verified FFmpeg %d public structure layout (%d fields)\n", layout.FFmpegMajor, len(expected))
	return nil
}

func readProbeOutput() (map[string]uintptr, error) {
	values := make(map[string]uintptr)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid probe line %q", line)
		}
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", key, err)
		}
		values[key] = uintptr(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read probe output: %w", err)
	}
	for _, key := range []string{"libavutil", "libavutil_version", "libavcodec", "libavformat"} {
		if _, ok := values[key]; !ok {
			return nil, fmt.Errorf("probe output is missing %s", key)
		}
	}
	return values, nil
}

func expectedOffsets(layout abi.Layout) map[string]uintptr {
	offsets := map[string]uintptr{
		"AVFrame.data":                     layout.Frame.Data,
		"AVFrame.linesize":                 layout.Frame.Linesize,
		"AVFrame.extended_data":            layout.Frame.ExtendedData,
		"AVFrame.width":                    layout.Frame.Width,
		"AVFrame.height":                   layout.Frame.Height,
		"AVFrame.nb_samples":               layout.Frame.NbSamples,
		"AVFrame.format":                   layout.Frame.Format,
		"AVFrame.flags":                    layout.Frame.Flags,
		"AVFrame.pts":                      layout.Frame.PTS,
		"AVFrame.sample_rate":              layout.Frame.SampleRate,
		"AVFrame.buf":                      layout.Frame.Buffer,
		"AVFrame.extended_buf":             layout.Frame.ExtendedBuffer,
		"AVFrame.nb_extended_buf":          layout.Frame.NbExtendedBuffer,
		"AVFrame.ch_layout":                layout.Frame.ChannelLayout,
		"AVCodecParameters.codec_type":     layout.CodecParameters.CodecType,
		"AVCodecParameters.codec_id":       layout.CodecParameters.CodecID,
		"AVCodecParameters.codec_tag":      layout.CodecParameters.CodecTag,
		"AVCodecParameters.extradata":      layout.CodecParameters.Extradata,
		"AVCodecParameters.extradata_size": layout.CodecParameters.ExtradataSize,
		"AVCodecParameters.format":         layout.CodecParameters.Format,
		"AVCodecParameters.width":          layout.CodecParameters.Width,
		"AVCodecParameters.height":         layout.CodecParameters.Height,
		"AVCodecParameters.sample_rate":    layout.CodecParameters.SampleRate,
		"AVCodecParameters.ch_layout":      layout.CodecParameters.ChannelLayout,
		"AVCodecContext.codec_type":        layout.CodecContext.CodecType,
		"AVCodecContext.codec_id":          layout.CodecContext.CodecID,
		"AVCodecContext.bit_rate":          layout.CodecContext.BitRate,
		"AVCodecContext.flags":             layout.CodecContext.Flags,
		"AVCodecContext.time_base":         layout.CodecContext.TimeBase,
		"AVCodecContext.width":             layout.CodecContext.Width,
		"AVCodecContext.height":            layout.CodecContext.Height,
		"AVCodecContext.gop_size":          layout.CodecContext.GOPSize,
		"AVCodecContext.pix_fmt":           layout.CodecContext.PixelFormat,
		"AVCodecContext.max_b_frames":      layout.CodecContext.MaxBFrames,
		"AVCodecContext.sample_rate":       layout.CodecContext.SampleRate,
		"AVCodecContext.sample_fmt":        layout.CodecContext.SampleFormat,
		"AVCodecContext.frame_size":        layout.CodecContext.FrameSize,
		"AVCodecContext.framerate":         layout.CodecContext.FrameRate,
		"AVCodecContext.hw_frames_ctx":     layout.CodecContext.HWFramesContext,
		"AVCodecContext.hw_device_ctx":     layout.CodecContext.HWDeviceContext,
		"AVCodecContext.ch_layout":         layout.CodecContext.ChannelLayout,
		"AVFormatContext.iformat":          layout.FormatContext.InputFormat,
		"AVFormatContext.oformat":          layout.FormatContext.OutputFormat,
		"AVFormatContext.pb":               layout.FormatContext.IOContext,
		"AVFormatContext.nb_streams":       layout.FormatContext.NumStreams,
		"AVFormatContext.streams":          layout.FormatContext.Streams,
		"AVFormatContext.duration":         layout.FormatContext.Duration,
		"AVFormatContext.bit_rate":         layout.FormatContext.BitRate,
		"AVFormatContext.flags":            layout.FormatContext.Flags,
		"AVFormatContext.nb_programs":      layout.FormatContext.NumPrograms,
		"AVFormatContext.programs":         layout.FormatContext.Programs,
		"AVFormatContext.nb_chapters":      layout.FormatContext.NumChapters,
		"AVFormatContext.chapters":         layout.FormatContext.Chapters,
		"AVFormatContext.metadata":         layout.FormatContext.Metadata,
		"AVFormatContext.probe_score":      layout.FormatContext.ProbeScore,
		"AVIOContext.buffer":               layout.IOContext.Buffer,
		"AVPacket.pts":                     layout.Packet.PTS,
		"AVPacket.dts":                     layout.Packet.DTS,
		"AVPacket.data":                    layout.Packet.Data,
		"AVPacket.size":                    layout.Packet.SizeField,
		"AVPacket.stream_index":            layout.Packet.StreamIndex,
		"AVPacket.flags":                   layout.Packet.Flags,
		"AVPacket.duration":                layout.Packet.Duration,
		"AVPacket.pos":                     layout.Packet.Position,
		"AVBSFContext.par_in":              layout.BSFContext.ParametersIn,
		"AVBSFContext.par_out":             layout.BSFContext.ParametersOut,
		"AVBSFContext.time_base_in":        layout.BSFContext.TimeBaseIn,
		"AVBSFContext.time_base_out":       layout.BSFContext.TimeBaseOut,
		"AVStream.index":                   layout.Stream.Index,
		"AVStream.id":                      layout.Stream.ID,
		"AVStream.codecpar":                layout.Stream.CodecParameters,
		"AVStream.time_base":               layout.Stream.TimeBase,
		"AVStream.metadata":                layout.Stream.Metadata,
		"AVStream.avg_frame_rate":          layout.Stream.AverageFrameRate,
		"AVChapter.id":                     layout.Chapter.ID,
		"AVChapter.time_base":              layout.Chapter.TimeBase,
		"AVChapter.start":                  layout.Chapter.Start,
		"AVChapter.end":                    layout.Chapter.End,
		"AVChapter.metadata":               layout.Chapter.Metadata,
		"AVProgram.id":                     layout.Program.ID,
		"AVProgram.stream_index":           layout.Program.StreamIndex,
		"AVProgram.nb_stream_indexes":      layout.Program.NumStreamIndexes,
		"AVProgram.metadata":               layout.Program.Metadata,
		"AVInputFormat.name":               layout.InputFormat.Name,
		"AVInputFormat.long_name":          layout.InputFormat.LongName,
		"AVOutputFormat.flags":             layout.OutputFormat.Flags,
		"AVCodec.name":                     layout.Codec.Name,
		"AVCodec.id":                       layout.Codec.ID,
		"AVCodecHWConfig.pix_fmt":          layout.CodecHWConfig.PixelFormat,
		"AVCodecHWConfig.methods":          layout.CodecHWConfig.Methods,
		"AVCodecHWConfig.device_type":      layout.CodecHWConfig.DeviceType,
		"AVFilterInOut.name":               layout.FilterInOut.Name,
		"AVFilterInOut.filter_ctx":         layout.FilterInOut.FilterContext,
		"AVFilterInOut.pad_idx":            layout.FilterInOut.PadIndex,
		"AVFilterInOut.next":               layout.FilterInOut.Next,
		"AVDictionaryEntry.key":            layout.DictionaryEntry.Key,
		"AVDictionaryEntry.value":          layout.DictionaryEntry.Value,
		"AVSubtitle.format":                layout.Subtitle.Format,
		"AVSubtitle.start_display_time":    layout.Subtitle.StartDisplayTime,
		"AVSubtitle.end_display_time":      layout.Subtitle.EndDisplayTime,
		"AVSubtitle.num_rects":             layout.Subtitle.NumRects,
		"AVSubtitle.rects":                 layout.Subtitle.Rects,
		"AVSubtitle.pts":                   layout.Subtitle.PTS,
		"sizeof(AVSubtitle)":               layout.Subtitle.Size,
		"AVSubtitleRect.x":                 layout.SubtitleRect.X,
		"AVSubtitleRect.y":                 layout.SubtitleRect.Y,
		"AVSubtitleRect.w":                 layout.SubtitleRect.Width,
		"AVSubtitleRect.h":                 layout.SubtitleRect.Height,
		"AVSubtitleRect.nb_colors":         layout.SubtitleRect.NumColors,
		"AVSubtitleRect.data":              layout.SubtitleRect.Data,
		"AVSubtitleRect.linesize":          layout.SubtitleRect.Linesize,
		"AVSubtitleRect.flags":             layout.SubtitleRect.Flags,
		"AVSubtitleRect.type":              layout.SubtitleRect.Type,
		"AVSubtitleRect.text":              layout.SubtitleRect.Text,
		"AVSubtitleRect.ass":               layout.SubtitleRect.ASS,
	}
	if layout.Frame.LegacyKeyFrame != 0 {
		offsets["AVFrame.key_frame"] = layout.Frame.LegacyKeyFrame
	}
	offsets["AVFormatContext.interrupt_callback"] = layout.FormatContext.InterruptCallback
	return offsets
}
