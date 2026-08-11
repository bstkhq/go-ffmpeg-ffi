# Roadmap

This is the project's only implementation plan. It is intentionally organized
as six coherent pull requests, not as one PR per bullet. Work found inside the
same subsystem stays in its PR unless reviewability forces a split; if one PR is
split, another is consolidated so the bootstrap remains at roughly six PRs.

## PR 1: establish the hard-fork baseline

- Adopt the `go-ffmpeg-ffi` repository, package, and module identity.
- Preserve the complete ffgo Git history, Apache-2.0 license, and upstream
  attribution.
- Record the exact upstream commit from which the hard fork starts.
- Carry the known FFmpeg 6 audio-layout, `swr_convert`, callback, and FFmpeg 6/7
  ABI corrections into a traceable baseline.
- Replace inherited compatibility, coverage, platform, and performance claims
  that have not been verified.
- Add the hard-fork, Codex-assistance, and human-review disclosures.

Complete when a clean clone builds under the new module path and no public
document presents inherited claims as go-ffmpeg-ffi guarantees.

## PR 2: make FFmpeg 6-8 ABI selection safe

- Centralize library discovery and registration instead of binding the complete
  API from package `init()` functions.
- Read the complete core and optional-library version tuple before selecting an
  ABI family.
- Inventory every direct FFmpeg structure access and move verified layouts into
  `internal/abi`; no private magic offsets remain.
- Generate or verify layouts for FFmpeg 6, 7, and 8 against matching headers.
- Version the C shim by operating system, architecture, shim API, and FFmpeg
  family; add a startup handshake and reject mismatches.
- Reject mixed families, FFmpeg 4, FFmpeg 9/master, and unknown layouts with a
  diagnostic error before unsafe field access.
- Decide whether FFmpeg 5.1 earns a narrowly scoped Debian 12 legacy tier, a CI
  job, and a sunset date. Otherwise remove it from the matrix.

Complete when every target family initializes through the same loader and all
unsupported combinations fail closed.

## PR 3: correct decoder and encoder state machines

- Model the complete FFmpeg send/receive protocol, including `EAGAIN` retry.
- Flush once and drain delayed video and audio output through FFmpeg EOF.
- Preserve packets for every selected stream rather than losing interleaved
  data.
- Propagate decode, encode, flush, mux, and trailer errors consistently.
- Add focused fixtures for delayed codecs, multiple streams, seeking, EOF, and
  truncated input.

Complete when reference counts and timestamps demonstrate that normal and
delayed frames are neither lost nor duplicated.

## PR 4: make ownership, callbacks, and cancellation safe

- Define owned, borrowed, clone, `Into`, and zero-copy lifetimes.
- Correct plane access, `extended_data`, pooling, and native cleanup.
- Pass callback handles as integer tokens, recover callback panics, and retain
  the original Go error.
- Add context-aware open/read operations and make close unblock pending I/O.
- Document object-level concurrency guarantees.
- Test repeated open/decode/seek/close, malformed and custom I/O, callback
  failure, cancellation, and concurrent independent decoders.

Complete when stress runs show no invalid callback pointers, leaked handles,
unbounded native memory, file-descriptor growth, or blocked shutdowns.

## PR 5: make compatibility claims executable

CI tests every published release line inside the supported majors, using the
latest patched release pinned for that line:

| Line | Initial pin | Required |
| --- | --- | --- |
| 5.1 | `5.1.10` | Only if the legacy tier is accepted. |
| 6.0 | `6.0.1` | Yes. |
| 6.1 | `6.1.6` | Yes. |
| 7.0 | `7.0.3` | Yes. |
| 7.1 | `7.1.5` | Yes. |
| 8.0 | `8.0.3` | Yes. |
| 8.1 | `8.1.2` | Yes. |

These initial pins were checked on 2026-08-11 against the
[official FFmpeg download page](https://ffmpeg.org/download.html).

Testing every release line catches header and build differences without running
every historical patch that shares the same stable library ABI. A scheduled job
checks for newer patch releases; pin updates remain explicit and reviewable.

Each required CI job:

- builds an LGPL-only shared FFmpeg configuration and the matching shim;
- verifies tarball signatures or checksums and records the configure flags;
- asserts the actual loaded versions and shim handshake;
- runs unit and integration tests with `CGO_ENABLED=0`;
- exercises decode, encode, audio resampling, custom I/O, seek, flush, and error
  paths with audio-only and audio/video media;
- runs repeated lifecycle and parallel-decoder stress tests;
- records goroutines, callback handles, file descriptors, RSS, and native
  allocations, with sanitizer or Valgrind coverage for the C shim;
- tests no-shim behavior separately for features that are genuinely optional.

Local validation repeats the matrix from versioned directories inside the
workspace, never a transient system path. For each FFmpeg line it first tests
go-ffmpeg-ffi, then points a go-avebi integration checkout at the local module
with a workspace-only `replace` in that checkout. The existing go-ebiten-mcp
examples run against the same media fixture containing both video and sound so
decoding, audio conversion, playback, seeking, shutdown, and the callback path
are all exercised. A machine-readable result matrix records the exact Go,
FFmpeg, shim, go-avebi, and go-ebiten-mcp revisions.

Complete when every supported line passes the same required suite in CI and the
local go-avebi matrix has no crashes, growing resources, or unexplained output
differences.

## PR 6: stabilize the public API and first release

- Set the high-level/low-level package boundary and consolidate duplicate
  constructors and options.
- Expose runtime diagnostics and capability discovery without leaking ABI
  details.
- Finish the concise user documentation and migrate only validated examples.
- Define semantic versioning, deprecation, security, and support-window policy.
- Publish reproducible shims, manifests, source/configuration material, and
  checksums for validated targets.

Complete when the API has an explicit compatibility promise and the first
release can be reproduced from its tag.

## Review contract

Every PR states the affected FFmpeg lines, ownership or ABI implications, tests
run, known gaps, and whether Codex assisted materially. Refactors do not hide
unrelated features.
