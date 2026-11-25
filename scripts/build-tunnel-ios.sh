#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_BASE="$ROOT/build/mobile/ios"
TAGS_FILE="$ROOT/scripts/go-build-tags.txt"

if [ "${GO_TAGS+set}" != "set" ] && [ -f "$TAGS_FILE" ]; then
  GO_TAGS="$(grep -v '^#' "$TAGS_FILE" | grep -v '^$' | paste -sd, -)"
else
  GO_TAGS="${GO_TAGS:-}"
fi

IOS_UNIVERSAL_SIM="${IOS_UNIVERSAL_SIM:-1}"

CLANG="$(xcrun --find clang)"
IOS_SDK="$(xcrun --sdk iphoneos --show-sdk-path)"
SIM_SDK="$(xcrun --sdk iphonesimulator --show-sdk-path)"

compute_hash() {
  {
    printf '%s\n%s\n%s\nIOS_UNIVERSAL_SIM=%s\n' "$IOS_SDK" "$SIM_SDK" "$GO_TAGS" "$IOS_UNIVERSAL_SIM"
    find "$ROOT/cmd/libbothub" "$ROOT/internal/tunnel" -type f -name '*.go' | sort | xargs shasum -a 256
    shasum -a 256 "$ROOT/go.mod" "$ROOT/go.sum" "$ROOT/scripts/build-tunnel-ios.sh"
    if [ -f "$TAGS_FILE" ]; then
      shasum -a 256 "$TAGS_FILE"
    fi
  } | shasum -a 256 | awk '{print $1}'
}

copy_tree() {
  local src="$1" dst="$2"
  cp -cR "$src" "$dst" 2>/dev/null || cp -R "$src" "$dst"
}

BUILD_HASH="$(compute_hash)"
HASH_FILE="$OUT_BASE/.build-hash"
CACHE_ROOT="${PEBBLE_TUNNEL_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/pebble-tunnel}/ios"
CACHE_DIR="$CACHE_ROOT/$BUILD_HASH"

if [ "${FORCE_TUNNEL_REBUILD:-0}" != "1" ] && [ -f "$HASH_FILE" ] && [ "$(cat "$HASH_FILE")" = "$BUILD_HASH" ] \
  && [ -f "$OUT_BASE/PebbleTunnel.xcframework/Info.plist" ]; then
  echo "iOS tunnel up-to-date (hash ${BUILD_HASH:0:12}), skipped rebuild."
  exit 0
fi

if [ "${FORCE_TUNNEL_REBUILD:-0}" != "1" ] && [ -f "$CACHE_DIR/PebbleTunnel.xcframework/Info.plist" ]; then
  mkdir -p "$OUT_BASE"
  rm -rf "$OUT_BASE/BotHubTunnel.xcframework" "$OUT_BASE/PebbleTunnel.xcframework"
  copy_tree "$CACHE_DIR/PebbleTunnel.xcframework" "$OUT_BASE/PebbleTunnel.xcframework"
  printf '%s\n' "$BUILD_HASH" > "$HASH_FILE"
  echo "iOS tunnel restored from shared cache (hash ${BUILD_HASH:0:12})."
  exit 0
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

mkdir -p "$WORK/device/headers" "$WORK/sim-arm64" "$WORK/sim-amd64" "$WORK/sim/headers" "$OUT_BASE"

build_slice() {
  local goarch="$1" sdk="$2" target="$3" dir="$4"
  local -a tag_args=()
  if [ -n "$GO_TAGS" ]; then
    tag_args=(-tags="$GO_TAGS")
  fi
  CGO_ENABLED=1 GOOS=ios GOARCH="$goarch" CC="$CLANG" \
    CGO_CFLAGS="-isysroot $sdk -target $target" \
    CGO_LDFLAGS="-isysroot $sdk -target $target" \
    go build -trimpath ${tag_args[@]+"${tag_args[@]}"} -ldflags="-s -w" -buildmode=c-archive \
    -o "$dir/libbothub.a" "$ROOT/cmd/libbothub"
}

write_modulemap() {
  local hdr_dir="$1"
  cat > "$hdr_dir/module.modulemap" <<'EOF'
module PebbleTunnelC {
  header "libbothub.h"
  export *
}
EOF
}

build_slice arm64 "$IOS_SDK" "arm64-apple-ios16.0" "$WORK/device" &
pid_dev=$!
build_slice arm64 "$SIM_SDK" "arm64-apple-ios16.0-simulator" "$WORK/sim-arm64" &
pid_sim_arm64=$!

if [ "$IOS_UNIVERSAL_SIM" = "1" ]; then
  build_slice amd64 "$SIM_SDK" "x86_64-apple-ios16.0-simulator" "$WORK/sim-amd64" &
  pid_sim_amd64=$!
fi

wait "$pid_dev"
wait "$pid_sim_arm64"
if [ "$IOS_UNIVERSAL_SIM" = "1" ]; then
  wait "$pid_sim_amd64"
fi

mv "$WORK/device/libbothub.h" "$WORK/device/headers/libbothub.h"
write_modulemap "$WORK/device/headers"

if [ "$IOS_UNIVERSAL_SIM" = "1" ]; then
  lipo -create "$WORK/sim-arm64/libbothub.a" "$WORK/sim-amd64/libbothub.a" -output "$WORK/sim/libbothub.a"
else
  mv "$WORK/sim-arm64/libbothub.a" "$WORK/sim/libbothub.a"
fi
mv "$WORK/sim-arm64/libbothub.h" "$WORK/sim/headers/libbothub.h"
write_modulemap "$WORK/sim/headers"

rm -rf "$OUT_BASE/BotHubTunnel.xcframework" "$OUT_BASE/PebbleTunnel.xcframework"
xcodebuild -create-xcframework \
  -library "$WORK/device/libbothub.a" -headers "$WORK/device/headers" \
  -library "$WORK/sim/libbothub.a" -headers "$WORK/sim/headers" \
  -output "$OUT_BASE/PebbleTunnel.xcframework"

printf '%s\n' "$BUILD_HASH" > "$HASH_FILE"

mkdir -p "$CACHE_ROOT"
if [ ! -d "$CACHE_DIR" ] || [ "${FORCE_TUNNEL_REBUILD:-0}" = "1" ]; then
  TMP_CACHE="$CACHE_ROOT/.tmp.$$.$RANDOM"
  mkdir -p "$TMP_CACHE"
  copy_tree "$OUT_BASE/PebbleTunnel.xcframework" "$TMP_CACHE/PebbleTunnel.xcframework"
  if [ "${FORCE_TUNNEL_REBUILD:-0}" = "1" ]; then
    rm -rf "$CACHE_DIR"
  fi
  mv "$TMP_CACHE" "$CACHE_DIR" 2>/dev/null || rm -rf "$TMP_CACHE"
fi

echo "Built $OUT_BASE/PebbleTunnel.xcframework"
