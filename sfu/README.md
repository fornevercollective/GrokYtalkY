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

## Bridge to GrokYtalkY hub (token + glyph DC)

`gy sfu-bridge` links the DOJO hub to a gy-sfu room over **signaling + media DataChannels** (`glyph` · `hex` · `chat`) with shared **token** auth.

| Direction | Message | Lane |
|-----------|---------|------|
| hub→sfu | `vburst-frame.glyph` | `type:glyph` (WS + DC fan-out) |
| hub→sfu | `gyst` `kind=hexlum` | `type:glyph` + `type:hex` |
| hub→sfu | forge-mark meta | `type:chat` + `meta.mark` |
| sfu→hub | `type:glyph` | hub `vburst-frame` (bidi) |
| sfu→hub | `type:hex` | hub `gyst` hexlum |
| sfu→hub | `type:chat` | hub `chat` |

```bash
export GY_SFU_TOKEN=$(gy sfu-token)   # or gy sfu-token --export
gy serve
make sfu-media && ./sfu/target/release/gy-sfu --token "$GY_SFU_TOKEN"
gy sfu-bridge --token "$GY_SFU_TOKEN" --room dojo
# browser: site/dojo.html → token field = $GY_SFU_TOKEN
# publisher: /forge examples/dojo.pcap  or  gy burst
gy doctor sfu
```

Join auth: `?token=` on WS URL **and/or** `{"type":"join","token":"…"}`.  
Welcome includes `auth: true` when the SFU requires a token.  
Glyph grids on the wire are **JSON number arrays** (not base64) so DC + bridge stay aligned.

Terminal clients stay on the hub; browser/WebRTC peers use glyph|hex DCs. Lattice watermark bytes pass through unchanged.

## Concurrency targets

| Mode | Target |
|------|--------|
| DOJO interactive | **16–32** peers (jam) · up to **~50–200** / node |
| Broadcast | **1k+** via Cloudflare mid-lane (`edge/mid-lane` + `gy mid-lane`) |

## Jam-scale hardening (v0.2 / gy 1.47)

| Control | Flag / env | Default |
|---------|------------|---------|
| Peers per room | `--max-peers-per-room` · `GY_SFU_MAX_PEERS` | 64 |
| Peers per node | `--max-peers-node` · `GY_SFU_MAX_PEERS_NODE` | 256 |
| Outbox cap (backpressure) | `--outbox-capacity` · `GY_SFU_OUTBOX` | 64 |
| Glyph max bytes | `--max-glyph-bytes` | 49² |
| Glyph min interval | `--glyph-min-interval-ms` | 33 (~30 fps) |
| STUN | `--stun-urls` · `GY_SFU_STUN_URLS` | Google STUN |
| TURN | `--turn-urls` · `GY_SFU_TURN_URLS` | unset |
| Auth | `--token` · `GY_SFU_TOKEN` | open |

```bash
# NAT jam room with TURN
export GY_SFU_TURN_URLS='turn:turn.example:3478,user,pass'
cargo run -p gy-sfu --features media -- \
  --bind 0.0.0.0:9880 --token secret --max-peers-per-room 32

curl -s http://127.0.0.1:9880/health   # counters + ice
curl -s http://127.0.0.1:9880/metrics  # rooms + drops
```

When peer outboxes fill, **glyph/hex frames drop** (counted in `outbox_drops`) instead of unbounded memory growth.

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
- [x] Auth token (`--token` / `GY_SFU_TOKEN` / `?token=`)  
- [x] Hub glyph/hex bridge: `gy sfu-bridge` (vburst + gyst hexlum lattice + forge-mark)  
- [ ] Metrics

## License

Apache-2.0 — same as GrokYtalkY.
