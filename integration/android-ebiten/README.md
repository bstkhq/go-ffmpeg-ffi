# Android Ebitengine integration fixture

This is an intentionally separate Go module that consumes the local
`go-ffmpeg-ffi` checkout. It proves that an Ebitengine Android application can
bind the complete public package without making Ebitengine a dependency of the
library.

The first milestone displays two independent facts:

1. Ebitengine created and rendered the Android surface.
2. go-ffmpeg-ffi attempted to load the packaged FFmpeg libraries and reported
   complete diagnostics on screen.

Until Android FFmpeg shared libraries are added to the APK, a red
`FFmpeg load: FAILED` result is expected. Once they are packaged, the same APK
must show a green `FFmpeg load: OK` result before decode tests are enabled.

## Compile the Go AAR

The production target is ARM64 and the emulator target is x86-64:

```bash
export ANDROID_SDK_ROOT="$HOME/Android/Sdk"
export JAVA_HOME="/path/to/jdk-17"
export PATH="$ANDROID_SDK_ROOT/platform-tools:$(go env GOPATH)/bin:$PATH"

make compile ANDROID_TARGET=android/arm64
make clean_arr compile ANDROID_TARGET=android/amd64
```

`make build` also assembles the debug APK. The default Makefile clones
`bstkhq/apk-ebiten-builder` below `.build/`. An existing checkout can be reused
without changing tracked files:

```bash
make compile \
  BUILDER_DIR=/absolute/path/to/apk-ebiten-builder \
  ANDROID_TARGET=android/arm64
```

## Build and package FFmpeg

The repository builds pinned FFmpeg 8.0.3 shared libraries from the official
source tag. The configuration remains LGPL, includes the native software
codecs, and enables Android MediaCodec decoders and encoders for later device
qualification:

```bash
../../scripts/build-ffmpeg-android.sh amd64
make apk_with_ffmpeg ANDROID_TARGET=android/amd64

../../scripts/build-ffmpeg-android.sh arm64
make apk_with_ffmpeg ANDROID_TARGET=android/arm64
```

The script verifies the source checksum and each unversioned Android SONAME.
The resulting libraries and APK are build artifacts below `.build/`; no native
binary is committed to the Go binding.

The Android floor is API 33. ARM64 is the only shipping ABI; x86-64 exists for
the emulator integration gate and is not a production target. The
`apk_with_ffmpeg` target changes the generated application manifest floor to
API 33 so an APK containing API-33 native libraries cannot be installed on an
older Android version by mistake.
