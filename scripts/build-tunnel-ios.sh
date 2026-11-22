#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_BASE="$ROOT/build/mobile/ios"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

CLANG="$(xcrun --find clang)"
IOS_SDK="$(xcrun --sdk iphoneos --show-sdk-path)"
SIM_SDK="$(xcrun --sdk iphonesimulator --show-sdk-path)"
MAC_SDK="$(xcrun --sdk macosx --show-sdk-path)"

mkdir -p "$WORK/device/headers" "$WORK/sim-arm64" "$WORK/sim-amd64" "$WORK/sim/headers" "$WORK/mac-arm64" "$WORK/mac-amd64" "$WORK/mac/headers" "$OUT_BASE"

build_slice() {
  local goarch="$1" sdk="$2" target="$3" dir="$4" goos="${5:-ios}"
  CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" CC="$CLANG" \
    CGO_CFLAGS="-isysroot $sdk -target $target" \
    CGO_LDFLAGS="-isysroot $sdk -target $target" \
    go build -trimpath -ldflags="-s -w" -buildmode=c-archive \
    -o "$dir/libbothub.a" "$ROOT/cmd/libbothub"
}

write_modulemap() {
  local hdr_dir="$1"
  cat > "$hdr_dir/module.modulemap" <<'EOF'
module BotHubTunnelC {
  header "libbothub.h"
  export *
}
EOF
}

build_slice arm64 "$IOS_SDK" "arm64-apple-ios16.0" "$WORK/device"
mv "$WORK/device/libbothub.h" "$WORK/device/headers/libbothub.h"
write_modulemap "$WORK/device/headers"

build_slice arm64 "$SIM_SDK" "arm64-apple-ios16.0-simulator" "$WORK/sim-arm64"
build_slice amd64 "$SIM_SDK" "x86_64-apple-ios16.0-simulator" "$WORK/sim-amd64"
lipo -create "$WORK/sim-arm64/libbothub.a" "$WORK/sim-amd64/libbothub.a" -output "$WORK/sim/libbothub.a"
mv "$WORK/sim-arm64/libbothub.h" "$WORK/sim/headers/libbothub.h"
write_modulemap "$WORK/sim/headers"

build_slice arm64 "$MAC_SDK" "arm64-apple-macos14.0" "$WORK/mac-arm64" darwin
build_slice amd64 "$MAC_SDK" "x86_64-apple-macos14.0" "$WORK/mac-amd64" darwin
lipo -create "$WORK/mac-arm64/libbothub.a" "$WORK/mac-amd64/libbothub.a" -output "$WORK/mac/libbothub.a"
mv "$WORK/mac-arm64/libbothub.h" "$WORK/mac/headers/libbothub.h"
write_modulemap "$WORK/mac/headers"

rm -rf "$OUT_BASE/BotHubTunnel.xcframework"
xcodebuild -create-xcframework \
  -library "$WORK/device/libbothub.a" -headers "$WORK/device/headers" \
  -library "$WORK/sim/libbothub.a" -headers "$WORK/sim/headers" \
  -library "$WORK/mac/libbothub.a" -headers "$WORK/mac/headers" \
  -output "$OUT_BASE/BotHubTunnel.xcframework"

MOD_FW="$ROOT/apps/mobile/modules/bothub-tunnel/ios/Frameworks"
rm -rf "$MOD_FW/BotHubTunnel.xcframework"
mkdir -p "$MOD_FW"
cp -R "$OUT_BASE/BotHubTunnel.xcframework" "$MOD_FW/BotHubTunnel.xcframework"

echo "Built $OUT_BASE/BotHubTunnel.xcframework"
