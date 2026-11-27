#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION=""
OUT_DIR=""
SKIP_TESTS=0

while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --out-dir)
      OUT_DIR="${2:-}"
      shift 2
      ;;
    --skip-tests)
      SKIP_TESTS=1
      shift
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

make android-tunnel

if [ -n "${PEBBLE_KEYSTORE_PATH:-}" ]; then
  echo "Android signing: using release keystore ($PEBBLE_KEYSTORE_PATH)"
else
  echo "note: PEBBLE_KEYSTORE_PATH not set; Android release APK will fall back to debug signing"
fi

GRADLE_TASKS=(":app:assembleRelease")
if [ "$SKIP_TESTS" != "1" ]; then
  GRADLE_TASKS=(":app:testDebugUnitTest" ":app:assembleRelease")
fi

(cd "$ROOT/apps/android" && ./gradlew "${GRADLE_TASKS[@]}")

SRC_APK="$ROOT/apps/android/app/build/outputs/apk/release/app-release.apk"
if [ ! -f "$SRC_APK" ]; then
  echo "ERROR: expected APK not found at $SRC_APK" >&2
  exit 1
fi

DST_APK="$OUT_DIR/Pebble_${VERSION}_android.apk"
cp "$SRC_APK" "$DST_APK"
echo "Created $DST_APK"
