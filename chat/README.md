# chat — Space / Creator Studio–style text plane

Hybrid chat for GrokYtalkY:

| Audience | Backend | Scale |
|----------|---------|-------|
| Public Space / Studio | **Cloudflare Worker + Durable Object** (`worker/`) | 1k+ |
| Private DOJO | Existing **`gy serve`** hub + **`gy-sfu`** `chat` msgs | 16–32 |

Media (CF Calls / SFU) stays separate. JAX/FFmpeg untouched.

Full mapping: [`docs/chat.md`](../docs/chat.md)

## Shared protocol

Same shape as the Go hub (`MeshClient.SendChat`):

```json
{
  "type": "chat",
  "text": "hello",
  "from": "nick",
  "id": "optional-peer-id",
  "t": 1710000000000,
  "room": "dojo",
  "role": "listener"
}
```

See [`protocol.json`](protocol.json) for the JSON Schema.

## DOJO path (already live)

```bash
gy serve          # hub relays type:chat
gy                # terminal chat mode
```

SFU (private WebRTC rooms):

```bash
make sfu && ./sfu/target/release/gy-sfu --bind 127.0.0.1:9880
# WS message: {"type":"chat","text":"hi"}
```

## Cloudflare path (scaffold)

```bash
cd chat/worker
# npm i -g wrangler   # once
npm install
npx wrangler dev      # local DO + WS
# npx wrangler deploy # edge
```

Endpoints (when running):

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | liveness |
| `GET` | `/ws?room=space:demo&nick=viewer` | Space chat WebSocket |
| `GET` | `/history?room=space:demo` | recent messages (DO memory) |

## Bridge (optional next)

- **DOJO → CF:** small process reads hub WS, POSTs host lines into DO (public captions).
- **CF → DOJO:** moderated only (rate limit + host approve) so 1k chatters never flood the terminal.

Stub note in `worker/src/bridge.ts`.

## Layout

```
chat/
  README.md
  protocol.json          # shared message schema
  worker/                # CF Workers + Durable Object
    package.json
    wrangler.toml
    src/index.ts
    src/room-do.ts
    src/bridge.ts
```
