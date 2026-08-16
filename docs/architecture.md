# Architecture

## Status

This document defines the current architecture for the go-ffmpeg-ffi hard fork.
FFmpeg 6-9 ABI selection, codec send/receive state machines, frame ownership,
callback containment, cancellable decoder I/O, and the Linux, macOS, Windows,
Android, and iOS loader boundaries are implemented. The public API continues to
evolve and is not compatible with the original ffgo API.

## Goals

- Provide a safe Go API over FFmpeg through PureGo. Desktop consumers remain
  CGO-free; mobile consumers use the CGO runtime required by PureGo.
- Officially support FFmpeg 6, 7, 8, and 9.
- Fail closed when a library set or structure layout is unsupported or mixed.
- Keep native artifacts small, auditable, versioned, and optional where
  practical.
- Make ownership, cancellation, and decoder/encoder state transitions explicit.
- Prove support with pinned integration and stress tests rather than inference.

## Non-goals

- FFmpeg 4 and older.
- FFmpeg 10 or current FFmpeg `master` before the next major ABI is released
  and qualified.
- Redistributing or choosing a native FFmpeg build for consumers. On iOS a
  consumer may link complete static FFmpeg archives into its signed app image.
- Hiding features that are absent from the installed FFmpeg build.
- Claiming complete FFmpeg API coverage.

## Compatibility policy

The supported ABI matrix is:

| FFmpeg | Core library majors (`avutil/avcodec/avformat`) | Policy |
| --- | --- | --- |
| 5.1 | `57/59/59` | Legacy candidate only for a supported system with no practical newer package, initially Debian 12. No new features. |
| 6.x | `58/60/60` | Official support target. |
| 7.x | `59/61/61` | Official support target. |
| 8.x | `60/62/62` | Official support target. |
| 9.x | `61/63/63` | Official support target, initially pinned to 9.0.1. |
| 10/master | future majors | Unsupported until an official release is available and qualified. |
| 4.x and older | older majors | Unsupported. |

FFmpeg names are convenient labels, but runtime compatibility is decided from
the complete tuple of loaded library majors. Optional libraries such as
`swscale`, `swresample`, `avfilter`, and `avdevice` must also match the selected
family.

Legacy FFmpeg 5 support is justified only while a relevant supported operating
system requires it. It must have a pinned CI job and an explicit sunset date;
otherwise it is removed rather than silently left untested.

## Desktop platform boundary

PureGo exposes the same function-registration API on macOS and Windows, but the
operating-system loaders are different. macOS opens `.dylib` files with
`dlopen`/`dlclose`; Windows opens `.dll` files with
`LoadLibrary`/`FreeLibrary`, while PureGo resolves registered functions with
`GetProcAddress`. This boundary belongs below the FFmpeg library-family
selection so version validation and the public API remain platform-independent.

macOS `amd64` and `arm64` are qualified on native runners. Windows `amd64` is
qualified on a native runner; Windows `arm64` is compile-qualified until a
native runner is available. Compile qualification covers every package and test
binary but is not runtime, codec, or hardware-acceleration evidence.

The desktop application remains CGO-free. FFmpeg and the optional C shim are
native shared libraries supplied separately. CI verifies the selected FFmpeg
headers against the Go ABI layout before running the shim and integration
suite. D3D11VA, DXVA2, and VideoToolbox remain runtime capabilities of the exact
FFmpeg build and device; desktop support does not imply that they are present or
meet a particular frame rate.

## Mobile platform boundary

Android and iOS keep the same PureGo registration layer and coherent FFmpeg
family validation, but require `CGO_ENABLED=1`. Android loads unversioned
`libav*.so` files from the APK linker namespace. iOS accepts signed
`libav*.framework` or `av*.framework` bundles embedded in the application and
can also resolve complete FFmpeg symbols that the application already linked
into its process image. It never searches desktop package-manager paths or
downloads executable code.

The library does not own AAR, APK, IPA, application-signing, or native FFmpeg
packaging. Ebitengine fixtures are downstream modules used to prove that the
public package can be bound into the artifact shape consumed by an application.
The minimum iOS deployment target is 13.0. Device/simulator compilation is not
evidence for VideoToolbox, audible audio, thermal stability, or sustained frame
rate; those claims require a named physical device and exact FFmpeg build.

## Layering

```text
Public Go API
    |
Decoder / encoder / muxer state machines
    |
Capabilities, ownership, and error translation
    |
+-------------------+-------------------+-------------------+
| PureGo functions  | Go ABI layouts    | Versioned C shim  |
+-------------------+-------------------+-------------------+
    |
Runtime-selected FFmpeg native libraries/frameworks
```

### Public API

The public API owns validation and presents Go conventions such as typed errors,
`context.Context`, explicit EOF, and deterministic cleanup. It must not expose
ABI offsets or shim selection.

### Media state machines

Decoder and encoder protocols must model FFmpeg's send/receive contract:

- drain output until `EAGAIN` before submitting more input;
- retain and retry input rejected with `EAGAIN`;
- submit a flush marker exactly once;
- drain delayed output until FFmpeg returns EOF;
- propagate flush, mux, and callback failures.

Convenience APIs must not discard packets belonging to another selected stream.

### Hardware decoder selection

Hardware acceleration is a policy of the normal `Decoder`, not an independent
demux/decode state machine. Every constructor that accepts `DecoderOptions`
preserves the same `HWDecoderConfig`; protocol and image-sequence settings are
merged separately from decoder policy.

The selector enumerates registered decoders for the stream codec ID and reads
their public `AVCodecHWConfig` entries. It considers only configurations that
accept an `AVHWDeviceContext`, applies a small platform preference order, and
tries the ordinary FFmpeg decoder first within each device type. This discovers
wrapper decoders such as Android's `h264_mediacodec` without hard-coding codec
names.

After attaching a compatible device context, libavcodec's default `get_format`
implementation selects the advertised hardware pixel format. No Go callback is
installed for this path. GPU-backed output is transferred into a reusable
system-memory frame, including frame properties, before it crosses the public
API. Wrapper decoders that already return CPU-backed frames do not incur that
transfer.

Automatic mode falls back to the regular software decoder if no candidate can
be opened. Required mode returns `ErrHardwareAccelerationUnavailable` instead.
Because some libavcodec accelerators initialize on the first packet, decoder
diagnostics distinguish pending, selected, active, and fallback states. Devices
created by the selector are decoder-owned; a caller-supplied `HWDevice` is
borrowed, while the codec context retains its own native buffer reference.

### PureGo binding layer

Normal exported C functions are called directly through PureGo. Signatures are
registered only after the runtime family is known. Missing optional symbols
become capabilities; missing required symbols produce an initialization error,
not a panic during package initialization.

## ABI strategy

A Go compatibility package can select layouts at runtime, but Go cannot inspect
C headers. Therefore every direct structure access must meet all of these rules:

1. It is represented in one central `internal/abi` layout.
2. Every used offset, and every size used for allocation, is generated or
   verified against the headers for every supported library major and
   architecture.
3. It has a focused test that compares the Go view with a C-compiled reference.
4. No package defines private magic offsets.
5. An unknown or inconsistent version tuple is rejected before field access.

The inventory includes more than `AVFrame`, `AVFormatContext`,
`AVCodecParameters`, and `AVCodecContext`. Packets, streams, formats, chapters,
programs, subtitles, filter nodes, bitstream-filter contexts, and dictionary
entries must also be audited.

## Why a C shim exists

The C shim is not a second implementation of FFmpeg. It is a narrow adapter for
cases where the C compiler has information or calling-convention support that a
Go-only wrapper does not:

- variadic functions and `va_list` callbacks;
- macros, inline helpers, and structs passed or returned by value;
- accessors whose correct field layout must be derived from FFmpeg headers;
- normalization of awkward C signatures into pointer-and-scalar calls.

A wrapper written only in Go can organize version-specific offsets, but it
cannot independently prove those offsets. A generated Go ABI table is the
preferred middle ground for simple field access: a small C generator reads the
headers during development/CI and emits data used by the CGO-free Go build.

The shim must obey these rules:

- one artifact per operating system, architecture, and FFmpeg library family;
- export its own API version and the FFmpeg library versions used to build it;
- load only after the core runtime family has been detected;
- reject every version mismatch before exposing functions;
- expose an explicit capability set;
- never use an unversioned prebuilt shim as a cross-version fallback.

Desktop users may consume prebuilt shims and still build the Go module with
`CGO_ENABLED=0`. Mobile PureGo builds require CGO. Building or regenerating a
shim requires a C compiler and the matching FFmpeg development headers.

## Runtime initialization

Initialization is explicit and deterministic:

1. Discover candidate core shared libraries without registering the full API.
2. Bind only version functions and read all core library versions.
3. Match the complete version tuple to a supported runtime family.
4. Select the corresponding Go ABI layout.
5. Load and validate the matching shim, if a requested capability needs it.
6. Register required and optional symbols for that family.
7. Publish an immutable runtime and capability report.

Partial loading must be rolled back where the platform permits it. A failed
initialization returns a diagnostic error identifying paths, versions, missing
symbols, and shim status.

## Memory and ownership

Every public frame, packet, buffer, and context is either owned or borrowed.
That status must be visible in the API contract.

- Owned values have one deterministic `Close` or `Free` responsibility.
- Borrowed values state exactly which operation invalidates them.
- Retained results require `Clone`, or the caller supplies storage through an
  `Into` method such as `DecodeInto`, `ScaleInto`, or `ResampleInto`.
- Native code must not retain an unpinned Go heap pointer after an FFI call.
- Plane access uses format descriptors and `extended_data`; it is not limited to
  YUV420P or eight audio planes.
- Pools accept only values leased by that pool and bound retained native memory.

`Frame.WrapBuffer` is an advanced zero-copy operation. It pins the byte-array
backing storage until FFmpeg releases its `AVBufferRef`; the release callback
then unpins the storage and removes its integer handle. The wrapped bytes must
not contain Go pointers.

Frames returned by decoder methods and slices returned by `FrameWrapper.Data`
are borrowed. The next operation on that decoder, or decoder close, may
invalidate them. `Copy` and `Clone` variants return owned values that the caller
must free. A `FramePool` accepts only a live lease issued by that same pool;
copied or already returned leases are rejected.

## Callbacks, cancellation, and concurrency

Opaque callback identifiers stay as integer-sized tokens across FFI; they are
never fabricated as Go pointers. Callback entry points recover panics, record
the original Go error, and return the correct FFmpeg error code.

`NewDecoderContext`, `ReadPacketContext`, `ReadFrameContext`, the context-aware
decode variants, and `SeekContext` use FFmpeg's ABI-verified interrupt callback.
`Decoder.Close` signals interruption before waiting for the decoder operation
lock.

Custom I/O prefers `ReadContext`, `WriteContext`, and `SeekContext` when they are
provided. Close cancels their context and waits for active callback trampolines
before freeing `AVIOContext`. Legacy `Read`, `Write`, and `Seek` remain
compatible, but a legacy callback that blocks indefinitely cannot be safely
forced to return; applications that require bounded shutdown must use the
context-aware forms. Callback panics are recovered. Synchronous custom-I/O
errors retain both the original Go cause and FFmpeg's numeric error, while an
asynchronous log callback panic is available through `TakeLogCallbackError`.

Independent decoders and encoders may run concurrently. Operations on one
decoder or encoder are serialized, but callers must not concurrently consume
borrowed frames or packets from that object. `Close` is the supported concurrent
lifecycle operation: it can interrupt an active decoder read or a cooperative
custom-I/O callback and then waits for cleanup. A `LogCallback` is process-wide
and may be called from FFmpeg-owned threads.

Focused lifecycle torture uses automatic hardware selection with the
audiovisual fixture, so hardware setup or software fallback and both audio and
video state machines run during concurrent open/decode/seek/close cycles. The
normal test suite runs a short pass; an extended local pass is:

```sh
FFMPEG_STRESS=1 go test -run TestDecoderLifecycleStress -count=1 .
FFMPEG_STRESS=1 go test -race -run TestDecoderLifecycleStress -count=1 .
```

`FFMPEG_STRESS_ITERATIONS` overrides the default extended iteration count. On
Linux the test checks handle, file-descriptor, Go-heap, and resident-memory
growth after cleanup.

## Capabilities and errors

Runtime capabilities come from loaded libraries, registered symbols, FFmpeg
configuration, and validated shim exports. They are never assumed.

All FFmpeg failures retain the numeric code and operation. Public sentinel
errors are used consistently with `errors.Is`; lower-level causes remain
available with `errors.As` and `Unwrap`.

## Distribution

The Go module and prebuilt shims are released together with a manifest containing
checksums, target platform, shim API version, and expected FFmpeg library majors.
No release is labelled as supporting a family that is absent from the pinned CI
matrix.

Release FFmpeg builds used for prebuilt shims are LGPL-only: GPL components and
`--enable-nonfree` are disabled. Each artifact is accompanied by the applicable
licenses, exact FFmpeg source and configuration, build instructions, and the
material required by that FFmpeg license. The Apache-2.0 license of the Go
wrapper does not relicense FFmpeg or remove those obligations.

## Provenance and assisted development

go-ffmpeg-ffi preserves the ffgo history, license, and contributor attribution.
The README and NOTICE identify the project as a hard fork from its first public
revision.

Current hard-fork development uses an Agentic Coding workflow with OpenAI Codex,
based on the GPT-5 model family. The agent can inspect and edit the repository,
run local builds and tests, use GitHub pull-request tooling, and exercise the
Android integration environment. Work is isolated on focused branches and
rechecked by the platform CI matrix.

A human maintainer defines scope, reviews the code and evidence, approves
privileged actions, decides whether to merge, and accepts responsibility for
the result. Material assistance is disclosed in pull requests and may be
recorded with `Assisted-by: OpenAI Codex`; no fictional identity is used in a
`Co-authored-by` trailer. Hosted model snapshots and available tools may evolve,
so a PR records any exact configuration that materially affects reproduction.
