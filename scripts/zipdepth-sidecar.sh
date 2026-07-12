#!/usr/bin/env bash
# Launch ZipDepth HTTP sidecar (aito-mac) for gy live depth.
# Protocol: https://zipdepth.github.io · aito-mac/zipdepth-sidecar
set -euo pipefail

PORT="${ZIPDEPTH_PORT:-8766}"
SIDE="${ZIPDEPTH_SIDE:-$HOME/dev/aito-mac/zipdepth-sidecar/booth_zipdepth.py}"

if [[ ! -f "$SIDE" ]]; then
  # try relative to this repo's sibling layout
  ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
  for cand in \
    "$HOME/dev/aito-mac/zipdepth-sidecar/booth_zipdepth.py" \
    "$ROOT/aito-mac/zipdepth-sidecar/booth_zipdepth.py" \
    "$(cd "$(dirname "$0")/.." && pwd)/../aito-mac/zipdepth-sidecar/booth_zipdepth.py"
  do
    if [[ -f "$cand" ]]; then
      SIDE="$cand"
      break
    fi
  done
fi

if [[ ! -f "$SIDE" ]]; then
  echo "zipdepth sidecar script not found."
  echo "Expected: ~/dev/aito-mac/zipdepth-sidecar/booth_zipdepth.py"
  echo "Clone/copy aito-mac or set ZIPDEPTH_SIDE=/path/to/booth_zipdepth.py"
  exit 1
fi

echo "→ ZipDepth sidecar  $SIDE"
echo "  port $PORT  (ZIPDEPTH_URL=http://127.0.0.1:$PORT)"
echo "  optional: ZIPDEPTH_ROOT ZIPDEPTH_CKPT ZIPDEPTH_ONNX for real model"
echo
# health check if already up
if curl -sf "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
  echo "already running:"
  curl -s "http://127.0.0.1:${PORT}/health"
  echo
  exit 0
fi

exec python3 "$SIDE" --port "$PORT"
