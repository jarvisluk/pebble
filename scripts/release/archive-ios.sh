#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

MODE="archive"
EXPORT_OPTIONS=""

while [ $# -gt 0 ]; do
  case "$1" in
    --export)
      MODE="export"
      shift
      ;;
    --upload)
      MODE="upload"
      shift
      ;;
    --export-options)
      EXPORT_OPTIONS="${2:-}"
      shift 2
      ;;
    -h|--help)
      cat <<'EOF'
Usage: scripts/release/archive-ios.sh [--export | --upload] [--export-options <ExportOptions.plist>]

Creates a signed Release .xcarchive for iOS/iPadOS (never publishes unsigned IPA to GitHub Releases).
Requires apps/apple/Config/Signing.local.xcconfig with DEVELOPMENT_TEAM and APP_BUNDLE_ID configured.
EOF
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

LOCAL_XCCONFIG="$ROOT/apps/apple/Config/Signing.local.xcconfig"
if [ ! -f "$LOCAL_XCCONFIG" ]; then
  echo "ERROR: $LOCAL_XCCONFIG not found; copy Signing.local.xcconfig.example and fill DEVELOPMENT_TEAM & APP_BUNDLE_ID" >&2
  exit 1
fi

TEAM_ID="$(awk -F= '/^[[:space:]]*DEVELOPMENT_TEAM[[:space:]]*=/{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2}' "$LOCAL_XCCONFIG" | tail -n 1)"
BUNDLE_ID="$(awk -F= '/^[[:space:]]*APP_BUNDLE_ID[[:space:]]*=/{gsub(/^[[:space:]]+|[[:space:]]+$/, "", $2); print $2}' "$LOCAL_XCCONFIG" | tail -n 1)"

if [ -z "$TEAM_ID" ] || [ -z "$BUNDLE_ID" ] || [ "$BUNDLE_ID" = "local.pebble.mobile" ]; then
  echo "ERROR: set a real DEVELOPMENT_TEAM and non-placeholder APP_BUNDLE_ID in $LOCAL_XCCONFIG" >&2
  exit 1
fi

VERSION="$(python3 scripts/version.py show --field version)"
OUT_DIR="$ROOT/build/release"
ARCHIVE_PATH="$OUT_DIR/Pebble-iOS-${VERSION}.xcarchive"
EXPORT_DIR="$OUT_DIR/Pebble-iOS-${VERSION}-export"

mkdir -p "$OUT_DIR"
make ios-tunnel

rm -rf "$ARCHIVE_PATH"
xcodebuild archive \
  -project "$ROOT/apps/apple/Pebble.xcodeproj" \
  -scheme Pebble \
  -configuration Release \
  -destination 'generic/platform=iOS' \
  -archivePath "$ARCHIVE_PATH"

echo "Created $ARCHIVE_PATH"

if [ "$MODE" = "archive" ]; then
  echo "Open $ARCHIVE_PATH in Xcode Organizer to distribute to TestFlight / App Store."
  exit 0
fi

TMP_PLIST=""
cleanup() {
  if [ -n "$TMP_PLIST" ] && [ -f "$TMP_PLIST" ]; then
    rm -f "$TMP_PLIST"
  fi
}
trap cleanup EXIT

if [ -z "$EXPORT_OPTIONS" ]; then
  TMP_PLIST="$(mktemp /tmp/pebble-ios-export.XXXXXX.plist)"
  DEST_VAL="export"
  if [ "$MODE" = "upload" ]; then
    DEST_VAL="upload"
  fi
  cat > "$TMP_PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>method</key>
  <string>app-store-connect</string>
  <key>destination</key>
  <string>${DEST_VAL}</string>
  <key>signingStyle</key>
  <string>automatic</string>
  <key>teamID</key>
  <string>${TEAM_ID}</string>
</dict>
</plist>
EOF
  EXPORT_OPTIONS="$TMP_PLIST"
fi

rm -rf "$EXPORT_DIR"
xcodebuild -exportArchive \
  -archivePath "$ARCHIVE_PATH" \
  -exportPath "$EXPORT_DIR" \
  -exportOptionsPlist "$EXPORT_OPTIONS"

echo "Completed iOS $MODE in $EXPORT_DIR"
