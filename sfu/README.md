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

## Status

- [x] Room registry · WS signaling · lane tags · health API  
- [ ] `webrtc` feature: PeerConnection, track forward, DataChannel  
- [ ] TURN config · auth tokens · hub glyph bridge  
- [ ] Metrics (peers, bitrate, room count)

## License

Apache-2.0 — same as GrokYtalkY.
