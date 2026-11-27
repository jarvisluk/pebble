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

UNIVERSAL="${UNIVERSAL:-0}"
make app APP_CONFIG=Release UNIVERSAL="$UNIVERSAL"

if [ "$UNIVERSAL" = "1" ]; then
  ARCH_LABEL="universal"
else
  ARCH_LABEL="$(uname -m)"
fi

ZIP_PATH="$OUT_DIR/Pebble_${VERSION}_macos_${ARCH_LABEL}.zip"
DMG_PATH="$OUT_DIR/Pebble_${VERSION}_macos_${ARCH_LABEL}.dmg"

rm -f "$ZIP_PATH" "$DMG_PATH"

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
cp -R "$ROOT/bin/Pebble.app" "$STAGE/Pebble.app"
ln -s /Applications "$STAGE/Applications"

ditto -c -k --sequesterRsrc --keepParent "$ROOT/bin/Pebble.app" "$ZIP_PATH"
hdiutil create -volname "Pebble" -srcfolder "$STAGE" -ov -format UDZO "$DMG_PATH" >/dev/null

# Optional Apple Notarization when Developer ID signed + credentials provided
SIGN_AUTH="$(codesign -dvv "$ROOT/bin/Pebble.app" 2>&1 | grep '^Authority=' | head -n 1 || true)"
IS_DEV_ID=0
if printf '%s' "$SIGN_AUTH" | grep -q "Developer ID Application"; then
  IS_DEV_ID=1
fi

submit_notary() {
  local target_file="$1"
  if [ -n "${APPLE_NOTARY_KEY_PATH:-}" ] && [ -n "${APPLE_NOTARY_KEY_ID:-}" ] && [ -n "${APPLE_NOTARY_ISSUER_ID:-}" ]; then
    xcrun notarytool submit "$target_file" \
      --key "$APPLE_NOTARY_KEY_PATH" \
      --key-id "$APPLE_NOTARY_KEY_ID" \
      --issuer "$APPLE_NOTARY_ISSUER_ID" \
      --wait
    return 0
  fi
  if [ -n "${APPLE_NOTARY_APPLE_ID:-}" ] && [ -n "${APPLE_NOTARY_TEAM_ID:-}" ] && [ -n "${APPLE_NOTARY_PASSWORD:-}" ]; then
    xcrun notarytool submit "$target_file" \
      --apple-id "$APPLE_NOTARY_APPLE_ID" \
      --team-id "$APPLE_NOTARY_TEAM_ID" \
      --password "$APPLE_NOTARY_PASSWORD" \
      --wait
    return 0
  fi
  return 1
}

if [ "$IS_DEV_ID" = "1" ]; then
  if submit_notary "$DMG_PATH"; then
    xcrun stapler staple "$DMG_PATH"
    xcrun stapler staple "$ROOT/bin/Pebble.app"
    rm -f "$ZIP_PATH"
    ditto -c -k --sequesterRsrc --keepParent "$ROOT/bin/Pebble.app" "$ZIP_PATH"
    echo "Notarized and stapled $DMG_PATH and $ZIP_PATH"
  else
    echo "note: Developer ID signed, but APPLE_NOTARY_* credentials not set; skipped notarization"
  fi
else
  echo "note: $ROOT/bin/Pebble.app is not Developer ID signed (${SIGN_AUTH:-ad-hoc}); skipped notarization"
fi

echo "Created $ZIP_PATH"
echo "Created $DMG_PATH"
