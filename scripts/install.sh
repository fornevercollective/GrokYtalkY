#!/usr/bin/env bash
# Install GrokYtalkY as terminal-wide commands: gy  (and grokytalky)
#
#   ./scripts/install.sh
#   curl -fsSL … | bash   # if published later
#
# Dest: ~/.local/bin  (override with PREFIX=…)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="${PREFIX}/bin"
NAME_PRIMARY="${NAME_PRIMARY:-gy}"
NAME_FULL="${NAME_FULL:-grokytalky}"

cd "$ROOT"
mkdir -p bin "$BIN_DIR"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 1.9.0)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)}"
DATE="${DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
LDFLAGS="-s -w -X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.Date=${DATE}"

echo "→ building ${VERSION} (${COMMIT})…"
go build -ldflags "${LDFLAGS}" -o "bin/${NAME_FULL}" .

echo "→ install ${BIN_DIR}/${NAME_FULL}"
install -m 755 "bin/${NAME_FULL}" "${BIN_DIR}/${NAME_FULL}"

# short command (like grok / agent)
echo "→ link  ${BIN_DIR}/${NAME_PRIMARY} → ${NAME_FULL}"
ln -sfn "${NAME_FULL}" "${BIN_DIR}/${NAME_PRIMARY}"

# optional burst shortcut
ln -sfn "${NAME_FULL}" "${BIN_DIR}/gy-burst" 2>/dev/null || true

echo
echo "Installed:"
echo "  ${BIN_DIR}/${NAME_PRIMARY}       # short (recommended)"
echo "  ${BIN_DIR}/${NAME_FULL}"
echo
if ! command -v "${NAME_PRIMARY}" >/dev/null 2>&1; then
  echo "Add to PATH (zsh):"
  echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
  echo
fi
echo "Try:"
echo "  gy              # companion dock"
echo "  gy burst        # Siri-sized video walkie"
echo "  gy version      # build info"
echo "  gy update       # check / install latest"
echo "  gy serve        # mesh hub"
echo "  gy --help"
"${BIN_DIR}/${NAME_PRIMARY}" --version 2>/dev/null || true
