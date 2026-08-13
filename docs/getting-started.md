# Getting started

go-ffmpeg-ffi loads FFmpeg at runtime. Installing the Go module is only half of
the setup: the application must also provide a coherent supported FFmpeg shared-
library family for its operating system and architecture.

## Requirements

- Go 1.22.2 or newer.
- FFmpeg 6.x, 7.x, 8.x, or 9.x shared libraries from one coherent build.
- A 64-bit target covered by the [support matrix](support.md).
- Android NDK or Xcode and `CGO_ENABLED=1` for mobile builds.

The FFmpeg command-line program is not used by the Go API. It is useful for
diagnostics, but the required runtime components are the shared libraries such
as `avutil`, `avcodec`, `avformat`, `swscale`, and `swresample`. Optional
features load `avfilter` and `avdevice` from the same FFmpeg family.

## Install the module

```sh
go get github.com/bstkhq/go-ffmpeg-ffi
```

The module path and package name differ. Import it explicitly as `ffgo`:

```go
import ffgo "github.com/bstkhq/go-ffmpeg-ffi"
```

This project is based on ffgo but is not API-compatible with it. Do not use the
original `github.com/obinnaokechukwu/ffgo` import path or copy examples from the
[archived documentation](ffgo/README.md).

## Select the native FFmpeg build

Configure the library location before `ffgo.Init()` or any high-level API call:

| Platform | Primary application-controlled location |
| --- | --- |
| Linux | `LD_LIBRARY_PATH=/path/to/ffmpeg/lib` |
| macOS | `DYLD_LIBRARY_PATH=/path/to/ffmpeg/lib` |
| Windows | Add the FFmpeg `bin` directory to `PATH`. |
| Android | Package unversioned `libav*.so` files in the APK/AAR native-library namespace. |
| iOS | Embed and sign FFmpeg frameworks, or link complete FFmpeg archives into the final process image. |

Do not mix libraries from different FFmpeg builds in one search directory. The
loader validates the core and optional library-major tuple and rejects unknown
or mixed families before accessing version-specific structures.

The optional C shim provides operations that cannot be expressed safely through
direct PureGo calls. Point `FFGO_SHIM_DIR` at a shim built against the same
FFmpeg family. A missing optional shim does not prevent core decoding; an
incompatible shim is rejected. Prebuilt desktop shims live under
[`shim/prebuilt`](../shim/prebuilt), and source/build instructions are in the
[shim README](../shim/README.md).

## Check the runtime

High-level constructors initialize FFmpeg automatically. Calling `Init`
explicitly gives startup failures a clear place in the application lifecycle:

```go
if err := ffgo.Init(); err != nil {
	log.Fatal(err)
}
log.Print(ffgo.Diagnose())
```

`Diagnose` reports the OS and architecture, loaded FFmpeg versions, shim status,
and availability of scaling, resampling, filters, and devices. It reports
capabilities of the exact runtime build, not theoretical FFmpeg features.

## Decode a video

```go
package main

import (
	"errors"
	"io"
	"log"

	ffgo "github.com/bstkhq/go-ffmpeg-ffi"
)

func main() {
	decoder, err := ffgo.NewDecoder("video.mp4", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer decoder.Close()

	if !decoder.HasVideo() {
		log.Fatal(ffgo.ErrNoVideoStream)
	}
	stream := decoder.VideoStream()
	log.Printf("%s %dx%d", stream.CodecID, stream.Width, stream.Height)

	for {
		frame, err := decoder.DecodeVideo()
		switch {
		case errors.Is(err, io.EOF):
			return
		case err != nil:
			log.Fatal(err)
		case frame.IsNil():
			continue
		}

		log.Printf("frame pts=%d format=%d", frame.PTS(), frame.PixelFormat())
	}
}
```

`DecodeVideo` opens the selected video decoder on first use and returns
`io.EOF` after it is fully drained. Context variants are available for
cancellable open and decode operations.

## Frame ownership

Frames returned by `DecodeVideo`, `DecodeAudio`, and `ReadFrame` are borrowed
from the decoder. They remain valid only until the next decode/reuse operation
and must not be freed by the caller.

Use `DecodeVideoCopy`, `DecodeAudioCopy`, `ReadFrameCopy`, or `Frame.Clone` when
a frame must be retained. Those frames are owned by the caller and must be
released:

```go
frame, err := decoder.DecodeVideoCopy()
if err != nil {
	return err
}
defer frame.Free()
```

Plane data returned by `Frame.Data` is also borrowed. Copy it before the frame
is reused or freed.

## Encoding and finalization

Encoding, muxing, filters, capture, and hardware acceleration have less
production coverage than playback. Check the installed FFmpeg capabilities and
test the exact codec/container/device combination you ship.

Prefer `Encoder.Flush()` over using an empty frame as FFmpeg's flush marker.
Always handle the result of `Encoder.Close()`: it flushes delayed packets and
writes the output trailer, so discarding that error can hide an incomplete
output file.

```go
if err := encoder.Flush(); err != nil {
	return err
}
if err := encoder.Close(); err != nil {
	return err
}
```

Video and audio frames preserve an explicitly supplied PTS. When the PTS is
`AV_NOPTS_VALUE`, the encoder supplies the next frame or sample timestamp.

## Mobile integration

The Go module does not build an APK, AAR, IPA, or application-specific FFmpeg
distribution. Those artifacts and their native licensing material belong to the
consuming application.

- [Android Ebitengine fixture](../integration/android-ebiten/README.md)
- [iOS Ebitengine fixture](../integration/ios-ebiten/README.md)

These fixtures prove downstream packaging and binding without adding an
Ebitengine dependency to go-ffmpeg-ffi itself.
