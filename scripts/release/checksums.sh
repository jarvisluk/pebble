#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

DIR="${1:-}"
if [ -z "$DIR" ]; then
  VERSION="$(python3 scripts/version.py show --field version)"
  DIR="$ROOT/dist/release/$VERSION"
fi

if [ ! -d "$DIR" ]; then
  echo "ERROR: release directory not found: $DIR" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  HASH_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  HASH_CMD=(shasum -a 256)
else
  echo "ERROR: neither sha256sum nor shasum found" >&2
  exit 1
fi

(
  cd "$DIR"
  rm -f SHA256SUMS.txt
  files=()
  while IFS= read -r f; do
    files+=("$f")
  done < <(find . -maxdepth 1 -type f ! -name 'SHA256SUMS.txt' -exec basename {} \; | LC_ALL=C sort)

  if [ ${#files[@]} -eq 0 ]; then
    echo "ERROR: no release artifacts found in $DIR" >&2
    exit 1
  fi

  "${HASH_CMD[@]}" "${files[@]}" > SHA256SUMS.txt
)

echo "Generated $DIR/SHA256SUMS.txt"
