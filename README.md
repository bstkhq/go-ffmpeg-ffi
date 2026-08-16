# go-ffmpeg-ffi

Go bindings for dynamically loaded FFmpeg libraries on Linux, macOS, Windows,
Android, and iOS, built with
[PureGo](https://github.com/ebitengine/purego).

Based on [ffgo](https://github.com/obinnaokechukwu/ffgo), with its history and
attribution preserved, but **not API-compatible with ffgo**.

## Purpose and maturity

go-ffmpeg-ffi aims to provide one functional FFmpeg backend across the operating
systems used by our applications. It is being developed as the media backend for
[go-avebi](https://github.com/erparts/go-avebi), which Bombastik! uses in several
projects.

Our primary use case is playback. Library loading, probing, demuxing, video and
audio decoding, resampling, seeking, EOF, cancellation, and repeated lifecycle
paths receive the most integration and stress testing. Encoding, muxing,
filtering, capture, subtitles, and hardware acceleration are available, but have
less production use and less exhaustive validation. Contributions that improve
those areas are especially welcome.

Codec, container, filter, device, and hardware-acceleration availability still
depends on the FFmpeg build and the device. Operating-system support alone does
not guarantee MediaCodec, VideoToolbox, D3D11VA, a particular codec profile, or
60 FPS.

## Compatibility

The loader supports coherent FFmpeg 6.x, 7.x, 8.x, and 9.x shared-library
families. CI currently builds and tests these releases on Linux amd64:

| Release line | Pinned test release |
| --- | --- |
| 6.0 / 6.1 | 6.0.1 / 6.1.6 |
| 7.0 / 7.1 | 7.0.3 / 7.1.5 |
| 8.0 / 8.1 | 8.0.3 / 8.1.2 |
| 9.0 | 9.0.1 |

FFmpeg 4.x and older, development snapshots, and future major versions are
rejected until explicitly qualified. See the full
[support and validation matrix](docs/support.md) for the distinction between
compilation, runtime integration, emulator evidence, and physical-device
qualification.

| Operating system | Current validation |
| --- | --- |
| Linux | Native runtime and the complete FFmpeg 6–9 release matrix on amd64; arm64 source and shim support. |
| macOS | Native runtime on Intel and Apple Silicon. |
| Windows | Native runtime with FFmpeg 9.0.1 on amd64; complete compilation on arm64. |
| Android | API 33+ compilation on arm64 and x86-64; prolonged H.264/AAC playback and lifecycle tests on an API 33 x86-64 emulator. |
| iOS | iOS 13+ device and simulator compilation plus Ebitengine XCFramework binding; physical-device runtime qualification is pending. |

Desktop applications can remain CGO-free. Android and iOS require
`CGO_ENABLED=1` and their native NDK or Xcode toolchain, as required by PureGo
on mobile.

## Install and decode

Install the Go module:

```sh
go get github.com/bstkhq/go-ffmpeg-ffi
```

The imported package is named `ffmpeg`:

```go
package main

import (
	"errors"
	"io"
	"log"

	"github.com/bstkhq/go-ffmpeg-ffi"
)

func main() {
	decoder, err := ffmpeg.NewDecoder("video.mp4", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer decoder.Close()

	for {
		frame, err := decoder.DecodeVideo()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		// frame is borrowed and remains valid until the next decode call.
		log.Printf("frame: %dx%d pts=%d", frame.Width(), frame.Height(), frame.PTS())
	}
}
```

The high-level API loads FFmpeg automatically. Applications must provide one
coherent FFmpeg shared-library family at runtime. Before the first FFmpeg call,
select it with `LD_LIBRARY_PATH` on Linux, `DYLD_LIBRARY_PATH` on macOS, or
`PATH` on Windows. Set `FFMPEG_SHIM_DIR` when supplying the optional matching C
shim. Android packages unversioned `.so` files inside the application; signed
iOS applications embed frameworks or link FFmpeg into the process image.

See [Getting started](docs/getting-started.md) for installation, diagnostics,
ownership, [hardware-decoder selection](docs/getting-started.md#hardware-decoding),
and mobile packaging details.

## Agentic development

The current hard-fork is developed with an Agentic Coding workflow configured as
follows:

| Component | Configuration |
| --- | --- |
| Model | OpenAI Codex, based on the GPT-5 model family. |
| Agent mode | Long-running repository sessions with workspace inspection, editing, builds, tests, and diagnostics. |
| Tools | Go and native toolchains, GitHub PR/CI workflows, and Android emulator/integration tooling. |
| Delivery | Focused branches, local validation, platform CI, and a pull request before merge. |
| Control | A human maintainer defines scope, approves privileged actions, reviews evidence, and decides what is merged. |

The model is a development tool, not the accountable author or reviewer.
Material assistance is disclosed in PRs and may be recorded with an
`Assisted-by: OpenAI Codex` commit trailer. Hosted model snapshots and tool
availability can evolve; reproducible facts belong in the corresponding PR.

## Documentation

- [Getting started](docs/getting-started.md)
- [Support and validation](docs/support.md)
- [Architecture](docs/architecture.md)
- [Roadmap and qualification gates](docs/roadmap.md)
- [Documentation index](docs/README.md)

The original ffgo documents are archived under [`docs/ffgo`](docs/ffgo/README.md)
for provenance. Their examples and compatibility claims do not describe the
current API.

## Contributing

Issues and pull requests are welcome. Please include the operating system,
architecture, exact FFmpeg versions and build, reproduction steps, and the tests
run. Read [CONTRIBUTING.md](CONTRIBUTING.md) before changing ABI, ownership,
callbacks, packaging, or platform code.

## License and attribution

go-ffmpeg-ffi and its inherited ffgo source are distributed under the
[Apache License 2.0](LICENSE); provenance is recorded in [NOTICE](NOTICE).
FFmpeg is a separate project, normally distributed under LGPL or GPL depending
on its build. This repository does not redistribute or relicense FFmpeg.
