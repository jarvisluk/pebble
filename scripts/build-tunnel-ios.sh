#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_BASE="$ROOT/build/mobile/ios"
MOD_FW="$ROOT/apps/mobile/modules/bothub-tunnel/ios/Frameworks"

CLANG="$(xcrun --find clang)"
IOS_SDK="$(xcrun --sdk iphoneos --show-sdk-path)"
SIM_SDK="$(xcrun --sdk iphonesimulator --show-sdk-path)"

compute_hash() {
  {
    printf '%s\n%s\n' "$IOS_SDK" "$SIM_SDK"
    find "$ROOT/cmd/libbothub" "$ROOT/internal/tunnel" -type f -name '*.go' | sort | xargs shasum -a 256
    shasum -a 256 "$ROOT/go.mod" "$ROOT/go.sum" "$ROOT/scripts/build-tunnel-ios.sh"
  } | shasum -a 256 | awk '{print $1}'
}

BUILD_HASH="$(compute_hash)"
HASH_FILE="$OUT_BASE/.build-hash"

if [ "${FORCE_TUNNEL_REBUILD:-0}" != "1" ] && [ -f "$HASH_FILE" ] && [ "$(cat "$HASH_FILE")" = "$BUILD_HASH" ] \
  && [ -f "$OUT_BASE/BotHubTunnel.xcframework/Info.plist" ] && [ -f "$MOD_FW/BotHubTunnel.xcframework/Info.plist" ]; then
  echo "iOS tunnel up-to-date (hash ${BUILD_HASH:0:12}), skipped rebuild."
  exit 0
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/device/headers" "$WORK/sim-arm64" "$WORK/sim-amd64" "$WORK/sim/headers" "$OUT_BASE"

build_slice() {
  local goarch="$1" sdk="$2" target="$3" dir="$4"
  CGO_ENABLED=1 GOOS=ios GOARCH="$goarch" CC="$CLANG" \
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

build_slice arm64 "$IOS_SDK" "arm64-apple-ios16.0" "$WORK/device" &
pid_dev=$!
build_slice arm64 "$SIM_SDK" "arm64-apple-ios16.0-simulator" "$WORK/sim-arm64" &
pid_sim_arm64=$!
build_slice amd64 "$SIM_SDK" "x86_64-apple-ios16.0-simulator" "$WORK/sim-amd64" &
pid_sim_amd64=$!

wait "$pid_dev"
wait "$pid_sim_arm64"
wait "$pid_sim_amd64"

mv "$WORK/device/libbothub.h" "$WORK/device/headers/libbothub.h"
write_modulemap "$WORK/device/headers"

lipo -create "$WORK/sim-arm64/libbothub.a" "$WORK/sim-amd64/libbothub.a" -output "$WORK/sim/libbothub.a"
mv "$WORK/sim-arm64/libbothub.h" "$WORK/sim/headers/libbothub.h"
write_modulemap "$WORK/sim/headers"

rm -rf "$OUT_BASE/BotHubTunnel.xcframework"
xcodebuild -create-xcframework \
  -library "$WORK/device/libbothub.a" -headers "$WORK/device/headers" \
  -library "$WORK/sim/libbothub.a" -headers "$WORK/sim/headers" \
  -output "$OUT_BASE/BotHubTunnel.xcframework"

rm -rf "$MOD_FW/BotHubTunnel.xcframework"
mkdir -p "$MOD_FW"
cp -R "$OUT_BASE/BotHubTunnel.xcframework" "$MOD_FW/BotHubTunnel.xcframework"
printf '%s\n' "$BUILD_HASH" > "$HASH_FILE"

echo "Built $OUT_BASE/BotHubTunnel.xcframework"
