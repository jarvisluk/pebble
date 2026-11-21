#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_BASE="$ROOT/build/mobile/android/jniLibs"
MOD_LIBS="$ROOT/apps/mobile/modules/bothub-tunnel/android/src/main/jniLibs"

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

build_abi() {
  local goarch="$1" triple="$2" abi="$3"
  local cc="$PREBUILT/bin/${triple}24-clang"
  mkdir -p "$OUT_BASE/$abi"
  CGO_ENABLED=1 GOOS=android GOARCH="$goarch" CC="$cc" \
    CGO_LDFLAGS="-Wl,-z,max-page-size=16384" \
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared \
    -o "$OUT_BASE/$abi/libbothub.so" "$ROOT/cmd/libbothub"
}

build_abi arm64 aarch64-linux-android arm64-v8a
build_abi amd64 x86_64-linux-android x86_64

rm -rf "$MOD_LIBS"
mkdir -p "$MOD_LIBS"
cp -R "$OUT_BASE/arm64-v8a" "$OUT_BASE/x86_64" "$MOD_LIBS/"

echo "Built Android jniLibs under $OUT_BASE (synced to $MOD_LIBS)"
