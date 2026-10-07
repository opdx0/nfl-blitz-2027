#!/bin/bash
# build_kits.sh [patch.bpz] [release_id] [outdir]
# Builds blitz2027_kit_{windows,macos,linux}.zip (blitzpatch + chdman + bpz + scripts + README + LICENSES).
# Defaults: patch = <repo>/blitz2027.bpz, release_id = build id from README.md, outdir = <repo>/out (git-ignored).
# Needs: go, lipo (macOS), zip. Vendored binaries come from $VENDOR (default <repo>/vendor):
#   chdman.exe, chdman_macos (universal), chdman_linux_x86_64, chdman_linux_arm64, COPYING, GPL-2.0, README.md (provenance)
set -euo pipefail
REPO="$(cd "$(dirname "$0")/.." && pwd)"
BPZ="${1:-$REPO/blitz2027.bpz}"
REL="${2:-$(grep -o 'build [0-9a-z]*' "$REPO/README.md" | head -1 | cut -d' ' -f2)}"; : "${REL:=dev}"
OUTDIR="${3:-$REPO/out}"
VENDOR="${VENDOR:-$REPO/vendor}"
T="$REPO/dist/kit_templates"
BIN="$OUTDIR/bin"; STAGE="$OUTDIR/stage_$REL"
BPZ="$(cd "$(dirname "$BPZ")" && pwd)/$(basename "$BPZ")"
mkdir -p "$BIN" "$OUTDIR"; rm -rf "$STAGE"; mkdir -p "$STAGE"

echo "== go build"
( cd "$REPO/dist/go"
  for t in windows/amd64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
    o=${t%/*}; a=${t#*/}; e=""; [ "$o" = windows ] && e=.exe
    CGO_ENABLED=0 GOOS=$o GOARCH=$a go build -trimpath -ldflags "-s -w" -o "$BIN/blitzpatch_${o}_${a}$e" .
  done )
lipo -create "$BIN/blitzpatch_darwin_amd64" "$BIN/blitzpatch_darwin_arm64" -output "$BIN/blitzpatch_darwin_universal"
codesign -s - --force "$BIN/blitzpatch_darwin_universal" 2>/dev/null || true   # ad-hoc signature (arm64 requires one)

# header facts from the bpz
HOSTBP="$BIN/blitzpatch_darwin_$(uname -m | sed 's/x86_64/amd64/')"
INFO="$("$HOSTBP" info "$BPZ")"
TDATA="$(echo "$INFO" | awk '/^target chd data sha1/{print $NF}')"
TRAW="$(echo "$INFO" | awk '/^target sha1/{print $NF}')"
TMD5="$(echo "$INFO" | awk '/^target md5/{print $NF}')"
BPZ_MB="$(awk -v s="$(wc -c < "$BPZ")" 'BEGIN{printf "%.1f", s/1048576}')"
[ -n "$TDATA" ] || { echo "cannot read bpz header"; exit 1; }
echo "release $REL  target Data SHA1 $TDATA  raw md5 $TMD5  bpz ${BPZ_MB} MB"

fill() { # template out key=val...
  local f="$1" o="$2"; shift 2; cp "$f" "$o"
  for kv in "$@"; do python3 - "$o" "${kv%%=*}" "${kv#*=}" <<'PY'
import sys
p,k,v=sys.argv[1:4]; s=open(p).read().replace("@"+k+"@",v.replace("\\n","\n")); open(p,"w").write(s)
PY
  done
}
readme() { # platform run-text extra
  fill "$T/README.txt.in" "$1/README.txt" "PLATFORM=$2" "RELEASE=$REL" "BPZ_MB=$BPZ_MB" "TARGET_DATA_SHA1=$TDATA" "RUN=$3" "EXTRA=$4"
}
licenses() { # dir
  { echo "NFL Blitz 2027 patch - licenses ($REL)"; echo
    echo "== blitzpatch, PATCH scripts, blitz2027.bpz"
    echo "blitzpatch and the PATCH scripts: Apache License 2.0"
    echo "(full text below). blitz2027.bpz and the artwork/audio it carries are NOT under that licence: personal,"
    echo "non-commercial use only (see DATA_NOTICE below). The patch contains no game data; the .bpz holds only byte"
    echo "differences and is useless without your own original blitz2k.chd."; echo
    echo "----- DATA_NOTICE -----"; cat "$REPO/DATA_NOTICE.md"; echo
    echo "----- Apache License 2.0 -----"; cat "$REPO/LICENSE"; echo
    echo "== chdman (bundled, unmodified, from MAME 0.289)"
    cat "$VENDOR/README.md"; echo; echo "-----"; cat "$VENDOR/COPYING"; echo; echo "----- GNU GPL v2 -----"; cat "$VENDOR/GPL-2.0"
    true
  } > "$1/LICENSES.txt"; }
crlf() { sed -e 's/\r$//' -e 's/$/\r/' "$1" > "$1.tmp" && mv "$1.tmp" "$1"; }

# ---- Windows
D="$STAGE/blitz2027_kit_windows"; mkdir -p "$D"
cp "$BIN/blitzpatch_windows_amd64.exe" "$D/blitzpatch.exe"; cp "$VENDOR/chdman.exe" "$D"; cp "$BPZ" "$D/blitz2027.bpz"
cp "$T/PATCH.bat" "$D/PATCH.bat"; crlf "$D/PATCH.bat"
readme "$D" Windows "   Put your original blitz2k.chd in this patch folder, then double-click PATCH.bat.\n   (Or drag the CHD onto PATCH.bat from anywhere.) If SmartScreen warns about blitzpatch.exe/chdman.exe: More info > Run anyway." ""
licenses "$D" windows; for f in README.txt LICENSES.txt; do crlf "$D/$f"; done
# ---- macOS
D="$STAGE/blitz2027_kit_macos"; mkdir -p "$D"
cp "$BIN/blitzpatch_darwin_universal" "$D/blitzpatch"; cp "$VENDOR/chdman_macos" "$D/chdman"; cp "$BPZ" "$D/blitz2027.bpz"
cp "$T/PATCH.command" "$D/PATCH.command"
readme "$D" macOS "   Double-click PATCH.command (first time only: right-click > Open, then Open; macOS blocks unsigned downloads).\n   If it still refuses, run once in Terminal:  xattr -dr com.apple.quarantine <the kit folder>\n   Put your original blitz2k.chd in the kit folder first (or type ./PATCH.command in Terminal and drag the CHD after it)." \
  "The bundled blitzpatch and chdman are universal (Apple Silicon + Intel, macOS 12+). Both are ad-hoc signed, not notarized, hence the Gatekeeper step above."
licenses "$D" macos; chmod +x "$D/PATCH.command" "$D/blitzpatch" "$D/chdman"
# ---- Linux (one zip, x86_64 + arm64; patch.sh picks by uname -m)
D="$STAGE/blitz2027_kit_linux"; mkdir -p "$D"
cp "$BIN/blitzpatch_linux_amd64" "$D/blitzpatch"; cp "$VENDOR/chdman_linux_x86_64" "$D/chdman"; cp "$BPZ" "$D/blitz2027.bpz"
cp "$BIN/blitzpatch_linux_arm64" "$D/blitzpatch_arm64"
if [ -f "$VENDOR/chdman_linux_arm64" ]; then cp "$VENDOR/chdman_linux_arm64" "$D/chdman_arm64"
else echo "WARNING: $VENDOR/chdman_linux_arm64 missing; Linux kit ships without chdman_arm64 (arm64 users need CHDMAN=)" >&2; fi
cp "$T/patch.sh" "$D/patch.sh"
readme "$D" Linux "" ""
licenses "$D" linux; chmod +x "$D/patch.sh" "$D"/blitzpatch* "$D"/chdman*

for p in windows macos linux; do
  rm -f "$OUTDIR/blitz2027_kit_$p.zip"; ( cd "$STAGE" && zip -qr -X "$OUTDIR/blitz2027_kit_$p.zip" "blitz2027_kit_$p" )
done
( cd "$OUTDIR" && ls -l blitz2027_kit_*.zip && shasum -a 256 blitz2027_kit_*.zip )
