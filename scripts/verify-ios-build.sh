#!/usr/bin/env bash
set -euo pipefail

readonly IOS_VERSION="${IOS_VERSION:-13.0}"

usage() {
  echo "usage: $0 <iphoneos|iphonesimulator> <arm64|amd64>" >&2
  exit 2
}

[[ $# -eq 2 ]] || usage
readonly sdk="$1"
readonly goarch="$2"

case "$sdk/$goarch" in
  iphoneos/arm64)
    readonly clang_arch="arm64"
    readonly minimum_flag="-miphoneos-version-min=$IOS_VERSION"
    ;;
  iphonesimulator/arm64)
    readonly clang_arch="arm64"
    readonly minimum_flag="-mios-simulator-version-min=$IOS_VERSION"
    ;;
  iphonesimulator/amd64)
    readonly clang_arch="x86_64"
    readonly minimum_flag="-mios-simulator-version-min=$IOS_VERSION"
    ;;
  *)
    usage
    ;;
esac

readonly sdk_path="$(xcrun --sdk "$sdk" --show-sdk-path)"
readonly clang="$(xcrun --sdk "$sdk" --find clang)"
readonly cflags="-isysroot $sdk_path $minimum_flag -arch $clang_arch"

echo "Compiling all packages and tests for $sdk/$goarch (iOS $IOS_VERSION)"
CGO_ENABLED=1 \
GOOS=ios \
GOARCH="$goarch" \
GOFLAGS=-tags=ios \
CC="$clang" \
CXX="$clang++" \
CGO_CFLAGS="$cflags" \
CGO_CXXFLAGS="$cflags" \
CGO_LDFLAGS="$cflags" \
DARWIN_SDK="$sdk" \
go test -exec /usr/bin/true -run '^$' ./...
