#!/usr/bin/env bash
# powerhouse-up.sh — bring up gy hub (+ optional venue) for the stack
# Usage:
#   ./scripts/powerhouse-up.sh
#   POWERHOUSE_VENUE=1 ./scripts/powerhouse-up.sh
#   GY_HUB_PORT=9876 ./scripts/powerhouse-up.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${GY_HUB_PORT:-9876}"
GY="${GY_BIN:-}"
if [[ -z "$GY" ]]; then
  if command -v gy >/dev/null 2>&1; then
    GY="$(command -v gy)"
  elif [[ -x "$ROOT/bin/gy" ]]; then
    GY="$ROOT/bin/gy"
  elif [[ -x "$HOME/.local/bin/gy" ]]; then
    GY="$HOME/.local/bin/gy"
  else
    echo "gy not found — build: (cd $ROOT && go build -o bin/gy .)" >&2
    exit 1
  fi
fi

echo "powerhouse · gy=$GY · hub :$PORT"
# headless hub
if lsof -i ":$PORT" -sTCP:LISTEN -t >/dev/null 2>&1; then
  echo "powerhouse · hub already on :$PORT"
else
  "$GY" serve --port "$PORT" --bind 127.0.0.1 &
  echo $! > /tmp/gy-powerhouse-hub.pid
  sleep 0.4
  echo "powerhouse · hub pid $(cat /tmp/gy-powerhouse-hub.pid)"
fi

if [[ "${POWERHOUSE_VENUE:-}" == "1" ]]; then
  echo "powerhouse · venue st2110 + anc (log json)"
  "$GY" venue --hub "ws://127.0.0.1:${PORT}/" --sink log --json --quiet &
  echo $! > /tmp/gy-powerhouse-venue.pid
fi

echo "powerhouse · GY_HUB=ws://127.0.0.1:${PORT}/"
echo "powerhouse · open: gy · blank StageForge · overview · qbpm"
echo "powerhouse · docs: $ROOT/docs/powerhouse-stack.md"
