#!/usr/bin/env bash
# Open a new terminal window with gy running.
set -euo pipefail

GY_BIN="${GY_BIN:-}"
if [[ -z "$GY_BIN" ]]; then
  if [[ -x /usr/local/bin/gy ]]; then
    GY_BIN=/usr/local/bin/gy
  elif command -v gy >/dev/null 2>&1; then
    GY_BIN="$(command -v gy)"
  elif [[ -x "$HOME/.local/bin/gy" ]]; then
    GY_BIN="$HOME/.local/bin/gy"
  else
    echo "gy not found — run scripts/install-system.sh first" >&2
    exit 1
  fi
fi

# remaining args passed to gy (e.g. burst, serve)
EXTRA=("$@")

echo "→ launching: $GY_BIN ${EXTRA[*]:-}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  if command -v gnome-terminal >/dev/null; then
    gnome-terminal -- "$GY_BIN" "${EXTRA[@]}"
  elif command -v xterm >/dev/null; then
    xterm -e "$GY_BIN" "${EXTRA[@]}"
  else
    exec "$GY_BIN" "${EXTRA[@]}"
  fi
  exit 0
fi

# Write a tiny launcher script so AppleScript quoting stays simple
LAUNCHER="$(mktemp "${TMPDIR:-/tmp}/gy-launch.XXXXXX")"
{
  echo '#!/bin/zsh'
  echo 'export PATH="/usr/local/bin:/opt/homebrew/bin:$HOME/.local/bin:$PATH"'
  echo 'clear'
  echo 'echo "◈ GrokYtalkY — gy"'
  echo 'echo "  /watch <yt-url|file|m3u8>  ·  tab modes  ·  q quit"'
  echo 'echo'
  printf 'exec %q' "$GY_BIN"
  for a in "${EXTRA[@]}"; do
    printf ' %q' "$a"
  done
  echo
} >"$LAUNCHER"
chmod +x "$LAUNCHER"

# Prefer Terminal.app (reliable on all Macs)
osascript \
  -e 'tell application "Terminal"' \
  -e 'activate' \
  -e "do script \"${LAUNCHER}\"" \
  -e 'end tell'

# cleanup launcher after a bit (window has already started)
(sleep 30 && rm -f "$LAUNCHER") &
echo "  ✓ Terminal window opened"
