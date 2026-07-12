#!/usr/bin/env bash
# Machine-wide install of gy + stream tools (ffmpeg/yt-dlp).
# Also installs user copy to ~/.local/bin.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-$(git describe --tags --exact-match 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo 1.9.1)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}"

echo "→ build gy ${VERSION}"
mkdir -p bin
go build -ldflags "${LDFLAGS}" -o bin/grokytalky .

# user install
mkdir -p "$HOME/.local/bin"
install -m 755 bin/grokytalky "$HOME/.local/bin/grokytalky"
ln -sfn grokytalky "$HOME/.local/bin/gy"
echo "  ✓ ~/.local/bin/gy"

# machine-wide (prefer /usr/local/bin — already on PATH on this Mac)
SYS_BIN="/usr/local/bin"
if [[ -w "$SYS_BIN" ]] || [[ -w "$(dirname "$SYS_BIN")" ]]; then
  install -m 755 bin/grokytalky "$SYS_BIN/grokytalky"
  ln -sfn grokytalky "$SYS_BIN/gy"
  echo "  ✓ $SYS_BIN/gy"
elif command -v sudo >/dev/null 2>&1; then
  echo "  → sudo install to $SYS_BIN"
  sudo install -m 755 bin/grokytalky "$SYS_BIN/grokytalky"
  sudo ln -sfn grokytalky "$SYS_BIN/gy"
  echo "  ✓ $SYS_BIN/gy (sudo)"
else
  echo "  ! cannot write $SYS_BIN — using ~/.local/bin only"
fi

# PATH: ensure ~/.local/bin in zshrc
ZSHRC="${ZDOTDIR:-$HOME}/.zshrc"
if [[ -f "$ZSHRC" ]] && ! grep -q '\.local/bin' "$ZSHRC" 2>/dev/null; then
  echo '' >> "$ZSHRC"
  echo '# GrokYtalkY / user bins' >> "$ZSHRC"
  echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$ZSHRC"
  echo "  ✓ PATH line added to $ZSHRC"
fi

# stream deps
ensure() {
  local name="$1"
  if command -v "$name" >/dev/null 2>&1; then
    echo "  ✓ $name ($(command -v "$name"))"
    return 0
  fi
  if command -v brew >/dev/null 2>&1; then
    echo "  → brew install $name"
    brew install "$name" || true
  else
    echo "  ! missing $name — install manually"
  fi
}
echo "→ stream tools"
ensure ffmpeg
ensure yt-dlp
# ffplay ships with ffmpeg formula on brew usually
if ! command -v ffplay >/dev/null 2>&1; then
  ensure ffmpeg
fi

echo
echo "Installed:"
command -v gy || true
gy --version 2>/dev/null || "$HOME/.local/bin/gy" --version
gy doctor 2>/dev/null || "$HOME/.local/bin/gy" doctor || true
