# Architecture

## Status

This document defines the target architecture for the go-ffmpeg-ffi hard fork.
The current source tree is transitional: it includes the known FFmpeg 6/7
corrections, but not every rule below has been implemented yet.

## Goals

- Provide a safe Go API over dynamically loaded FFmpeg libraries without CGO.
- Officially support FFmpeg 6, 7, and 8.
- Fail closed when a library set or structure layout is unsupported or mixed.
- Keep native artifacts small, auditable, versioned, and optional where
  practical.
- Make ownership, cancellation, and decoder/encoder state transitions explicit.
- Prove support with pinned integration and stress tests rather than inference.

## Non-goals

- FFmpeg 4 and older.
- FFmpeg 9 or current FFmpeg `master` before FFmpeg 9 is released and its ABI is
  fixed.
- Static linking of FFmpeg.
- Hiding features that are absent from the installed FFmpeg build.
- Claiming complete FFmpeg API coverage.

## Compatibility policy

The desired support matrix is:

| FFmpeg | Core library majors (`avutil/avcodec/avformat`) | Policy |
| --- | --- | --- |
| 5.1 | `57/59/59` | Legacy candidate only for a supported system with no practical newer package, initially Debian 12. No new features. |
| 6.x | `58/60/60` | Official support target. |
| 7.x | `59/61/61` | Official support target. |
| 8.x | `60/62/62` | Official support target. |
| 9/master | future majors | Unsupported until an official FFmpeg 9 release is available. |
| 4.x and older | older majors | Unsupported. |

FFmpeg names are convenient labels, but runtime compatibility is decided from
the complete tuple of loaded library majors. Optional libraries such as
`swscale`, `swresample`, `avfilter`, and `avdevice` must also match the selected
family.

Legacy FFmpeg 5 support is justified only while a relevant supported operating
system requires it. It must have a pinned CI job and an explicit sunset date;
otherwise it is removed rather than silently left untested.

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
Runtime-selected FFmpeg shared libraries
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

### PureGo binding layer

Normal exported C functions are called directly through PureGo. Signatures are
registered only after the runtime family is known. Missing optional symbols
become capabilities; missing required symbols produce an initialization error,
not a panic during package initialization.

## ABI strategy

A Go compatibility package can select layouts at runtime, but Go cannot inspect
C headers. Therefore every direct structure access must meet all of these rules:

1. It is represented in one central `internal/abi` layout.
2. Its offset and size are generated or verified against the headers for every
   supported library major and architecture.
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

End users may consume prebuilt shims and still build the Go module with
`CGO_ENABLED=0`. Building or regenerating a shim requires a C compiler and the
matching FFmpeg development headers.

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
- Native code must not retain an ordinary Go heap pointer after an FFI call.
- Plane access uses format descriptors and `extended_data`; it is not limited to
  YUV420P or eight audio planes.
- Pools accept only values leased by that pool and bound retained native memory.

Zero-copy APIs, if retained, are explicitly unsafe/advanced and use a mechanism
whose lifetime can be verified under the race detector and stress tests.

## Callbacks, cancellation, and concurrency

Opaque callback identifiers stay as integer-sized tokens across FFI; they are
never fabricated as Go pointers. Callback entry points recover panics, record
the original Go error, and return the correct FFmpeg error code.

Blocking open/read operations have context-aware variants backed by FFmpeg's
interrupt callback. Closing a resource must be able to cancel blocked I/O.

Objects are not concurrent-safe unless their documentation explicitly says so.
Internal mutexes protect lifecycle invariants but do not imply that borrowed
frames or packets can be used concurrently.

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

OpenAI Codex may assist with implementation and review. A human maintainer
reviews and accepts responsibility for every change. Material assistance is
recorded with `Assisted-by: OpenAI Codex`; no fictional identity is used in a
`Co-authored-by` trailer.
