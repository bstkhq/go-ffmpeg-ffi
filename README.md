# go-ffmpeg-ffi

Go bindings for dynamically loaded FFmpeg libraries, built with
[PureGo](https://github.com/ebitengine/purego). Supported desktop targets aim
to remain CGO-free; PureGo mobile targets require the platform C toolchain.

> **Hard fork:** go-ffmpeg-ffi starts from
> [obinnaokechukwu/ffgo](https://github.com/obinnaokechukwu/ffgo). We are
> grateful to its author and contributors. The Git history, Apache-2.0 license,
> and attribution are preserved; this project does not claim their work as its
> own.

## Status

The project is in hard-fork bootstrap and has not published its first release.
Existing APIs and inherited documentation are being audited before that
release, so they are not yet a compatibility promise.

The target is a maintainable binding with explicit ABI detection, predictable
ownership, safe callbacks, and integration evidence for every supported FFmpeg
release line.

## Intended support

| FFmpeg | Policy |
| --- | --- |
| 5.1 | Legacy candidate only where a supported system has no practical newer package. |
| 6.0 and 6.1 | Official support target. |
| 7.0 and 7.1 | Official support target. |
| 8.0 and 8.1 | Official support target. |
| 4.x and older | Unsupported. |
| 9 and development snapshots | Unsupported until FFmpeg 9 is released and validated. |

Support means that the exact loaded library family is recognized and that the
same required test suite passes. It does not mean that every FFmpeg build has
the same codecs, filters, devices, or hardware accelerators.

## Design in brief

- Exported FFmpeg functions are loaded with PureGo. Supported desktop
  applications remain buildable with `CGO_ENABLED=0`; planned Android and iOS
  builds follow PureGo's requirement for `CGO_ENABLED=1`.
- Version-specific Go ABI layouts cover simple, verified structure access.
- A small versioned C shim handles C features that cannot be expressed safely
  through direct calls, such as variadic APIs and header-derived accessors.
- The loader rejects unknown, mixed, or shim-incompatible library families
  before unsafe access occurs.
- High-level APIs define ownership, cancellation, flushing, and concurrency
  explicitly.

See [Architecture](docs/architecture.md) for the reasoning and
[Roadmap](docs/roadmap.md) for the bootstrap and platform rollout.

## Documentation

- [Documentation index](docs/README.md)
- [Architecture and ABI policy](docs/architecture.md)
- [Implementation and test roadmap](docs/roadmap.md)
- [Inherited ffgo user guide](docs/user-guide.md), retained as migration input

The inherited design and feature documents are clearly labelled and must not be
read as go-ffmpeg-ffi support guarantees.

## Contributing

Please read [CONTRIBUTING.md](CONTRIBUTING.md). In particular, changes must name
the FFmpeg release lines they affect and include evidence at the appropriate
unit, integration, or stress-test level. Public branches and PRs are reviewed
for scope, compatibility impact, and validation evidence.

Development and review are assisted by **OpenAI Codex**. Human maintainers
review and remain responsible for every change. Material assistance is recorded
with an `Assisted-by: OpenAI Codex` commit trailer; Codex is not given a made-up
email address or authorship identity.

## License and attribution

go-ffmpeg-ffi and the inherited ffgo source are distributed under the
[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for provenance.

FFmpeg is a separate project, normally under LGPL-2.1-or-later and sometimes GPL
depending on how it is built. This repository's Apache-2.0 license does not
relicense FFmpeg. Any distributed FFmpeg or linked shim binaries must satisfy
the license of the FFmpeg build used to create them.
