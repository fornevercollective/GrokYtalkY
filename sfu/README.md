# gy-sfu — DOJO WebRTC SFU sidecar

Minimal **Rust + Tokio** SFU scaffold for [GrokYtalkY](https://github.com/fornevercollective/GrokYtalkY).

| Role | Owner |
|------|--------|
| Interactive jams · Glyph · hex | This SFU + `gy serve` hub |
| Public 1k+ web viewers | **Cloudflare** Calls / media (not this binary) |
| Process / depth / style | FFmpeg · JAX — **off** the hot path |

Architecture: [`docs/streams-capacity.md`](../docs/streams-capacity.md)

## Lanes

Publish only what consumers need:

| Lane | Use |
|------|-----|
| `glyph` | 13×13 / 25×25 LED grid (DataChannel or tiny track) |
| `hex` | Low-res hex / packet stream |
| `chat` | Space/DOJO text (same JSON as `gy` hub) |
| `mid` | Compact web tile (~320p) |
| `full` | Optional HD — web/CF only, never force into terminal |

Public 1k+ **Space chat** lives on Cloudflare (Workers + DO) — see [`chat/`](../chat/README.md).

## Run (signaling scaffold)

```bash
cd sfu
cargo run -p gy-sfu -- --bind 0.0.0.0:9880

# health
curl -s http://127.0.0.1:9880/health

# join room via WebSocket (see protocol below)
```

Full media (`webrtc-rs`) when ready:

```bash
cargo run -p gy-sfu --features media -- --bind 0.0.0.0:9880
```

## HTTP / WS

| Endpoint | Purpose |
|----------|---------|
| `GET /health` | liveness |
| `GET /rooms` | list rooms + peer counts |
| `GET /ws?room=dojo&nick=qbit` | signaling WebSocket |

### Signaling messages (JSON)

Client → SFU:

```json
{"type":"join","room":"dojo","nick":"qbit","lanes":["glyph","hex"]}
{"type":"offer","sdp":"..."}
{"type":"answer","sdp":"..."}
{"type":"ice","candidate":{...}}
{"type":"glyph","n":25,"data":[0,12,40]}
{"type":"chat","text":"hello dojo"}
{"type":"leave"}
```

SFU → client:

```json
{"type":"welcome","peer_id":"...","room":"dojo"}
{"type":"peer_joined","peer_id":"...","nick":"alice","lanes":["glyph"]}
{"type":"peer_left","peer_id":"..."}
{"type":"offer","from":"...","sdp":"..."}
{"type":"answer","from":"...","sdp":"..."}
{"type":"ice","from":"...","candidate":{...}}
{"type":"glyph","from":"...","n":25,"data":[...]}
{"type":"error","message":"..."}
```

`glyph` / `hex` frames can ride the signaling socket **or** a WebRTC DataChannel once `media` is enabled — same JSON body.

## Bridge to GrokYtalkY hub

Optional later: SFU subscribes to `ws://hub:9876` and re-publishes `vburst-frame.glyph` onto the `glyph` lane for WebRTC peers. Terminal clients keep using the existing hub; SFU is for browser/DOJO WebRTC rooms.

```
gy serve                 # :9876 hexcast mesh
cargo run -p gy-sfu      # :9880 SFU signaling
# Cloudflare: point public viewers at CF; DOJO at gy-sfu
```

## Concurrency targets

| Mode | Target |
|------|--------|
| DOJO interactive | **16–32** peers (jam) · up to **~50–200** / node |
| Broadcast | **1k+** via Cloudflare, downsampled `mid`/`glyph` |

## Media mode (`--features media`)

```bash
cargo run -p gy-sfu --features media -- --bind 0.0.0.0:9880
# or: make sfu-media
```

| Feature | Behavior |
|---------|----------|
| PeerConnection | Client **offer** (no `to`) → SFU **answer** |
| ICE | Client candidates (no `to`) → SFU PC; SFU ICE → client |
| Track fan-out | `OnTrack` → `TrackLocalStaticRTP` → other peers in room |
| DataChannel | Labels `glyph` / `hex` / `chat` → room fan-out (+ WS mirror) |
| STUN | `stun:stun.l.google.com:19302` |
| TURN | `GY_SFU_TURN_URLS=turn:host:3478,user,cred;…` |

Signaling-only still works without the feature (WS relay of SDP for pure mesh tests).

### E2E validate (A/V + glyph)

```bash
# Automated (two webrtc-rs peers, glyph+chat fan-out)
make sfu-e2e

# Browser (two tabs)
make sfu-media
./sfu/target/release/gy-sfu --bind 0.0.0.0:9880
open site/dojo.html   # Join · allow cam · Send glyph pulse · chat
```

### Client sketch (media)

```js
// 1) WS join → welcome.media === true
// 2) pc = new RTCPeerConnection(); pc.ondatachannel = …  // SFU outbound DCs
// 3) optional: pc.createDataChannel("glyph") // also fine
// 4) addTrack(cam); offer → ws { type:"offer", sdp }  // no `to`
// 5) answer/ice → setRemote / addIceCandidate
// 6) SFU renegotiate offer → createAnswer
```

## Status

- [x] Room registry · WS signaling · lane tags · health API  
- [x] `media` feature: PeerConnection, track fan-out, DataChannel glyph/hex/chat  
- [x] **Outbound SFU DataChannels** (`glyph` · `hex` · `chat`) + client DCs  
- [x] STUN + optional TURN env  
- [x] E2E demo: [`site/dojo.html`](../site/dojo.html)  
- [ ] Auth tokens · hub glyph bridge · metrics

## License

Apache-2.0 — same as GrokYtalkY.
