# Roadmap

This is the project's only implementation plan. The hard-fork bootstrap is
intentionally organized as six coherent pull requests, not as one PR per
bullet. Work found inside the same subsystem stays in its PR unless
reviewability forces a split; if one PR is split, another is consolidated so
the bootstrap remains at roughly six PRs. Platform expansion follows the
bootstrap as an Android-first program with its own evidence gates.

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
- Establish a pinned Linux amd64 gate for the latest patch of the 6.0, 6.1,
  7.0, 7.1, 8.0, and 8.1 release lines.
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

## PR 5: expand compatibility evidence and stress coverage

PR 2 establishes the Linux amd64 release-line gate. PR 5 expands that same
matrix with the complete behavioral, resource, and downstream integration
suite, using the latest patched release pinned for each line:

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

Each required matrix job:

- builds a shared FFmpeg test configuration and the matching shim;
- verifies tarball signatures or checksums and records the configure flags;
- asserts the actual loaded versions and shim handshake;
- runs unit and integration tests with `CGO_ENABLED=0`;
- exercises decode, encode, audio resampling, custom I/O, seek, flush, and error
  paths with audio-only and audio/video media;
- runs repeated lifecycle and parallel-decoder stress tests;
- records goroutines, callback handles, file descriptors, RSS, and native
  allocations, with sanitizer or Valgrind coverage for the C shim;
- tests no-shim behavior separately for features that are genuinely optional.

CI-only FFmpeg builds may enable GPL codecs needed by inherited tests. They are
not distributed. Prebuilt release shims remain subject to the LGPL-only policy
in [Architecture](architecture.md#distribution).

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

## Platform expansion program

Platform support is earned in three separate stages:

1. **Compile:** every importable package and its tests compile for the target,
   with the expected public API present.
2. **Integrate:** an external application packages the native libraries, loads
   FFmpeg, and exercises representative audio/video paths on the target OS.
3. **Qualify:** named hardware passes codec, stability, and performance tests.

Passing an earlier stage does not imply a later one. In particular, emulator
graphics acceleration is not evidence that a physical device provides FFmpeg
MediaCodec, VideoToolbox, or 60 FPS performance.

go-ffmpeg-ffi remains independent of Ebitengine. Ebitengine applications are
downstream integration fixtures: the binding does not import Ebitengine, expose
an Ebitengine API, or make the engine part of its platform implementation.

### Target order and support floor

| Order | Target | Architecture and floor | Planned evidence |
| --- | --- | --- | --- |
| 1 | Android production | `arm64-v8a`, Android 13 / API 33 | Compile CI, external APK, and Samsung Galaxy Tab A9+ qualification. |
| 1 | Android emulator | `x86_64`, Android 13 / API 33 | GPU-accelerated application integration; not a shipping ABI or hardware-codec benchmark. |
| 2 | iOS | `arm64` device and simulator; simulator `amd64` while supported by the selected Xcode | Compile and application integration first; the minimum iOS version and reference device are set before implementation. |
| 3 | Windows | `amd64`, then `arm64` | Replace the Unix loader calls, add compile CI, and add native runtime evidence. |
| Continuous | macOS | `amd64`, `arm64` | Preserve the existing native compile/runtime jobs as regression gates. |
| Continuous | Linux | `amd64`, `arm64` | Preserve the primary FFmpeg release-line and stress matrix. |

[PureGo](https://github.com/ebitengine/purego#supported-platforms) treats
Android and iOS 64-bit targets as Tier 1, but requires `CGO_ENABLED=1` for them.
The mobile build is therefore still PureGo-based, but it requires the Android
NDK or Xcode C toolchain. Desktop targets retain the CGO-free application-build
goal where PureGo supports it.

### Android phase A: make the module compile

- Remove the blanket Android exclusions and replace them with capability-based
  build constraints.
- Split library open, symbol lookup, close, naming, and search policy by
  platform. Android uses its native linker and packaged `.so` files rather than
  desktop search paths.
- Verify every enabled ABI layout against Android FFmpeg headers for each
  supported FFmpeg family. Do not infer layout compatibility from Linux alone.
- Keep the public API present. Operations that are genuinely unavailable on
  Android return a documented unsupported-capability error instead of
  disappearing behind build constraints.
- Pin an NDK and compile all packages and test binaries with API 33 toolchains
  for `arm64-v8a` and `x86_64`. Native FFmpeg libraries are not required for the
  compile-only gate.
- Compare the exported API with the Linux reference so a green mobile build
  cannot be produced by accidentally excluding most of the module.

Complete when `go list ./...`, package builds, and test compilation succeed for
both Android targets with the intended public surface and without an Ebitengine
dependency.

Current branch evidence (12 August 2026): this compile gate passes for API 33
`arm64` and `amd64`, including every root package and test binary. The external
Ebitengine fixture remains a separate module.

### Android phase B: emulator integration

- Maintain the external-module fixture in
  [`integration/android-ebiten`](../integration/android-ebiten) and build it with
  [`apk-ebiten-builder`](https://github.com/bstkhq/apk-ebiten-builder). Packaging
  FFmpeg `.so` files belongs to that fixture or its builder, not to the Go
  binding.
- Run an Android 13 / API 33 `x86_64` AVD with VM acceleration and host GPU
  graphics where the runner exposes them.
- Install and launch the APK through the connected Android automation tooling;
  retain screenshots, application state, logcat output, FFmpeg library/version
  diagnostics, and crash traces as test evidence.
- Exercise library initialization, audiovisual decode, audio conversion,
  frame presentation through Ebitengine, seek, EOF, cancellation, and clean
  shutdown with the same small redistributable fixture used by desktop tests.
- Treat emulator MediaCodec results as diagnostic only. The host GPU validates
  Ebitengine graphics; it does not emulate the Snapdragon video codec blocks in
  the reference tablet.

Complete when a reproducible APK runs through the audiovisual scenario without
crashes, missing symbols, growing resources, or unexplained output differences.

Current branch evidence (12 August 2026): the API 33 x86-64 APK loads FFmpeg
8.0.3, software-decodes all 60 H.264 frames, converts and presents RGBA through
Ebitengine, software-decodes all 87 AAC frames, resamples them to 96,967 S16
stereo samples at 48 kHz, and starts an Ebitengine audio player. The current
host exposes neither `/dev/kvm` nor `/dev/dri`, so this run uses TCG and
SwiftShader and is correctness evidence only. Seek, cancellation, explicit EOF
assertions, clean shutdown, and resource-growth checks remain open before phase
B is complete.

### Android phase C: Galaxy Tab A9+ qualification

The Samsung Galaxy Tab A9+ is the minimum supported Android device and the
physical acceptance reference. Its launch platform, Android 13 / API 33, is the
OS floor; newer Android devices and framework versions must remain compatible.

- Install the exact APK already exercised by the emulator, using its
  `arm64-v8a` native libraries.
- Record device model, Android build, ABI, FFmpeg configuration and versions,
  selected decoders/encoders, and the MediaCodec capability report.
- Test H.264 and H.265 software and MediaCodec paths separately. A codec name or
  API being present is not sufficient evidence that frames stay on the
  hardware path.
- Measure 30 and 60 FPS workloads separately, including dropped/late frames,
  audiovisual drift, CPU load, memory, temperature-related throttling, and
  clean resource release. Screenshots prove rendered output, not frame rate.
- Report decode and encode results separately. A device can decode a profile or
  resolution in hardware without being able to encode the same combination.
- Run a sustained test after the short correctness case so a transient 60 FPS
  result is not presented as stable performance.

Compilation support is complete independently of hardware acceleration.
MediaCodec and 60 FPS claims are published only for the exact codec, profile,
resolution, pixel format, device, Android build, and FFmpeg configuration that
passed qualification.

### iOS phase

- Reuse the platform loader boundary established by Android, with iOS-specific
  library naming and application-bundle resolution.
- Compile all packages and tests for ARM64 device and simulator targets with
  Xcode and `CGO_ENABLED=1`; retain Intel simulator coverage only while the
  supported Xcode toolchain provides it.
- Verify ABI layouts against the selected iPhoneOS and simulator SDKs.
- Use an external application fixture for runtime integration; do not add an
  Ebitengine dependency or XCFramework packaging responsibility to the module.
- Qualify VideoToolbox and sustained codec performance on a named physical
  device. Simulator graphics and codec results are not physical-device
  qualification.

The minimum iOS version and reference hardware must be recorded before this
phase changes support claims.

### Windows and macOS closure

PureGo supports Windows `amd64` and `arm64` as Tier 1 platforms. The current
go-ffmpeg-ffi loader nevertheless calls PureGo's `Dlopen`, `Dlsym`, `Dlclose`,
and `RTLD_*` API, which is intentionally unavailable on Windows. Windows needs
a platform loader using `LoadLibrary`, `GetProcAddress`, and `FreeLibrary`; this
is a go-ffmpeg-ffi integration gap, not a lack of Windows support in PureGo.

After the mobile phases:

- add Windows `amd64` and `arm64` compile gates and native `amd64` runtime
  coverage;
- extend native runtime coverage to Windows ARM64 when a runner is available;
- rerun the existing macOS Intel and Apple Silicon FFmpeg jobs after every
  loader refactor; and
- keep hardware acceleration capability-driven: D3D11VA/DXVA2 and
  VideoToolbox are not implied by successful compilation.

### Unscheduled ecosystems

FreeBSD remains best-effort pending a pinned native runner. WebAssembly cannot
use the current dynamic-FFmpeg/PureGo architecture and would require a distinct
backend. Nintendo Switch and Xbox require their proprietary SDK and program
access before feasibility can be evaluated; they are not inferred from
Ebitengine application support. PlayStation is not a public Ebitengine target
and is not scheduled. None of these platforms is part of the Android/iOS
delivery sequence.

## Review contract

Every PR states the affected FFmpeg lines, ownership or ABI implications, tests
run, known gaps, and whether Codex assisted materially. Refactors do not hide
unrelated features.
