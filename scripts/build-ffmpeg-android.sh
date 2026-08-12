#!/usr/bin/env bash
set -euo pipefail

readonly ffmpeg_version="8.0.3"
readonly ffmpeg_archive_sha256="672183051a9ad4881a8a49311f76bd2f45fd560e836456d8b3c08e3d525baea7"
readonly default_ndk_version="26.3.11579264"
readonly default_android_api="33"

usage() {
	cat <<'EOF'
Usage: scripts/build-ffmpeg-android.sh <arm64|amd64>

Builds LGPL FFmpeg shared libraries for Android. The production target is
arm64; amd64 is the x86_64 emulator target.

Environment overrides:
  ANDROID_SDK_ROOT    Android SDK root (required)
  ANDROID_NDK_VERSION NDK version (default: 26.3.11579264)
  ANDROID_API         Android API floor (default: 33)
  FFMPEG_ANDROID_JOBS Parallel make jobs (default: online CPU count)
EOF
}

if [[ $# -ne 1 ]]; then
	usage >&2
	exit 2
fi

readonly go_arch="$1"
readonly android_sdk_root="${ANDROID_SDK_ROOT:?ANDROID_SDK_ROOT must point to the Android SDK}"
readonly ndk_version="${ANDROID_NDK_VERSION:-$default_ndk_version}"
readonly android_api="${ANDROID_API:-$default_android_api}"
readonly jobs="${FFMPEG_ANDROID_JOBS:-$(getconf _NPROCESSORS_ONLN)}"
readonly ndk_root="$android_sdk_root/ndk/$ndk_version"
readonly toolchain="$ndk_root/toolchains/llvm/prebuilt/linux-x86_64"

case "$go_arch" in
	arm64)
		readonly ffmpeg_arch="aarch64"
		readonly compiler_triple="aarch64-linux-android"
		readonly android_abi="arm64-v8a"
		readonly cpu_flags="--cpu=armv8-a"
		;;
	amd64)
		readonly ffmpeg_arch="x86_64"
		readonly compiler_triple="x86_64-linux-android"
		readonly android_abi="x86_64"
		readonly cpu_flags="--cpu=x86-64"
		;;
	*)
		echo "unsupported architecture: $go_arch (expected arm64 or amd64)" >&2
		exit 2
		;;
esac

readonly repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly work_root="$repo_root/.build/ffmpeg-android"
readonly download_dir="$work_root/downloads"
readonly source_parent="$work_root/src"
readonly archive="$download_dir/ffmpeg-n$ffmpeg_version.tar.gz"
readonly source_dir="$source_parent/FFmpeg-n$ffmpeg_version"
readonly build_dir="$work_root/build/$android_abi"
readonly install_dir="$work_root/install/$android_abi"
readonly archive_url="https://codeload.github.com/FFmpeg/FFmpeg/tar.gz/refs/tags/n$ffmpeg_version"

readonly cc="$toolchain/bin/${compiler_triple}${android_api}-clang"
readonly cxx="$toolchain/bin/${compiler_triple}${android_api}-clang++"

if [[ ! -x "$cc" || ! -x "$cxx" ]]; then
	echo "Android NDK compiler not found under $toolchain" >&2
	exit 1
fi

mkdir -p "$download_dir" "$source_parent" "$build_dir" "$install_dir"

if [[ -f "$archive" ]]; then
	actual_sha256="$(sha256sum "$archive" | awk '{print $1}')"
	if [[ "$actual_sha256" != "$ffmpeg_archive_sha256" ]]; then
		echo "cached FFmpeg archive has checksum $actual_sha256, expected $ffmpeg_archive_sha256" >&2
		exit 1
	fi
else
	echo ">>> Downloading FFmpeg $ffmpeg_version source"
	curl --fail --location --retry 3 --retry-all-errors \
		--output "$archive.partial" "$archive_url"
	actual_sha256="$(sha256sum "$archive.partial" | awk '{print $1}')"
	if [[ "$actual_sha256" != "$ffmpeg_archive_sha256" ]]; then
		echo "downloaded FFmpeg archive has checksum $actual_sha256, expected $ffmpeg_archive_sha256" >&2
		exit 1
	fi
	mv "$archive.partial" "$archive"
fi

if [[ ! -x "$source_dir/configure" ]]; then
	echo ">>> Extracting FFmpeg $ffmpeg_version source"
	tar -xzf "$archive" -C "$source_parent"
fi

echo ">>> Configuring FFmpeg $ffmpeg_version for Android $android_api / $android_abi"
cd "$build_dir"
"$source_dir/configure" \
	--prefix="$install_dir" \
	--target-os=android \
	--arch="$ffmpeg_arch" \
	"$cpu_flags" \
	--enable-cross-compile \
	--sysroot="$toolchain/sysroot" \
	--cc="$cc" \
	--cxx="$cxx" \
	--ar="$toolchain/bin/llvm-ar" \
	--nm="$toolchain/bin/llvm-nm" \
	--ranlib="$toolchain/bin/llvm-ranlib" \
	--strip="$toolchain/bin/llvm-strip" \
	--enable-shared \
	--disable-static \
	--enable-pic \
	--enable-jni \
	--enable-mediacodec \
	--disable-programs \
	--disable-doc \
	--disable-debug \
	--disable-avdevice \
	--disable-avfilter \
	--disable-network \
	--disable-autodetect \
	--extra-cflags="-O3 -fPIC" \
	--extra-ldflags="-Wl,-z,max-page-size=16384"

echo ">>> Building FFmpeg shared libraries ($jobs jobs)"
make -j"$jobs"
make install

echo ">>> Verifying Android shared-library package"
for library in avutil avcodec avformat swscale swresample; do
	path="$install_dir/lib/lib$library.so"
	if [[ ! -f "$path" ]]; then
		echo "missing required Android library: $path" >&2
		exit 1
	fi
	soname="$($toolchain/bin/llvm-readelf -d "$path" | sed -n 's/.*SONAME.*\[\(.*\)\].*/\1/p')"
	if [[ "$soname" != "lib$library.so" ]]; then
		echo "$path has unexpected SONAME $soname" >&2
		exit 1
	fi
done

cat <<EOF
>>> FFmpeg Android build complete
    Version : $ffmpeg_version
    API     : $android_api
    ABI     : $android_abi
    License : LGPL (GPL and nonfree components disabled)
    Output  : $install_dir/lib
EOF
