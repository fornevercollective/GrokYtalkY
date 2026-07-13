.PHONY: build install install-system uninstall brew-local test help version launch sfu sfu-media

PREFIX ?= $(HOME)/.local
BIN_DIR := $(PREFIX)/bin

# Version metadata for ldflags (override: make VERSION=1.9.1 install)
# Prefer explicit VERSION=1.9.1; else git tag; else default.
VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo 1.9.1)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/grokytalky .

install: build
	@mkdir -p $(BIN_DIR)
	install -m 755 bin/grokytalky $(BIN_DIR)/grokytalky
	ln -sfn grokytalky $(BIN_DIR)/gy
	@echo "→ $(BIN_DIR)/gy  ($(VERSION))"
	@echo "→ $(BIN_DIR)/grokytalky"
	@command -v gy >/dev/null || echo "note: add $(BIN_DIR) to PATH"
	@$(BIN_DIR)/gy --version

install-system: build
	@bash scripts/install-system.sh

launch:
	@bash scripts/launch-terminal.sh

uninstall:
	rm -f $(BIN_DIR)/gy $(BIN_DIR)/grokytalky $(BIN_DIR)/gy-burst
	rm -f /usr/local/bin/gy /usr/local/bin/grokytalky 2>/dev/null || true

# Build from this checkout without a remote release tarball
brew-local:
	brew install --build-from-source --formula ./Formula/grokytalky.rb

test:
	go test ./...

# DOJO SFU sidecar (Rust + Tokio signaling; optional webrtc-rs via sfu-media)
sfu:
	cd sfu && cargo build --release
	@echo "→ sfu/target/release/gy-sfu  (signaling)"
	@echo "  run: ./sfu/target/release/gy-sfu --bind 127.0.0.1:9880"

sfu-media:
	cd sfu && cargo build --release --features media
	@echo "→ sfu/target/release/gy-sfu  (+webrtc-rs media)"

# Two-peer glyph+chat fan-out against media SFU (spawns binary)
sfu-e2e:
	cd sfu && cargo test --features media --test e2e_media -- --nocapture

version:
	@echo "VERSION=$(VERSION)"
	@echo "COMMIT=$(COMMIT)"
	@echo "DATE=$(DATE)"
	@go run -ldflags "$(LDFLAGS)" . version 2>/dev/null || true

help:
	@echo "make build | install | install-system | launch | uninstall | test | sfu"
	@echo "  install-system → /usr/local/bin/gy + yt-dlp/ffmpeg check"
	@echo "  launch         → new Terminal window running gy"
	@echo "  sfu            → Rust DOJO SFU sidecar (signaling)"
	@echo "  sfu-media      → SFU + webrtc-rs (heavier)"
	@echo "  gy watch <url> → auto yt-dlp resolve"
	@echo "CLI also: gy update|upgrade · gy install · gy uninstall ·"
	@echo "          gy clean install · gy install dependencies [--yes]"
