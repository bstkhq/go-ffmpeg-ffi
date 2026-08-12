/*
 * Print the public FFmpeg structure offsets used by go-ffmpeg-ffi.
 *
 * Compile this file against the headers for each supported FFmpeg ABI. The
 * output is intentionally stable so it can be compared with internal/abi.
 */
#include <stddef.h>
#include <stdio.h>

#include <libavcodec/avcodec.h>
#include <libavcodec/bsf.h>
#include <libavcodec/codec.h>
#include <libavcodec/packet.h>
#include <libavdevice/version.h>
#include <libavfilter/avfilter.h>
#include <libavformat/avformat.h>
#include <libavutil/dict.h>
#include <libavutil/frame.h>
#include <libswresample/version.h>
#include <libswscale/version.h>

#define FIELD(type, field) printf(#type "." #field "=%zu\n", offsetof(type, field))
#define SIZE(type) printf("sizeof(" #type ")=%zu\n", sizeof(type))

int main(void) {
    printf("libavutil=%d\n", LIBAVUTIL_VERSION_MAJOR);
    printf("libavutil_version=%u\n", LIBAVUTIL_VERSION_INT);
    printf("libavcodec=%d\n", LIBAVCODEC_VERSION_MAJOR);
    printf("libavformat=%d\n", LIBAVFORMAT_VERSION_MAJOR);
    printf("libavfilter=%d\n", LIBAVFILTER_VERSION_MAJOR);
    printf("libavdevice=%d\n", LIBAVDEVICE_VERSION_MAJOR);
    printf("libswresample=%d\n", LIBSWRESAMPLE_VERSION_MAJOR);
    printf("libswscale=%d\n", LIBSWSCALE_VERSION_MAJOR);

    FIELD(AVFrame, data);
    FIELD(AVFrame, linesize);
    FIELD(AVFrame, extended_data);
    FIELD(AVFrame, width);
    FIELD(AVFrame, height);
    FIELD(AVFrame, nb_samples);
    FIELD(AVFrame, format);
#if LIBAVUTIL_VERSION_INT < AV_VERSION_INT(58, 7, 100)
    FIELD(AVFrame, key_frame);
#endif
    FIELD(AVFrame, flags);
    FIELD(AVFrame, pts);
    FIELD(AVFrame, sample_rate);
    FIELD(AVFrame, buf);
    FIELD(AVFrame, extended_buf);
    FIELD(AVFrame, nb_extended_buf);
    FIELD(AVFrame, ch_layout);
    SIZE(AVFrame);

    FIELD(AVCodecParameters, codec_type);
    FIELD(AVCodecParameters, codec_id);
    FIELD(AVCodecParameters, codec_tag);
    FIELD(AVCodecParameters, extradata);
    FIELD(AVCodecParameters, extradata_size);
    FIELD(AVCodecParameters, format);
    FIELD(AVCodecParameters, width);
    FIELD(AVCodecParameters, height);
    FIELD(AVCodecParameters, sample_rate);
    FIELD(AVCodecParameters, ch_layout);
    SIZE(AVCodecParameters);

    FIELD(AVCodecContext, codec_type);
    FIELD(AVCodecContext, codec_id);
    FIELD(AVCodecContext, bit_rate);
    FIELD(AVCodecContext, flags);
    FIELD(AVCodecContext, time_base);
    FIELD(AVCodecContext, width);
    FIELD(AVCodecContext, height);
    FIELD(AVCodecContext, gop_size);
    FIELD(AVCodecContext, pix_fmt);
    FIELD(AVCodecContext, max_b_frames);
    FIELD(AVCodecContext, sample_rate);
    FIELD(AVCodecContext, sample_fmt);
    FIELD(AVCodecContext, frame_size);
    FIELD(AVCodecContext, framerate);
    FIELD(AVCodecContext, hw_frames_ctx);
    FIELD(AVCodecContext, hw_device_ctx);
    FIELD(AVCodecContext, ch_layout);
    SIZE(AVCodecContext);

    FIELD(AVFormatContext, iformat);
    FIELD(AVFormatContext, oformat);
    FIELD(AVFormatContext, pb);
    FIELD(AVFormatContext, nb_streams);
    FIELD(AVFormatContext, streams);
    FIELD(AVFormatContext, duration);
    FIELD(AVFormatContext, bit_rate);
    FIELD(AVFormatContext, flags);
    FIELD(AVFormatContext, nb_programs);
    FIELD(AVFormatContext, programs);
    FIELD(AVFormatContext, nb_chapters);
    FIELD(AVFormatContext, chapters);
    FIELD(AVFormatContext, metadata);
    FIELD(AVFormatContext, probe_score);
    FIELD(AVFormatContext, interrupt_callback);
    SIZE(AVFormatContext);

    FIELD(AVIOContext, buffer);

    FIELD(AVPacket, pts);
    FIELD(AVPacket, dts);
    FIELD(AVPacket, data);
    FIELD(AVPacket, size);
    FIELD(AVPacket, stream_index);
    FIELD(AVPacket, flags);
    FIELD(AVPacket, duration);
    FIELD(AVPacket, pos);
    SIZE(AVPacket);

    FIELD(AVBSFContext, par_in);
    FIELD(AVBSFContext, par_out);
    FIELD(AVBSFContext, time_base_in);
    FIELD(AVBSFContext, time_base_out);
    SIZE(AVBSFContext);

    FIELD(AVStream, index);
    FIELD(AVStream, id);
    FIELD(AVStream, codecpar);
    FIELD(AVStream, time_base);
    FIELD(AVStream, metadata);
    FIELD(AVStream, avg_frame_rate);
    SIZE(AVStream);

    FIELD(AVChapter, id);
    FIELD(AVChapter, time_base);
    FIELD(AVChapter, start);
    FIELD(AVChapter, end);
    FIELD(AVChapter, metadata);
    SIZE(AVChapter);

    FIELD(AVProgram, id);
    FIELD(AVProgram, stream_index);
    FIELD(AVProgram, nb_stream_indexes);
    FIELD(AVProgram, metadata);
    SIZE(AVProgram);

    FIELD(AVInputFormat, name);
    FIELD(AVInputFormat, long_name);
    FIELD(AVOutputFormat, flags);
    FIELD(AVCodec, name);

    FIELD(AVFilterInOut, name);
    FIELD(AVFilterInOut, filter_ctx);
    FIELD(AVFilterInOut, pad_idx);
    FIELD(AVFilterInOut, next);
    SIZE(AVFilterInOut);

    FIELD(AVDictionaryEntry, key);
    FIELD(AVDictionaryEntry, value);
    SIZE(AVDictionaryEntry);

    FIELD(AVSubtitle, format);
    FIELD(AVSubtitle, start_display_time);
    FIELD(AVSubtitle, end_display_time);
    FIELD(AVSubtitle, num_rects);
    FIELD(AVSubtitle, rects);
    FIELD(AVSubtitle, pts);
    SIZE(AVSubtitle);

    FIELD(AVSubtitleRect, x);
    FIELD(AVSubtitleRect, y);
    FIELD(AVSubtitleRect, w);
    FIELD(AVSubtitleRect, h);
    FIELD(AVSubtitleRect, nb_colors);
    FIELD(AVSubtitleRect, data);
    FIELD(AVSubtitleRect, linesize);
    FIELD(AVSubtitleRect, flags);
    FIELD(AVSubtitleRect, type);
    FIELD(AVSubtitleRect, text);
    FIELD(AVSubtitleRect, ass);
    SIZE(AVSubtitleRect);

    return 0;
}
