#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

SKIP_WEB=0
VERSION=""
OUT_DIR=""

while [ $# -gt 0 ]; do
  case "$1" in
    --skip-web)
      SKIP_WEB=1
      shift
      ;;
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="${2:-}"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

if [ -z "$VERSION" ]; then
  VERSION="$(python3 scripts/version.py show --field version)"
fi
if [ -z "$OUT_DIR" ]; then
  OUT_DIR="$ROOT/dist/release/$VERSION"
fi
mkdir -p "$OUT_DIR"

if [ "$SKIP_WEB" != "1" ]; then
  make web
fi

GIT_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X bot-hub/internal/protocol.AppVersion=$VERSION -X bot-hub/internal/protocol.BuildCommit=$GIT_COMMIT -X bot-hub/internal/protocol.BuildDate=$BUILD_DATE"

PLATFORMS="${PLATFORMS:-linux/amd64 linux/arm64 darwin/arm64 darwin/amd64}"
HOST_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

for target in $PLATFORMS; do
  os="${target%/*}"
  arch="${target#*/}"

  cgo="${FORCE_CGO_ENABLED:-}"
  if [ -z "$cgo" ]; then
    if [ "$os" = "darwin" ] && [ "$HOST_OS" = "darwin" ]; then
      cgo="1"
    else
      cgo="0"
    fi
  fi

  pkg_name="pebble_${VERSION}_${os}_${arch}"
  stage="$WORK/$pkg_name"
  mkdir -p "$stage"

  bin_name="pebble"
  if [ "$os" = "windows" ]; then
    bin_name="pebble.exe"
  fi

  echo "Building $pkg_name (CGO_ENABLED=$cgo)..."
  CGO_ENABLED="$cgo" GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags="$LDFLAGS" -o "$stage/$bin_name" ./cmd/pebble

  cp "$ROOT/LICENSE" "$ROOT/README.md" "$ROOT/README.zh-CN.md" "$stage/"

  if [ "$os" = "windows" ]; then
    archive="$OUT_DIR/${pkg_name}.zip"
    rm -f "$archive"
    (cd "$WORK" && zip -q -r "$archive" "$pkg_name")
  else
    archive="$OUT_DIR/${pkg_name}.tar.gz"
    rm -f "$archive"
    tar -C "$WORK" -czf "$archive" "$pkg_name"
  fi
  echo "Created $archive"
done
