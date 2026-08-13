//go:build amd64 || arm64

// Package abi describes the public FFmpeg structure layouts used by ffgo.
//
// FFmpeg keeps these structures source-compatible within a release branch, but
// does not promise that their binary layout remains stable across major
// releases. ffgo accesses a small subset of their fields through purego, so it
// must select offsets from the versions of the libraries actually loaded.
package abi

import (
	"errors"
	"fmt"
)

// ErrUnsupported is returned for an unsupported or internally inconsistent
// set of FFmpeg shared libraries.
var ErrUnsupported = errors.New("ffgo: unsupported FFmpeg ABI")

// Layout contains every public FFmpeg structure layout used directly by Go.
type Layout struct {
	FFmpegMajor     int
	AVUtilMajor     int
	AVCodecMajor    int
	AVFormatMajor   int
	SWScaleMajor    int
	SWResampleMajor int
	AVFilterMajor   int
	AVDeviceMajor   int

	Frame           FrameLayout
	FormatContext   FormatContextLayout
	IOContext       IOContextLayout
	CodecParameters CodecParametersLayout
	CodecContext    CodecContextLayout
	Packet          PacketLayout
	BSFContext      BSFContextLayout
	Stream          StreamLayout
	Chapter         ChapterLayout
	Program         ProgramLayout
	Codec           CodecLayout
	InputFormat     InputFormatLayout
	OutputFormat    OutputFormatLayout
	FilterInOut     FilterInOutLayout
	DictionaryEntry DictionaryEntryLayout
	ChannelLayout   ChannelLayoutLayout
	Subtitle        SubtitleLayout
	SubtitleRect    SubtitleRectLayout
}

// FrameLayout contains offsets into AVFrame.
type FrameLayout struct {
	Data, Linesize, ExtendedData                 uintptr
	Width, Height, NbSamples, Format, Flags, PTS uintptr
	LegacyKeyFrame                               uintptr
	SampleRate, Buffer, ExtendedBuffer           uintptr
	NbExtendedBuffer, ChannelLayout              uintptr
}

// FormatContextLayout contains offsets into AVFormatContext.
type FormatContextLayout struct {
	InputFormat, OutputFormat, IOContext, NumStreams, Streams uintptr
	Duration, BitRate, Flags                                  uintptr
	NumPrograms, Programs, NumChapters, Chapters, Metadata    uintptr
	ProbeScore, InterruptCallback                             uintptr
}

// IOContextLayout contains offsets into AVIOContext.
type IOContextLayout struct {
	Buffer uintptr
}

// CodecParametersLayout contains offsets into AVCodecParameters.
type CodecParametersLayout struct {
	CodecType, CodecID, CodecTag, Extradata, ExtradataSize uintptr
	Format, Width, Height, SampleRate, ChannelLayout       uintptr
}

// CodecContextLayout contains offsets into AVCodecContext.
type CodecContextLayout struct {
	CodecType, CodecID, BitRate, Flags, TimeBase    uintptr
	Width, Height, GOPSize, PixelFormat, MaxBFrames uintptr
	SampleRate, SampleFormat, FrameSize, FrameRate  uintptr
	HWFramesContext, HWDeviceContext, ChannelLayout uintptr
}

// PacketLayout contains offsets into AVPacket.
type PacketLayout struct {
	PTS, DTS, Data, SizeField, StreamIndex, Flags uintptr
	Duration, Position                            uintptr
}

// BSFContextLayout contains offsets into AVBSFContext.
type BSFContextLayout struct {
	ParametersIn, ParametersOut, TimeBaseIn, TimeBaseOut uintptr
}

// StreamLayout contains offsets into AVStream.
type StreamLayout struct {
	Index, ID, CodecParameters, TimeBase uintptr
	Metadata, AverageFrameRate           uintptr
}

// ChapterLayout contains offsets into AVChapter.
type ChapterLayout struct {
	ID, TimeBase, Start, End, Metadata uintptr
}

// ProgramLayout contains offsets into AVProgram.
type ProgramLayout struct {
	ID, StreamIndex, NumStreamIndexes, Metadata uintptr
}

// CodecLayout contains offsets into AVCodec.
type CodecLayout struct {
	Name uintptr
}

// InputFormatLayout contains offsets into AVInputFormat.
type InputFormatLayout struct {
	Name, LongName uintptr
}

// OutputFormatLayout contains offsets into AVOutputFormat.
type OutputFormatLayout struct {
	Flags uintptr
}

// FilterInOutLayout contains offsets into AVFilterInOut.
type FilterInOutLayout struct {
	Name, FilterContext, PadIndex, Next uintptr
}

// DictionaryEntryLayout contains offsets into AVDictionaryEntry.
type DictionaryEntryLayout struct {
	Key, Value uintptr
}

// ChannelLayoutLayout contains offsets into AVChannelLayout.
type ChannelLayoutLayout struct {
	NumChannels uintptr
}

// SubtitleLayout contains offsets into AVSubtitle.
type SubtitleLayout struct {
	Format, StartDisplayTime, EndDisplayTime uintptr
	NumRects, Rects, PTS, Size               uintptr
}

// SubtitleRectLayout contains offsets into AVSubtitleRect.
type SubtitleRectLayout struct {
	X, Y, Width, Height, NumColors uintptr
	Data, Linesize, Flags, Type    uintptr
	Text, ASS                      uintptr
}

var (
	ffmpeg6 = makeFFmpeg6Layout()
	ffmpeg7 = makeFFmpeg7Layout()
	ffmpeg8 = makeFFmpeg8Layout()
	ffmpeg9 = makeFFmpeg9Layout()

	layouts = map[[3]int]Layout{
		{58, 60, 60}: ffmpeg6,
		{59, 61, 61}: ffmpeg7,
		{60, 62, 62}: ffmpeg8,
		{61, 63, 63}: ffmpeg9,
	}
)

// commonLayout contains offsets verified to be identical in the pinned public
// headers for the FFmpeg 6.0, 6.1, 7.0, 7.1, 8.0, 8.1, and 9.0 release lines.
// Family-specific constructors fill every structure that changed layout.
func commonLayout() Layout {
	return Layout{
		IOContext: IOContextLayout{
			Buffer: 8,
		},
		Packet: PacketLayout{
			PTS: 8, DTS: 16, Data: 24, SizeField: 32,
			StreamIndex: 36, Flags: 40, Duration: 64, Position: 72,
		},
		BSFContext: BSFContextLayout{
			ParametersIn: 24, ParametersOut: 32, TimeBaseIn: 40, TimeBaseOut: 48,
		},
		Chapter: ChapterLayout{
			ID: 0, TimeBase: 8, Start: 16, End: 24, Metadata: 32,
		},
		Program: ProgramLayout{
			ID: 0, StreamIndex: 16, NumStreamIndexes: 24, Metadata: 32,
		},
		Codec:        CodecLayout{Name: 0},
		InputFormat:  InputFormatLayout{Name: 0, LongName: 8},
		OutputFormat: OutputFormatLayout{Flags: 44},
		FilterInOut: FilterInOutLayout{
			Name: 0, FilterContext: 8, PadIndex: 16, Next: 24,
		},
		DictionaryEntry: DictionaryEntryLayout{Key: 0, Value: 8},
		ChannelLayout:   ChannelLayoutLayout{NumChannels: 4},
		Subtitle: SubtitleLayout{
			Format: 0, StartDisplayTime: 4, EndDisplayTime: 8,
			NumRects: 12, Rects: 16, PTS: 24, Size: 32,
		},
	}
}

func makeFFmpeg6Layout() Layout {
	layout := commonLayout()
	layout.FFmpegMajor = 6
	layout.AVUtilMajor = 58
	layout.AVCodecMajor = 60
	layout.AVFormatMajor = 60
	layout.SWScaleMajor = 7
	layout.SWResampleMajor = 4
	layout.AVFilterMajor = 9
	layout.AVDeviceMajor = 60
	layout.Frame = FrameLayout{
		Data: 0, Linesize: 64, ExtendedData: 96,
		Width: 104, Height: 108, NbSamples: 112, Format: 116,
		Flags: 316, PTS: 136, LegacyKeyFrame: 120, SampleRate: 208,
		Buffer: 224, ExtendedBuffer: 288, NbExtendedBuffer: 296,
		ChannelLayout: 448,
	}
	layout.FormatContext = FormatContextLayout{
		InputFormat: 8, OutputFormat: 16, IOContext: 32,
		NumStreams: 44, Streams: 48, Duration: 72, BitRate: 80,
		Flags: 96, NumPrograms: 132, Programs: 136,
		NumChapters: 164, Chapters: 168, Metadata: 176,
		ProbeScore: 300, InterruptCallback: 200,
	}
	layout.CodecParameters = CodecParametersLayout{
		CodecType: 0, CodecID: 4, CodecTag: 8, Extradata: 16,
		ExtradataSize: 24, Format: 28, Width: 56, Height: 60,
		SampleRate: 116, ChannelLayout: 144,
	}
	layout.CodecContext = CodecContextLayout{
		CodecType: 12, CodecID: 24, BitRate: 56, Flags: 76,
		TimeBase: 100, Width: 116, Height: 120, GOPSize: 132,
		PixelFormat: 136, MaxBFrames: 160, SampleRate: 352,
		SampleFormat: 360, FrameSize: 364, FrameRate: 704,
		HWFramesContext: 840, HWDeviceContext: 864, ChannelLayout: 912,
	}
	layout.Stream = StreamLayout{
		Index: 8, ID: 12, CodecParameters: 16, TimeBase: 32,
		Metadata: 80, AverageFrameRate: 88,
	}
	layout.SubtitleRect = SubtitleRectLayout{
		X: 0, Y: 4, Width: 8, Height: 12, NumColors: 16,
		Data: 24, Linesize: 56, Flags: 96, Type: 72,
		Text: 80, ASS: 88,
	}
	return layout
}

func makeFFmpeg7Layout() Layout {
	layout := commonLayout()
	layout.FFmpegMajor = 7
	layout.AVUtilMajor = 59
	layout.AVCodecMajor = 61
	layout.AVFormatMajor = 61
	layout.SWScaleMajor = 8
	layout.SWResampleMajor = 5
	layout.AVFilterMajor = 10
	layout.AVDeviceMajor = 61
	layout.Frame = FrameLayout{
		Data: 0, Linesize: 64, ExtendedData: 96,
		Width: 104, Height: 108, NbSamples: 112, Format: 116,
		Flags: 292, PTS: 136, SampleRate: 192,
		Buffer: 200, ExtendedBuffer: 264, NbExtendedBuffer: 272,
		ChannelLayout: 408,
	}
	layout.FormatContext = FormatContextLayout{
		InputFormat: 8, OutputFormat: 16, IOContext: 32,
		NumStreams: 44, Streams: 48, Duration: 104, BitRate: 112,
		Flags: 128, NumPrograms: 164, Programs: 168,
		NumChapters: 72, Chapters: 80, Metadata: 192,
		ProbeScore: 324, InterruptCallback: 216,
	}
	layout.CodecParameters = CodecParametersLayout{
		CodecType: 0, CodecID: 4, CodecTag: 8, Extradata: 16,
		ExtradataSize: 24, Format: 44, Width: 72, Height: 76,
		SampleRate: 152, ChannelLayout: 128,
	}
	layout.CodecContext = CodecContextLayout{
		CodecType: 12, CodecID: 24, BitRate: 56, Flags: 64,
		TimeBase: 84, Width: 116, Height: 120, GOPSize: 332,
		PixelFormat: 140, MaxBFrames: 200, SampleRate: 344,
		SampleFormat: 348, FrameSize: 376, FrameRate: 100,
		HWFramesContext: 552, HWDeviceContext: 560, ChannelLayout: 352,
	}
	layout.Stream = StreamLayout{
		Index: 8, ID: 12, CodecParameters: 16, TimeBase: 32,
		Metadata: 80, AverageFrameRate: 88,
	}
	layout.SubtitleRect = SubtitleRectLayout{
		X: 0, Y: 4, Width: 8, Height: 12, NumColors: 16,
		Data: 24, Linesize: 56, Flags: 72, Type: 76,
		Text: 80, ASS: 88,
	}
	return layout
}

func makeFFmpeg8Layout() Layout {
	layout := commonLayout()
	layout.FFmpegMajor = 8
	layout.AVUtilMajor = 60
	layout.AVCodecMajor = 62
	layout.AVFormatMajor = 62
	layout.SWScaleMajor = 9
	layout.SWResampleMajor = 6
	layout.AVFilterMajor = 11
	layout.AVDeviceMajor = 62
	layout.Frame = FrameLayout{
		Data: 0, Linesize: 64, ExtendedData: 96,
		Width: 104, Height: 108, NbSamples: 112, Format: 116,
		Flags: 276, PTS: 136, SampleRate: 180,
		Buffer: 184, ExtendedBuffer: 248, NbExtendedBuffer: 256,
		ChannelLayout: 384,
	}
	layout.FormatContext = FormatContextLayout{
		InputFormat: 8, OutputFormat: 16, IOContext: 32,
		NumStreams: 44, Streams: 48, Duration: 104, BitRate: 112,
		Flags: 128, NumPrograms: 164, Programs: 168,
		NumChapters: 72, Chapters: 80, Metadata: 192,
		ProbeScore: 324, InterruptCallback: 216,
	}
	layout.CodecParameters = CodecParametersLayout{
		CodecType: 0, CodecID: 4, CodecTag: 8, Extradata: 16,
		ExtradataSize: 24, Format: 44, Width: 72, Height: 76,
		SampleRate: 152, ChannelLayout: 128,
	}
	layout.CodecContext = CodecContextLayout{
		CodecType: 12, CodecID: 24, BitRate: 56, Flags: 64,
		TimeBase: 84, Width: 112, Height: 116, GOPSize: 332,
		PixelFormat: 136, MaxBFrames: 200, SampleRate: 344,
		SampleFormat: 348, FrameSize: 376, FrameRate: 100,
		HWFramesContext: 552, HWDeviceContext: 560, ChannelLayout: 352,
	}
	layout.Stream = StreamLayout{
		Index: 8, ID: 12, CodecParameters: 16, TimeBase: 32,
		Metadata: 80, AverageFrameRate: 88,
	}
	layout.SubtitleRect = SubtitleRectLayout{
		X: 0, Y: 4, Width: 8, Height: 12, NumColors: 16,
		Data: 24, Linesize: 56, Flags: 72, Type: 76,
		Text: 80, ASS: 88,
	}
	return layout
}

func makeFFmpeg9Layout() Layout {
	// FFmpeg 9.0.1 retains the FFmpeg 8 offsets for every public structure
	// field accessed by Go. Keep a distinct family so the loader still requires
	// a coherent 61/63/63 runtime tuple and validates all optional libraries.
	layout := makeFFmpeg8Layout()
	layout.FFmpegMajor = 9
	layout.AVUtilMajor = 61
	layout.AVCodecMajor = 63
	layout.AVFormatMajor = 63
	layout.SWScaleMajor = 10
	layout.SWResampleMajor = 7
	layout.AVFilterMajor = 12
	layout.AVDeviceMajor = 63
	return layout
}

// Detect selects a layout from the runtime library versions. Version values
// use FFmpeg's AV_VERSION_INT representation.
func Detect(avutilVersion, avcodecVersion, avformatVersion uint32) (Layout, error) {
	key := [3]int{major(avutilVersion), major(avcodecVersion), major(avformatVersion)}
	if layout, ok := layouts[key]; ok {
		return layout, nil
	}
	return Layout{}, fmt.Errorf(
		"%w: libavutil %d, libavcodec %d, libavformat %d; supported combinations are FFmpeg 6 (58/60/60), FFmpeg 7 (59/61/61), FFmpeg 8 (60/62/62), and FFmpeg 9 (61/63/63)",
		ErrUnsupported, key[0], key[1], key[2],
	)
}

// ForFFmpegMajor returns the layout for a supported FFmpeg release family.
func ForFFmpegMajor(ffmpegMajor int) (Layout, bool) {
	switch ffmpegMajor {
	case 6:
		return ffmpeg6, true
	case 7:
		return ffmpeg7, true
	case 8:
		return ffmpeg8, true
	case 9:
		return ffmpeg9, true
	default:
		return Layout{}, false
	}
}

// Supported returns supported layouts in loader preference order.
func Supported() []Layout {
	return []Layout{ffmpeg9, ffmpeg8, ffmpeg7, ffmpeg6}
}

// LibraryMajor returns the shared-library major expected by this FFmpeg ABI.
func (l Layout) LibraryMajor(name string) (int, bool) {
	switch name {
	case "avutil":
		return l.AVUtilMajor, true
	case "avcodec":
		return l.AVCodecMajor, true
	case "avformat":
		return l.AVFormatMajor, true
	case "swscale":
		return l.SWScaleMajor, true
	case "swresample":
		return l.SWResampleMajor, true
	case "avfilter":
		return l.AVFilterMajor, true
	case "avdevice":
		return l.AVDeviceMajor, true
	default:
		return 0, false
	}
}

func major(version uint32) int {
	return int(version >> 16)
}
