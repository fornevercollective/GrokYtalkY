#!/usr/bin/env bash
# GrokYtalkY — first-time / from-scratch install (new user)
#
#   # from a git clone:
#   ./scripts/install.sh
#
#   # one-liner (Go required):
#   go install github.com/fornevercollective/grokytalky@latest
#   ln -sfn "$(go env GOPATH)/bin/grokytalky" ~/.local/bin/gy   # optional short name
#
# Dest: ~/.local/bin  (override with PREFIX=…)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="${PREFIX}/bin"
NAME_PRIMARY="${NAME_PRIMARY:-gy}"
NAME_FULL="${NAME_FULL:-grokytalky}"

step=0
total=6
bar() {
  step=$((step + 1))
  local filled=$(( step * 24 / total ))
  local empty=$(( 24 - filled ))
  local b
  b="$(printf '%0.s█' $(seq 1 "$filled" 2>/dev/null) || true)"
  # portable bar
  b=""
  local i
  for ((i=0; i<filled; i++)); do b+="█"; done
  for ((i=0; i<empty; i++)); do b+="░"; done
  echo "  [${step}/${total}] ${b}  $*"
}

echo
echo "  ┌─────────────────────────────────────────────┐"
echo "  │  GrokYtalkY install                         │"
echo "  └─────────────────────────────────────────────┘"
echo

bar "check tools"
need=0
if ! command -v go >/dev/null 2>&1; then
  echo "    ✗ go missing — https://go.dev/dl/  or: brew install go"
  need=1
fi
if ! command -v git >/dev/null 2>&1; then
  echo "    ! git missing (optional for version metadata)"
fi
if [[ "$need" -eq 1 ]]; then
  echo
  echo "  Install Go, then re-run this script."
  exit 1
fi
echo "    ✓ go $(go version 2>/dev/null | awk '{print $3}')"

bar "prepare ${BIN_DIR}"
cd "$ROOT"
mkdir -p bin "$BIN_DIR"

VERSION="${VERSION:-$(git describe --tags --exact-match 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo 1.54.0)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}"

bar "build ${VERSION} (${COMMIT})"
go build -ldflags "${LDFLAGS}" -o "bin/${NAME_FULL}" .

bar "install binaries"
install -m 755 "bin/${NAME_FULL}" "${BIN_DIR}/${NAME_FULL}"
ln -sfn "${NAME_FULL}" "${BIN_DIR}/${NAME_PRIMARY}"
ln -sfn "${NAME_FULL}" "${BIN_DIR}/gy-burst" 2>/dev/null || true

bar "optional deps (ffmpeg · yt-dlp)"
if command -v brew >/dev/null 2>&1; then
  for f in ffmpeg yt-dlp; do
    if command -v "$f" >/dev/null 2>&1; then
      echo "    ✓ $f"
    else
      echo "    → brew install $f  (optional; or: gy install deps -y)"
    fi
  done
else
  echo "    · install ffmpeg + yt-dlp for /watch and news wall"
fi

bar "verify"
echo
echo "  ✓  installed"
echo "  ───────────"
echo "  ${BIN_DIR}/${NAME_PRIMARY}"
echo "  ${BIN_DIR}/${NAME_FULL}"
echo
if ! command -v "${NAME_PRIMARY}" >/dev/null 2>&1; then
  echo "  Add to PATH (zsh):"
  echo "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
  echo
fi
echo "  Getting started"
echo "  ───────────────"
echo "  gy                 companion dock"
echo "  gy serve           mesh hub (phone cast on LAN)"
echo "  gy burst           dual Glyph walkie"
echo "  gy update          check GitHub + upgrade"
echo "  gy install deps -y go · ffmpeg · yt-dlp"
echo "  gy doctor          tool check"
echo
echo "  Docs  https://fornevercollective.github.io/GrokYtalkY/"
echo
"${BIN_DIR}/${NAME_PRIMARY}" --version 2>/dev/null || true
"${BIN_DIR}/${NAME_PRIMARY}" version 2>/dev/null | head -6 || true
