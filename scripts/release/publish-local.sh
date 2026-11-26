#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

TAG=""
DIR=""
DRAFT=0

while [ $# -gt 0 ]; do
  case "$1" in
    --tag)
      TAG="${2:-}"
      shift 2
      ;;
    --dir)
      DIR="${2:-}"
      shift 2
      ;;
    --draft)
      DRAFT=1
      shift
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

if ! command -v gh >/dev/null 2>&1; then
  echo "ERROR: gh CLI is required" >&2
  exit 1
fi
gh auth status >/dev/null

if [ -z "$TAG" ]; then
  TAG="$(python3 scripts/version.py show --field tag)"
fi
VERSION="${TAG#v}"

python3 scripts/version.py check --expect-tag "$TAG"

if [ -z "$DIR" ]; then
  DIR="$ROOT/dist/release/$VERSION"
fi
"$ROOT/scripts/release/checksums.sh" "$DIR"

NOTES_FILE="$(mktemp /tmp/pebble-release-notes.XXXXXX.md)"
trap 'rm -f "$NOTES_FILE"' EXIT
python3 scripts/version.py changelog --to "$TAG" --version "$VERSION" --no-header > "$NOTES_FILE"

ARGS=("$TAG" --title "$TAG" --notes-file "$NOTES_FILE")
if [ "$(python3 scripts/version.py show --field prerelease)" = "true" ]; then
  ARGS+=(--prerelease)
fi
if [ "$DRAFT" = "1" ]; then
  ARGS+=(--draft)
fi

ASSETS=()
while IFS= read -r f; do
  ASSETS+=("$f")
done < <(find "$DIR" -maxdepth 1 -type f | LC_ALL=C sort)

if gh release view "$TAG" >/dev/null 2>&1; then
  gh release upload "$TAG" "${ASSETS[@]}" --clobber
  gh release edit "$TAG" --notes-file "$NOTES_FILE"
else
  gh release create "${ARGS[@]}" "${ASSETS[@]}"
fi
