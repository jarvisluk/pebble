#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_BASE="$ROOT/build/mobile/android/jniLibs"
MOD_LIBS="$ROOT/apps/mobile/modules/bothub-tunnel/android/src/main/jniLibs"
NATIVE_LIBS="$ROOT/apps/android/app/src/main/jniLibs"

ANDROID_SDK="${ANDROID_HOME:-}"
if [ -z "$ANDROID_SDK" ]; then
  if [ -d "/opt/homebrew/share/android-commandlinetools" ]; then
    ANDROID_SDK="/opt/homebrew/share/android-commandlinetools"
  elif [ -d "$HOME/Library/Android/sdk" ]; then
    ANDROID_SDK="$HOME/Library/Android/sdk"
  fi
fi

NDK="${ANDROID_NDK_HOME:-}"
if [ -z "$NDK" ] && [ -n "$ANDROID_SDK" ] && [ -d "$ANDROID_SDK/ndk" ]; then
  NDK="$(find "$ANDROID_SDK/ndk" -maxdepth 1 -mindepth 1 | sort -V | tail -n 1)"
fi
if [ -z "$NDK" ] || [ ! -d "$NDK" ]; then
  echo "ANDROID_NDK_HOME (or ANDROID_HOME/ndk) is not set" >&2
  exit 1
fi

HOST_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
HOST_ARCH="$(uname -m)"
PREBUILT="$NDK/toolchains/llvm/prebuilt/${HOST_OS}-${HOST_ARCH}"
if [ ! -d "$PREBUILT" ]; then
  PREBUILT="$NDK/toolchains/llvm/prebuilt/${HOST_OS}-x86_64"
fi

compute_hash() {
  {
    printf '%s\n' "$PREBUILT"
    find "$ROOT/cmd/libbothub" "$ROOT/internal/tunnel" -type f -name '*.go' | sort | xargs shasum -a 256
    shasum -a 256 "$ROOT/go.mod" "$ROOT/go.sum" "$ROOT/scripts/build-tunnel-android.sh"
  } | shasum -a 256 | awk '{print $1}'
}

sync_abi_dir() {
  local src_dir="$1" dst_dir="$2"
  mkdir -p "$dst_dir"
  local f base
  for f in "$src_dir"/*; do
    [ -e "$f" ] || continue
    base="$(basename "$f")"
    if [ ! -e "$dst_dir/$base" ] || ! cmp -s "$f" "$dst_dir/$base"; then
      cp "$f" "$dst_dir/$base"
    fi
  done
  for f in "$dst_dir"/*; do
    [ -e "$f" ] || continue
    base="$(basename "$f")"
    if [ ! -e "$src_dir/$base" ]; then
      rm -f "$f"
    fi
  done
}

sync_all() {
  for abi in arm64-v8a x86_64; do
    sync_abi_dir "$OUT_BASE/$abi" "$MOD_LIBS/$abi"
    sync_abi_dir "$OUT_BASE/$abi" "$NATIVE_LIBS/$abi"
  done
}

BUILD_HASH="$(compute_hash)"
HASH_FILE="$OUT_BASE/.build-hash"

if [ "${FORCE_TUNNEL_REBUILD:-0}" != "1" ] && [ -f "$HASH_FILE" ] && [ "$(cat "$HASH_FILE")" = "$BUILD_HASH" ] \
  && [ -f "$OUT_BASE/arm64-v8a/libbothub.so" ] && [ -f "$OUT_BASE/x86_64/libbothub.so" ]; then
  sync_all
  echo "Android tunnel up-to-date (hash ${BUILD_HASH:0:12}), skipped rebuild."
  exit 0
fi

build_abi() {
  local goarch="$1" triple="$2" abi="$3"
  local cc="$PREBUILT/bin/${triple}24-clang"
  mkdir -p "$OUT_BASE/$abi"
  CGO_ENABLED=1 GOOS=android GOARCH="$goarch" CC="$cc" \
    CGO_LDFLAGS="-Wl,-z,max-page-size=16384" \
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared \
    -o "$OUT_BASE/$abi/libbothub.so" "$ROOT/cmd/libbothub"
}

build_abi arm64 aarch64-linux-android arm64-v8a &
pid_arm64=$!
build_abi amd64 x86_64-linux-android x86_64 &
pid_amd64=$!

wait "$pid_arm64"
wait "$pid_amd64"

printf '%s\n' "$BUILD_HASH" > "$HASH_FILE"
sync_all

echo "Built Android jniLibs under $OUT_BASE (synced to $MOD_LIBS and $NATIVE_LIBS)"
