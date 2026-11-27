#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

usage() {
  cat <<'EOF'
Usage:
  scripts/release/prepare.sh <patch|minor|major|alpha|beta|rc|build|X.Y.Z[-pre]> [options]

Options:
  --base <patch|minor|major>  Base bump when entering prerelease from stable (default: minor)
  --build <N>                 Explicit build number (>= 1)
  --allow-branch              Allow running outside the 'main' branch
  --push                      Push commit and tag to origin automatically
  --dry-run                   Preview version bump and changelog without modifying files or git
EOF
}

if [ $# -lt 1 ]; then
  usage >&2
  exit 1
fi

TARGET="$1"
shift

BASE="minor"
EXPLICIT_BUILD=""
ALLOW_BRANCH=0
DO_PUSH=0
DRY_RUN=0

while [ $# -gt 0 ]; do
  case "$1" in
    --base)
      BASE="${2:-}"
      shift 2
      ;;
    --build)
      EXPLICIT_BUILD="${2:-}"
      shift 2
      ;;
    --allow-branch)
      ALLOW_BRANCH=1
      shift
      ;;
    --push)
      DO_PUSH=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

BRANCH="$(git rev-parse --abbrev-ref HEAD)"
if [ "$ALLOW_BRANCH" != "1" ] && [ "$BRANCH" != "main" ]; then
  echo "ERROR: release-prepare must run on 'main' (current: '$BRANCH'; pass --allow-branch to override)" >&2
  exit 1
fi

if [ "$DRY_RUN" != "1" ] && [ -n "$(git status --porcelain)" ]; then
  echo "ERROR: git working tree is not clean; commit or stash changes first" >&2
  exit 1
fi

python3 scripts/version.py check

BUMP_ARGS=("$TARGET" --base "$BASE")
if [ -n "$EXPLICIT_BUILD" ]; then
  BUMP_ARGS+=(--build "$EXPLICIT_BUILD")
fi

if [ "$DRY_RUN" = "1" ]; then
  DRY_OUT="$(python3 scripts/version.py bump "${BUMP_ARGS[@]}" --dry-run)"
  printf '%s\n' "$DRY_OUT"
  PREVIEW_VER="$(printf '%s\n' "$DRY_OUT" | sed -n 's/.*-> \([^ ]*\) (build .*/\1/p')"
  echo "--- Changelog preview ---"
  python3 scripts/version.py changelog --version "${PREVIEW_VER:-$TARGET}"
  exit 0
fi

python3 scripts/version.py bump "${BUMP_ARGS[@]}"

NEW_VERSION="$(python3 scripts/version.py show --field version)"
NEW_TAG="$(python3 scripts/version.py show --field tag)"

if git rev-parse -q --verify "refs/tags/$NEW_TAG" >/dev/null 2>&1; then
  echo "ERROR: git tag $NEW_TAG already exists" >&2
  exit 1
fi

python3 scripts/version.py changelog --version "$NEW_VERSION" --write CHANGELOG.md

STAGE_PATHS=(
  version.json
  CHANGELOG.md
  internal/protocol/protocol.go
  web/package.json
  apps/apple/Config/Version.xcconfig
  apps/android/version.properties
  build/darwin/Info.plist
)
if [ -f web/package-lock.json ]; then
  STAGE_PATHS+=(web/package-lock.json)
fi

git add "${STAGE_PATHS[@]}"
git commit -m "chore(release): $NEW_TAG"
git tag -a "$NEW_TAG" -m "Release $NEW_TAG"

echo "Prepared release $NEW_TAG on branch $BRANCH."
if [ "$DO_PUSH" = "1" ]; then
  git push origin "$BRANCH" "$NEW_TAG"
else
  echo "Next: git push origin $BRANCH $NEW_TAG"
fi
