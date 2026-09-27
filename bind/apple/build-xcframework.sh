#!/usr/bin/env bash
#
# build-xcframework.sh: builds Tailnet.xcframework for macOS and iOS
# using Go c-archive and xcodebuild.
#
# Requirements (on macOS host):
#   - Xcode 15+ (with command-line tools)
#   - Go 1.21+
#
# Outputs:
#   dist/Tailnet.xcframework
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
FFI_DIR="${ROOT_DIR}/bind/ffi"
DIST_DIR="${ROOT_DIR}/dist"
BUILD_DIR="${DIST_DIR}/apple-build"

rm -rf "${BUILD_DIR}"
mkdir -p "${BUILD_DIR}" "${DIST_DIR}"

echo "==> Building static libraries for Apple platforms..."

# 1. macOS (Universal: arm64 + x86_64)
echo "--> Compiling macOS (arm64)..."
CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
    go build -C "${FFI_DIR}" -buildmode=c-archive -ldflags="-s -w" \
    -o "${BUILD_DIR}/macos-arm64/libtailnet.a" .

echo "--> Compiling macOS (amd64)..."
CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
    go build -C "${FFI_DIR}" -buildmode=c-archive -ldflags="-s -w" \
    -o "${BUILD_DIR}/macos-amd64/libtailnet.a" .

mkdir -p "${BUILD_DIR}/macos-universal"
lipo -create \
    "${BUILD_DIR}/macos-arm64/libtailnet.a" \
    "${BUILD_DIR}/macos-amd64/libtailnet.a" \
    -output "${BUILD_DIR}/macos-universal/libtailnet.a"
cp "${BUILD_DIR}/macos-arm64/libtailnet.h" "${BUILD_DIR}/macos-universal/tailnet.h"

# 2. iOS Device (arm64)
SDK_IPHONEOS="$(xcrun --sdk iphoneos --show-sdk-path)"
CLANG_IPHONEOS="$(xcrun --sdk iphoneos --find clang)"

echo "--> Compiling iOS Device (arm64)..."
CGO_ENABLED=1 GOOS=ios GOARCH=arm64 \
CC="${CLANG_IPHONEOS}" \
CGO_CFLAGS="-isysroot ${SDK_IPHONEOS} -miphoneos-version-min=15.0 -arch arm64" \
CGO_LDFLAGS="-isysroot ${SDK_IPHONEOS} -miphoneos-version-min=15.0 -arch arm64" \
    go build -C "${FFI_DIR}" -buildmode=c-archive -ldflags="-s -w" \
    -o "${BUILD_DIR}/ios-arm64/libtailnet.a" .

# 3. iOS Simulator (Universal: arm64 + x86_64)
SDK_IPHONESIM="$(xcrun --sdk iphonesimulator --show-sdk-path)"
CLANG_IPHONESIM="$(xcrun --sdk iphonesimulator --find clang)"

echo "--> Compiling iOS Simulator (arm64)..."
CGO_ENABLED=1 GOOS=ios GOARCH=arm64 \
CC="${CLANG_IPHONESIM}" \
CGO_CFLAGS="-isysroot ${SDK_IPHONESIM} -mios-simulator-version-min=15.0 -arch arm64" \
CGO_LDFLAGS="-isysroot ${SDK_IPHONESIM} -mios-simulator-version-min=15.0 -arch arm64" \
    go build -C "${FFI_DIR}" -buildmode=c-archive -ldflags="-s -w" \
    -o "${BUILD_DIR}/iossim-arm64/libtailnet.a" .

echo "--> Compiling iOS Simulator (amd64)..."
CGO_ENABLED=1 GOOS=ios GOARCH=amd64 \
CC="${CLANG_IPHONESIM}" \
CGO_CFLAGS="-isysroot ${SDK_IPHONESIM} -mios-simulator-version-min=15.0 -arch x86_64" \
CGO_LDFLAGS="-isysroot ${SDK_IPHONESIM} -mios-simulator-version-min=15.0 -arch x86_64" \
    go build -C "${FFI_DIR}" -buildmode=c-archive -ldflags="-s -w" \
    -o "${BUILD_DIR}/iossim-amd64/libtailnet.a" .

mkdir -p "${BUILD_DIR}/iossim-universal"
lipo -create \
    "${BUILD_DIR}/iossim-arm64/libtailnet.a" \
    "${BUILD_DIR}/iossim-amd64/libtailnet.a" \
    -output "${BUILD_DIR}/iossim-universal/libtailnet.a"
cp "${BUILD_DIR}/iossim-arm64/libtailnet.h" "${BUILD_DIR}/iossim-universal/tailnet.h"

# 4. Prepare Headers & Modulemap
for dir in "${BUILD_DIR}/macos-universal" "${BUILD_DIR}/ios-arm64" "${BUILD_DIR}/iossim-universal"; do
    mkdir -p "${dir}/Headers"
    cp "${BUILD_DIR}/macos-universal/tailnet.h" "${dir}/Headers/tailnet.h"
    cat << 'EOF' > "${dir}/Headers/module.modulemap"
module libtailnet {
    header "tailnet.h"
    export *
}
EOF
done

# 5. Assemble XCFramework
echo "==> Creating Tailnet.xcframework..."
XCFRAMEWORK_OUT="${DIST_DIR}/Tailnet.xcframework"
rm -rf "${XCFRAMEWORK_OUT}"

xcodebuild -create-xcframework \
    -library "${BUILD_DIR}/macos-universal/libtailnet.a" \
    -headers "${BUILD_DIR}/macos-universal/Headers" \
    -library "${BUILD_DIR}/ios-arm64/libtailnet.a" \
    -headers "${BUILD_DIR}/ios-arm64/Headers" \
    -library "${BUILD_DIR}/iossim-universal/libtailnet.a" \
    -headers "${BUILD_DIR}/iossim-universal/Headers" \
    -output "${XCFRAMEWORK_OUT}"

echo "==> Successfully created ${XCFRAMEWORK_OUT}"
