#!/bin/bash
# Picks the bundled blitzpatch + chdman for this CPU. CHDMAN=/path/to/chdman overrides the bundled chdman.
cd "$(dirname "$0")" || exit 1
case "$(uname -m)" in
  x86_64|amd64)  BP=./blitzpatch;       CH=./chdman ;;
  aarch64|arm64) BP=./blitzpatch_arm64; CH=./chdman_arm64 ;;
  *) echo "Unsupported CPU: $(uname -m) (kit has x86_64 and arm64 builds)" >&2; exit 1 ;;
esac
[ -x "$BP" ] || { echo "Missing or not executable: $BP" >&2; exit 1; }
CH="${CHDMAN:-$CH}"
if [ ! -x "$CH" ] && ! command -v "$CH" >/dev/null 2>&1; then
  echo "chdman not found: $CH (set CHDMAN=/path/to/chdman)" >&2; exit 1
fi
exec "$BP" run --chdman "$CH" "$@"
