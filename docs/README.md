# go-ffmpeg-ffi documentation

## Use the library

- [Getting started](getting-started.md): installation, library discovery,
  first decode, diagnostics, frame ownership, and native packaging.
- [Support and validation](support.md): FFmpeg versions, operating systems,
  architectures, evidence levels, and known qualification gaps.

## Understand and develop the project

- [Architecture](architecture.md): runtime layers, ABI policy, native ownership,
  callbacks, initialization, and distribution boundaries.
- [Roadmap](roadmap.md): completed platform work and the remaining emulator,
  physical-device, and runtime qualification gates.
- [Contributing](../CONTRIBUTING.md): expectations for changes, tests, PRs, and
  assisted-development disclosure.

## Historical ffgo documents

The original ffgo guide, design, feature analysis, and TODO list are archived in
[`docs/ffgo`](ffgo/README.md). They are retained for attribution and historical
context only. They contain obsolete import paths, examples, platform claims,
and APIs, and must not be used as go-ffmpeg-ffi documentation.

Current cross-cutting decisions belong in `architecture.md`; compatibility
evidence belongs in `support.md`; schedule and qualification work belongs in
`roadmap.md`.
