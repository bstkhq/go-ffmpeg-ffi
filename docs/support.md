# Support and validation

go-ffmpeg-ffi targets the five operating-system families used by Bombastik!
applications: Linux, macOS, Windows, Android, and iOS. Support is evidence-based
and recorded at separate levels so that compilation is not mistaken for runtime
or hardware qualification.

## Evidence levels

1. **Compile:** every package and test binary compiles for the target and keeps
   the expected public API.
2. **Runtime:** FFmpeg loads and representative media operations run on the
   target operating system.
3. **Integration:** a downstream application packages the native libraries and
   exercises the public API in its real artifact shape.
4. **Qualification:** a named physical device passes sustained codec,
   lifecycle, resource, synchronization, and performance tests.

An earlier level never implies a later one. In particular, emulator rendering
does not prove a physical hardware-codec path or sustained 60 FPS.

## FFmpeg versions

The runtime loader recognizes these stable FFmpeg ABI families:

| FFmpeg | Core majors (`avutil/avcodec/avformat`) | CI evidence |
| --- | --- | --- |
| 6.x | `58/60/60` | 6.0.1 and 6.1.6 |
| 7.x | `59/61/61` | 7.0.3 and 7.1.5 |
| 8.x | `60/62/62` | 8.0.3 and 8.1.2 |
| 9.x | `61/63/63` | 9.0.1 |

Every pin is built from official FFmpeg source on Linux amd64. CI verifies the
selected version, checks public structure layouts against its headers, builds
the matching shim, and runs the package and integration suite. Testing the
latest supported patch of each stable release line exercises header and build
differences without claiming that every historical patch is separately tested.

FFmpeg 5.1 remains a legacy candidate in the architecture, but it is not part
of the current required CI matrix and should not be selected for new projects.
FFmpeg 4.x and older, future major versions, and development snapshots are
unsupported until explicitly added and qualified.

Optional libraries must belong to the same FFmpeg family. The enabled codecs,
formats, filters, devices, protocols, and hardware backends depend on the exact
FFmpeg configure flags and native platform.

## Operating systems

| Target | Compile | Runtime/integration | Remaining qualification |
| --- | --- | --- | --- |
| Linux `amd64` | Yes | Native CI; complete FFmpeg 6–9 matrix, ABI probe, shim, unit and integration tests. | Hardware acceleration and workload-specific performance remain device/build dependent. |
| Linux `arm64` | Supported source set and prebuilt shim | No dedicated native runtime job in the current CI workflow. | Add pinned native runtime evidence where required by a shipping product. |
| macOS `amd64` | Yes | Native Intel runner with Homebrew FFmpeg, ABI probe, shim and integration tests. | VideoToolbox and sustained performance require named hardware/build evidence. |
| macOS `arm64` | Yes | Native Apple Silicon runner with Homebrew FFmpeg, ABI probe, shim and integration tests. | Same VideoToolbox/device qualification boundary. |
| Windows `amd64` | Yes | Native runner with the public Gyan FFmpeg 9.0.1 shared build, ABI probe, shim and integration tests. | D3D11VA/DXVA2 and workload-specific performance are not implied. |
| Windows `arm64` | Yes | Compile-only; GitHub currently provides no public native runner used by this project. | Native FFmpeg loading and runtime suite. |
| Android `arm64` | API 33+ | Ebitengine AAR binding compiles. The Samsung Galaxy Tab A9+ is the minimum physical reference. | Physical APK runtime, MediaCodec, audio, thermal, sustained H.264/H.265 and 30/60 FPS qualification. |
| Android `x86-64` | API 33+ | Ebitengine APK on an API 33 emulator: FFmpeg 8.0.3, H.264/AAC software playback, RGBA presentation, resampling, seek, EOF, cancellation and prolonged lifecycle stress. | Emulator evidence is not a shipping ABI, MediaCodec result, audible-audio result, or physical performance benchmark. |
| iOS `arm64` device | iOS 13+ | Package/tests compile and downstream Ebitengine XCFramework binding succeeds. | Signed application runtime and physical VideoToolbox/audio/lifecycle/performance qualification on a named iPhone. |
| iOS simulator `arm64`, `amd64` | iOS 13+ while supported by the selected Xcode | Package/tests compile and the XCFramework contains simulator slices. | Simulator results do not qualify physical codecs or performance. |

FreeBSD is best-effort without a pinned native runner. WebAssembly cannot use
the current dynamic-FFmpeg architecture. Console targets requiring proprietary
SDKs are not inferred from Ebitengine support and are outside the present
matrix.

## Functional maturity

Playback is the primary production path and receives the strongest testing:

- coherent runtime loading and diagnostics;
- probing, demuxing, video/audio decoding and interleaved streams;
- pixel conversion and audio resampling;
- timestamps, seeking, EOF and delayed-frame draining;
- cancellation, close, repeated lifecycle and resource-growth checks;
- downstream Ebitengine packaging and presentation on Android.

The repository also exposes encoding, muxing/remuxing, stream copy, filters,
custom I/O, capture, subtitles, metadata, bitstream filters, segmentation,
colorspace conversion, and hardware acceleration. These areas have unit and
integration coverage, but less production use and less exhaustive platform and
device validation than playback. A green general CI run is not a claim that
every FFmpeg combination works.

When reporting or contributing a feature, record the OS, architecture, exact
library tuple, FFmpeg source/build, enabled codec or hardware backend, shim
status, media characteristics, and the evidence level reached.
