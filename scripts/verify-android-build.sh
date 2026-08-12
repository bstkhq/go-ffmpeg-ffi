#!/usr/bin/env bash

set -euo pipefail

readonly android_arch="${1:-}"
readonly android_api="${ANDROID_API:-33}"
readonly ndk_version="${ANDROID_NDK_VERSION:-26.3.11579264}"
readonly sdk_root="${ANDROID_SDK_ROOT:-${ANDROID_HOME:-}}"

if [[ -z "${sdk_root}" ]]; then
	echo "ANDROID_SDK_ROOT or ANDROID_HOME must point to an Android SDK" >&2
	exit 2
fi

case "${android_arch}" in
	arm64)
		readonly goarch="arm64"
		readonly compiler_prefix="aarch64-linux-android"
		;;
	amd64)
		readonly goarch="amd64"
		readonly compiler_prefix="x86_64-linux-android"
		;;
	*)
		echo "usage: $0 {arm64|amd64}" >&2
		exit 2
		;;
esac

case "$(uname -s)" in
	Linux)
		readonly ndk_host="linux-x86_64"
		;;
	Darwin)
		readonly ndk_host="darwin-x86_64"
		;;
	*)
		echo "unsupported NDK host: $(uname -s)" >&2
		exit 2
		;;
esac

readonly toolchain="${sdk_root}/ndk/${ndk_version}/toolchains/llvm/prebuilt/${ndk_host}/bin"
readonly cc="${toolchain}/${compiler_prefix}${android_api}-clang"

if [[ ! -x "${cc}" ]]; then
	echo "Android compiler not found: ${cc}" >&2
	exit 2
fi

export GOOS=android
export GOARCH="${goarch}"
export CGO_ENABLED=1
export CC="${cc}"

echo "Verifying Android ${GOARCH}, API ${android_api}, NDK ${ndk_version}"

# Prevent a falsely green build caused by broad build constraints excluding the
# public implementation. These files collectively cover initialization, decode,
# encode, frames, hardware capability discovery, and audio resampling.
readonly root_go_files="$(go list -f '{{range .GoFiles}}{{println .}}{{end}}' .)"
for required_file in ffgo.go decoder.go encoder.go frame.go hwaccel.go resampler.go; do
	if ! grep -Fxq "${required_file}" <<<"${root_go_files}"; then
		echo "Android source set is missing ${required_file}" >&2
		exit 1
	fi
done

go list ./...
go build ./...

# The binaries cannot execute on the host runner. -exec replaces their launch
# only after the Go tool has compiled every target test binary.
go test -exec /bin/true -run '^$' ./...
