#!/usr/bin/env bash
# Install GrokYtalkY into ~/.local/bin
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
mkdir -p bin "$HOME/.local/bin"
go build -o bin/grokytalky .
install -m 755 bin/grokytalky "$HOME/.local/bin/grokytalky"
echo "Installed: $HOME/.local/bin/grokytalky"
echo "Ensure ~/.local/bin is on PATH, then: grokytalky"
