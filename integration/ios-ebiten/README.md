# iOS Ebitengine integration fixture

This separate Go module proves that an Ebitengine iOS application can compile
and bind `go-ffmpeg-ffi` without adding Ebitengine to the library's dependency
graph. The supported deployment floor is iOS 13.0, matching Ebitengine's
documented minimum.

On macOS with Xcode installed:

```bash
go install github.com/hajimehoshi/ebiten/v2/cmd/ebitenmobile@v2.9.9
make compile
```

The default `ios` target creates an XCFramework containing iPhone ARM64 and the
simulator slices supported by the selected Xcode. CI checks that output and
also compiles every package and test binary for device and simulator targets.

## Native FFmpeg packaging

The Go binding does not redistribute FFmpeg or place native binaries inside a
consumer's application. The Xcode application must use one coherent FFmpeg 6,
7, 8, or 9 family and choose one of these packaging models:

1. Embed and sign dynamic `libav*.framework` (or `av*.framework`) bundles. The
   iOS loader searches the app's `Frameworks` directory and its `@rpath`.
2. Link complete static FFmpeg libraries into the final application and retain
   their symbols so they are visible through the process-wide dynamic symbol
   namespace. Merely adding an archive without force-loading its FFmpeg object
   files is insufficient because calls are resolved by name at runtime.

All embedded code must be present when the application is signed. This fixture
does not download or execute replacement native code at runtime.

Without packaged FFmpeg, the fixture still launches and reports `FFmpeg load:
FAILED`; that is a useful UI/loader diagnostic, but it is not runtime support
evidence. With FFmpeg packaged, `FFmpeg load: OK` confirms family detection.

## Qualification boundary

The simulator validates compilation, Ebitengine/Metal presentation, lifecycle,
and software-codec correctness once native libraries are supplied. It does not
qualify VideoToolbox, audible audio, thermal behavior, or sustained 60 FPS.
Those require the exact signed application on a named physical iPhone.
