#!/usr/bin/env bash
# Launch gy hub + Live News + glyph-cast TV URL for smart-TV dev.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export PATH="$ROOT/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"

PORT="${GY_PORT:-9876}"
BIND="${GY_BIND:-0.0.0.0}"
LAN_IP="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo 127.0.0.1)"

echo "== GrokYtalkY Live News → smart TV cast =="
echo "  LAN   $LAN_IP"
echo "  hub   http://${LAN_IP}:${PORT}/"

# install latest binary to user path
if [[ -x "$ROOT/bin/gy" ]]; then
  mkdir -p "$HOME/.local/bin"
  cp -f "$ROOT/bin/gy" "$HOME/.local/bin/grokytalky"
  ln -sfn "$HOME/.local/bin/grokytalky" "$HOME/.local/bin/gy"
  echo "  gy    $(gy version 2>/dev/null | head -1 || echo installed)"
fi

# start blank for YouTube live resolve + HLS proxy (CORS)
if ! curl -sf -o /dev/null --max-time 1 "http://127.0.0.1:5173/"; then
  if [[ -x "${HOME}/dev/blank/start.sh" ]]; then
    echo "  starting blank :5173 (yt-dlp resolve for YouTube live)…"
    (cd "${HOME}/dev/blank" && BLANK_OPEN_BROWSER=0 BLANK_QUIET=1 nohup ./start.sh > /tmp/blank-livenews.log 2>&1 &)
    sleep 1
  else
    echo "  blank missing — Go live uses hub yt-dlp only (may hit CORS on video sample)"
  fi
else
  echo "  blank already up"
fi

# start hub if down
if ! curl -sf -o /dev/null --max-time 1 "http://127.0.0.1:${PORT}/api/lan"; then
  echo "  starting hub on ${BIND}:${PORT}…"
  nohup gy serve --bind "$BIND" --port "$PORT" > /tmp/gy-serve-livenews.log 2>&1 &
  echo $! > /tmp/gy-serve-livenews.pid
  sleep 0.8
else
  echo "  hub already up"
fi

NEWS_URL="http://${LAN_IP}:${PORT}/livenews.html"
TV_URL="http://${LAN_IP}:${PORT}/glyph-cast.html?source=livenews&cast=1&tv=1&fs=1&hub=ws://${LAN_IP}:${PORT}/&room=news&layout=grid&n=25"

echo ""
echo "  Live News (control)  $NEWS_URL"
echo "  Smart TV player      $TV_URL"
echo ""
echo "  1) Open Live News on this Mac → Connect hub → pin mosaic → Cast TV"
echo "  2) On smart TV browser (same Wi‑Fi) open the TV player URL"
echo "  3) Or use Cast TV Presentation / second display from Chrome"
echo ""

# open control surface + cast player on this machine
if [[ "${GY_NO_OPEN:-}" != "1" ]]; then
  open "$NEWS_URL" 2>/dev/null || true
  sleep 0.4
  open "$TV_URL" 2>/dev/null || true
fi

# copy TV URL for pasting into TV
if command -v pbcopy >/dev/null 2>&1; then
  printf '%s' "$TV_URL" | pbcopy
  echo "  TV URL copied to clipboard"
fi

echo "  log    /tmp/gy-serve-livenews.log"
echo "done."
