# ffshim - FFmpeg Shim Library for ffmpeg

This directory contains a small C shim library that wraps FFmpeg functionality that purego cannot handle directly:

1. **Variadic functions** - `av_log()` uses `printf`-style variadic arguments
2. **Log callbacks** - FFmpeg log callbacks receive `va_list` parameters
3. **AVRational operations** - struct-by-value returns on non-Darwin platforms
4. **Chapter creation** - requires direct struct manipulation

## Important: The Shim is Optional

**Core ffmpeg functionality (decode, encode, transcode, scale, filter) works WITHOUT the shim.**

The shim is only required for:
- Custom log callbacks (`ffmpeg.SetLogCallback`)
- Log level control (`ffmpeg.SetLogLevel`)
- Chapter writing
- AVFrame color offset discovery
- Device enumeration (with libavdevice)

## Building the Shim

### Prerequisites

You need FFmpeg development libraries installed:

**Linux (Debian/Ubuntu):**
```bash
sudo apt install libavcodec-dev libavformat-dev libavutil-dev libavdevice-dev
```

**Linux (Fedora/RHEL):**
```bash
sudo dnf install ffmpeg-devel
```

**macOS:**
```bash
brew install ffmpeg
```

**Windows (MSYS2):**
```bash
pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-ffmpeg pkg-config
```

### Build Commands

**Using build.sh (recommended):**
```bash
# Build for current platform
./build.sh

# Build and install to /usr/local/lib
./build.sh install

# Build and stage a local copy under prebuilt/<os>-<arch>/
./build.sh prebuilt
```

**Using Makefile:**
```bash
# Build for current platform
make

# Install
sudo make install

# Clean
make clean
```

## Using the Shim

After building, the shim library needs to be discoverable. Options:

1. **Install system-wide** (recommended for production):
   ```bash
   sudo ./build.sh install
   # or
   sudo make install
   ```

2. **Set FFMPEG_SHIM_DIR** (recommended for development):
   ```bash
   export FFMPEG_SHIM_DIR=/path/to/ffmpeg/shim
   ```

3. **Copy to application directory**:
   - Place the shim library in the same directory as your Go executable

`LD_LIBRARY_PATH`, `DYLD_LIBRARY_PATH`, and `PATH` may still be needed for the
shim's FFmpeg dependencies, but they are not searched for the shim itself.

## Pre-built Shims

Pre-built shims are distributed only in release archives, never from the source
checkout. A release archive contains an explicit FFmpeg-family directory and a
`manifest.json` for every artifact:

```
shim/prebuilt/
  linux-amd64/ffmpeg-9/
    libffshim.so
    manifest.json
```

Choose the directory whose manifest has the same FFmpeg major as the runtime,
then set `FFMPEG_SHIM_DIR` to that directory, install the shim, or copy it
beside the application executable. The loader also verifies the artifact's
contract, exported symbols, and actual loaded FFmpeg family before using it.
`./build.sh prebuilt` is only a local staging helper; it does not create a
release-attested artifact.

## Search Paths

The shim library is searched in the following order:

1. `FFMPEG_SHIM_DIR` environment variable
2. Standard library paths (`/usr/local/lib`, `/usr/lib`, etc.)
3. Executable directory

The current working directory and paths embedded from the build machine are
deliberately excluded.

## Troubleshooting

### Checking Shim Status

```go
import "github.com/bstkhq/go-ffmpeg-ffi"

func main() {
    // Initialize ffmpeg (loads FFmpeg and shim if available)
    ffmpeg.Init()

    // Check shim status
    fmt.Println("Shim status:", ffmpeg.ShimStatus())

    // Check if logging is available
    if ffmpeg.IsLoggingAvailable() {
        fmt.Println("Logging is available")
    } else {
        fmt.Println("Logging not available:", ffmpeg.ShimBuildInstructions())
    }

    // Full diagnostics
    fmt.Println(ffmpeg.Diagnose())
}
```

### Common Issues

**"shim library not found"**
- Build the shim: `cd shim && ./build.sh`
- Set `FFMPEG_SHIM_DIR` to the directory containing the shim
- Or install it: `cd shim && ./build.sh install`

**"FFmpeg development libraries not found"**
- Install FFmpeg dev packages for your OS (see Prerequisites above)

**"failed to load shim"**
- Check that the shim matches your FFmpeg version
- Ensure FFmpeg libraries are in the library path
- On Linux: run `ldconfig` after installing

## Building for Multiple Platforms

For CI/CD and releases, use GitHub Actions to build shims on native runners. See `.github/workflows/build-shim.yml`.

For local cross-compilation (advanced), you can use `zig cc`:

```bash
# Build all platforms (requires zig and FFmpeg libs for each target)
FFMPEG_DIR=/path/to/ffmpeg-multiplatform make -C shim all-platforms
```

## Files

- `ffshim.c` - Shim implementation
- `ffshim.h` - Public API
- `build.sh` - Build script (recommended)
- `Makefile` - Alternative build system with cross-compilation support
- `prebuilt/` - ignored local staging directory; release archives contain the
  attested, versioned prebuilt artifacts
