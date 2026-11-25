#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_BASE="$ROOT/build/mobile/android/jniLibs"
MOD_LIBS="$ROOT/apps/mobile/modules/bothub-tunnel/android/src/main/jniLibs"
NATIVE_LIBS="$ROOT/apps/android/app/src/main/jniLibs"
TAGS_FILE="$ROOT/scripts/go-build-tags.txt"

if [ "${GO_TAGS+set}" != "set" ] && [ -f "$TAGS_FILE" ]; then
  GO_TAGS="$(grep -v '^#' "$TAGS_FILE" | grep -v '^$' | paste -sd, -)"
else
  GO_TAGS="${GO_TAGS:-}"
fi

RAW_ABIS="${ANDROID_ABIS:-arm64-v8a,x86_64}"
ABIS_NORM="$(printf '%s' "$RAW_ABIS" | tr ',' '\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' | grep -v '^$' | sort -u | paste -sd, -)"
if [ -z "$ABIS_NORM" ]; then
  ABIS_NORM="arm64-v8a,x86_64"
fi

IFS=',' read -r -a ABIS <<< "$ABIS_NORM"
for abi in "${ABIS[@]}"; do
  case "$abi" in
    arm64-v8a|x86_64) ;;
    *)
      echo "Unsupported Android ABI: $abi (supported: arm64-v8a, x86_64)" >&2
      exit 1
      ;;
  esac
done

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
    printf '%s\n%s\n%s\n' "$PREBUILT" "$GO_TAGS" "$ABIS_NORM"
    find "$ROOT/cmd/libbothub" "$ROOT/internal/tunnel" -type f -name '*.go' | sort | xargs shasum -a 256
    shasum -a 256 "$ROOT/go.mod" "$ROOT/go.sum" "$ROOT/scripts/build-tunnel-android.sh"
    if [ -f "$TAGS_FILE" ]; then
      shasum -a 256 "$TAGS_FILE"
    fi
  } | shasum -a 256 | awk '{print $1}'
}

copy_tree() {
  local src="$1" dst="$2"
  cp -cR "$src" "$dst" 2>/dev/null || cp -R "$src" "$dst"
}

sync_abi_dir() {
  local src_dir="$1" dst_dir="$2"
  mkdir -p "$dst_dir"
  local f base
  for f in "$src_dir"/*; do
    [ -e "$f" ] || continue
    base="$(basename "$f")"
    if [ ! -e "$dst_dir/$base" ] || ! cmp -s "$f" "$dst_dir/$base"; then
      cp -c "$f" "$dst_dir/$base" 2>/dev/null || cp "$f" "$dst_dir/$base"
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
  local abi
  for abi in "${ABIS[@]}"; do
    sync_abi_dir "$OUT_BASE/$abi" "$NATIVE_LIBS/$abi"
    if [ "${SYNC_LEGACY_MOBILE:-0}" = "1" ]; then
      sync_abi_dir "$OUT_BASE/$abi" "$MOD_LIBS/$abi"
    fi
  done
}

have_all_outputs() {
  local base="$1" abi
  for abi in "${ABIS[@]}"; do
    [ -f "$base/$abi/libbothub.so" ] || return 1
  done
  return 0
}

BUILD_HASH="$(compute_hash)"
HASH_FILE="$OUT_BASE/.build-hash"
CACHE_ROOT="${PEBBLE_TUNNEL_CACHE_DIR:-${XDG_CACHE_HOME:-$HOME/.cache}/pebble-tunnel}/android"
CACHE_DIR="$CACHE_ROOT/$BUILD_HASH"

if [ "${FORCE_TUNNEL_REBUILD:-0}" != "1" ] && [ -f "$HASH_FILE" ] && [ "$(cat "$HASH_FILE")" = "$BUILD_HASH" ] \
  && have_all_outputs "$OUT_BASE"; then
  sync_all
  echo "Android tunnel up-to-date (hash ${BUILD_HASH:0:12}, abis ${ABIS_NORM}), skipped rebuild."
  exit 0
fi

if [ "${FORCE_TUNNEL_REBUILD:-0}" != "1" ] && have_all_outputs "$CACHE_DIR"; then
  mkdir -p "$OUT_BASE"
  for abi in "${ABIS[@]}"; do
    rm -rf "$OUT_BASE/$abi"
    copy_tree "$CACHE_DIR/$abi" "$OUT_BASE/$abi"
  done
  printf '%s\n' "$BUILD_HASH" > "$HASH_FILE"
  sync_all
  echo "Android tunnel restored from shared cache (hash ${BUILD_HASH:0:12}, abis ${ABIS_NORM})."
  exit 0
fi

build_abi() {
  local goarch="$1" triple="$2" abi="$3"
  local cc="$PREBUILT/bin/${triple}24-clang"
  mkdir -p "$OUT_BASE/$abi"
  local -a tag_args=()
  if [ -n "$GO_TAGS" ]; then
    tag_args=(-tags="$GO_TAGS")
  fi
  CGO_ENABLED=1 GOOS=android GOARCH="$goarch" CC="$cc" \
    CGO_LDFLAGS="-Wl,-z,max-page-size=16384" \
    go build -trimpath ${tag_args[@]+"${tag_args[@]}"} -ldflags="-s -w" -buildmode=c-shared \
    -o "$OUT_BASE/$abi/libbothub.so" "$ROOT/cmd/libbothub"
}

pids=()
for abi in "${ABIS[@]}"; do
  case "$abi" in
    arm64-v8a)
      build_abi arm64 aarch64-linux-android arm64-v8a &
      pids+=($!)
      ;;
    x86_64)
      build_abi amd64 x86_64-linux-android x86_64 &
      pids+=($!)
      ;;
  esac
done

for pid in "${pids[@]}"; do
  wait "$pid"
done

printf '%s\n' "$BUILD_HASH" > "$HASH_FILE"
sync_all

mkdir -p "$CACHE_ROOT"
if [ ! -d "$CACHE_DIR" ] || [ "${FORCE_TUNNEL_REBUILD:-0}" = "1" ]; then
  TMP_CACHE="$CACHE_ROOT/.tmp.$$.$RANDOM"
  mkdir -p "$TMP_CACHE"
  for abi in "${ABIS[@]}"; do
    copy_tree "$OUT_BASE/$abi" "$TMP_CACHE/$abi"
  done
  if [ "${FORCE_TUNNEL_REBUILD:-0}" = "1" ]; then
    rm -rf "$CACHE_DIR"
  fi
  mv "$TMP_CACHE" "$CACHE_DIR" 2>/dev/null || rm -rf "$TMP_CACHE"
fi

echo "Built Android jniLibs under $OUT_BASE (abis ${ABIS_NORM}, synced to $NATIVE_LIBS)"
