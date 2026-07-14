# BitChat dual-path · GrokYtalkY

Offline **Bluetooth mesh + Nostr** coordination bridged into the gy hub, so sphere / GrokGlyph / terminal stay one conversation surface when Wi‑Fi is flaky or gone.

## Sources

| Repo | Role |
|------|------|
| [permissionlesstech/bitchat](https://github.com/permissionlesstech/bitchat) | iOS/macOS native (BLE + Nostr) |
| [jackjackbits/bitchat-1](https://github.com/jackjackbits/bitchat-1) | Fork / same protocol family |
| [permissionlesstech/bitchat-android](https://github.com/permissionlesstech/bitchat-android) | Android port |
| [bitchat.free](https://bitchat.free) | Product site |

Public domain (Unlicense).

## What rides where

| Payload | Transport |
|---------|-----------|
| Multi-cam glyphs, vburst, sphere cast, HLS | **Wi‑Fi hub** (`gy serve`) |
| Chat text, presence, cast-start/stop control | **BitChat BLE / Nostr** via bridge |
| Same chat on LAN | Hub mesh `type:chat` |

Browser **cannot** open BLE mesh. Native BitChat (or a small adapter) **POSTs** into the hub.

## Architecture

```
BitChat app (BLE) ──adapter──► POST /api/bitchat/ingress
                                      │
                                      ▼
                               gy hub BitChatBus
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
              mesh chat         bitchat-presence   egress queue
              (sphere/GG)                          GET /api/bitchat/egress
                                                      │
                                                      ▼
                                              adapter → BLE/Nostr
```

## Hub API

| Method | Path | Purpose |
|--------|------|---------|
| GET | `/api/bitchat` | Status, peers, bridges |
| POST | `/api/bitchat/ingress` | Native → hub (JSON envelope) |
| GET | `/api/bitchat/egress` | Hub → native (drain queue) |
| POST | `/api/bitchat/send` | Inject chat (+ `dual` egress) |
| POST | `/api/bitchat/sim` | Dev: fake BLE peer |

### Ingress envelope

```json
{
  "type": "chat",
  "from": "alice",
  "text": "lights up",
  "transport": "ble",
  "channel": "mesh#bluetooth",
  "room": "global",
  "geohash": "dr5rsj7"
}
```

Types: `chat` · `presence` · `control` · `dm` · `system`  
Control `action` examples: `cast-start` · `cast-stop` · `sphere-cast`

## CLI

```bash
gy serve --bind 0.0.0.0 --port 9876
gy bitchat doctor
gy bitchat sim "hello from BLE" --from crew
gy bitchat send "stage ready" --from director --dual
gy bitchat bridge --http http://127.0.0.1:9876   # poll egress stub
gy doctor bitchat
```

Env: `GY_BITCHAT=0` disable · `GY_BITCHAT_CHANNEL` · `GY_HUB_HTTP` · `GY_ROOM`

## Mesh types

- `bitchat-chat` + ordinary `chat` with `meta.via=bitchat`
- `bitchat-presence`
- `bitchat-control` (GrokGlyph can start/stop cast)
- Wi‑Fi `chat` from site/terminal is **queued for egress** so a native bridge can rebroadcast over BLE

## Site

| File | Role |
|------|------|
| `site/bitchat-bridge.js` | Dual-path helper |
| `site/sphere.html` | Dual chat · Sim BLE · status line |
| `site/grokglyph.html` | Poll status · dual join chat · control cast |

Live Pages: [GrokGlyph](https://fornevercollective.github.io/GrokYtalkY/grokglyph.html) · [Sphere](https://fornevercollective.github.io/GrokYtalkY/sphere.html)

## Native adapter (minimal contract)

1. Join BitChat BLE mesh / geohash channel.
2. On each public chat: `POST /api/bitchat/ingress` with envelope.
3. Loop: `GET /api/bitchat/egress` → send those texts over BLE/Nostr.
4. Optional: connect WS `?role=bitchat-bridge&nick=bc-bridge` for presence count.

## Security notes

- Public mesh chat is the primary product surface.
- BitChat DMs use Noise/NIP-17; treat as unreviewed for high-sensitivity use.
- Do not put camera frames on BLE; keep media on hub.

## Related

- `docs/powerhouse-stack.md` · `docs/platform-integration.md`
- Hub media tools · multi-cam GrokGlyph cast · sphere walkie
