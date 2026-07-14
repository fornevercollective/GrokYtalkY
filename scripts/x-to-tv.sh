#!/usr/bin/env bash
# One-tap: clipboard / arg X or t.co link → Play Queue (resolve high-res → TV).
# Usage:
#   ./scripts/x-to-tv.sh
#   ./scripts/x-to-tv.sh 'https://x.com/.../status/...'
#   ./scripts/x-to-tv.sh --watch   # also gy watch for terminal scrub
set -euo pipefail
URL="${1:-}"
WATCH=0
if [[ "${URL}" == "--watch" ]]; then
  WATCH=1
  URL="${2:-}"
fi
if [[ -z "$URL" ]]; then
  if command -v pbpaste >/dev/null 2>&1; then
    URL="$(pbpaste | head -1 | tr -d '\r\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  fi
fi
if [[ -z "$URL" ]]; then
  echo "usage: $0 [url|--watch url]   (or copy URL to clipboard first)" >&2
  exit 1
fi
# Prefer live hub origin
HUB="${GY_HUB_HTTP:-http://127.0.0.1:9876}"
# Encode for #add=
ENC="$(python3 -c 'import sys,urllib.parse; print(urllib.parse.quote(sys.argv[1], safe=""))' "$URL")"
OPEN_URL="${HUB}/queue.html#add=${ENC}"
echo "queue ← $URL"
echo "open  $OPEN_URL"
if command -v open >/dev/null 2>&1; then
  open "$OPEN_URL"
elif command -v xdg-open >/dev/null 2>&1; then
  xdg-open "$OPEN_URL"
else
  echo "$OPEN_URL"
fi
if [[ "$WATCH" == "1" ]] && command -v gy >/dev/null 2>&1; then
  echo "gy watch (scrub: j/l ±5s · J/L ±30s)"
  exec gy watch "$URL"
fi
