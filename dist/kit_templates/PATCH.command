#!/bin/bash
xattr -dr com.apple.quarantine "$(dirname "$0")" 2>/dev/null
cd "$(dirname "$0")" && ./blitzpatch run "$@"
rc=$?
echo; read -r -p "Press Enter to close..." _
exit $rc
