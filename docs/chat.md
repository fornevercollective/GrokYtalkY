# Space / Creator Studio–style chat in the hybrid pipeline

X **Spaces** / Creator Studio chat is a high-fanout, low-bitrate text plane: reactions, captions, host pin, moderation — **not** media. That maps cleanly onto GrokYtalkY’s hybrid stack.

## How it maps

| Space / Studio concept | Hybrid home | Why |
|------------------------|-------------|-----|
| Public live chat (1k+) | **Cloudflare Workers + Durable Objects** | Edge WS fanout, persistence, global scale, DDoS/TLS |
| Host / co-host tools | DO room state (roles, mute, slow-mode) | Strong consistency per room at edge |
| Private jam / DOJO chat | **`gy serve` hub** and/or **`gy-sfu` data channel** | 16–32 peers, low latency, terminal aesthetic |
| Glyph/hex “chat as pixels” | `glyph` / `hex` lanes | Optional LED ticker / hexcast captions |
| Video / voice of Space | CF Calls / SFU media (separate) | Chat never rides media SFU hot path |
| Processing | — | JAX/FFmpeg **untouched** |

```
  Creator / host          Interactive DOJO              Broadcast viewers
       │                        │                              │
       │                   gy hub :9876                    CF Workers + DO
       │                   chat + PTT + burst              Space-style chat
       │                        │                              │
       └──────────┬─────────────┴──────────────┬───────────────┘
                  │                            │
                  ▼                            ▼
           gy · Glyph · terminal          web player UI
           16–32 native                    1k+ edge fanout
                  │
                  └── optional bridge: hub chat → CF room (one-way or sync)
```

## Flows

### A) Public Creator Studio / Space chat (CF)

1. Viewer opens page → Worker upgrades WS to Durable Object room `space:{id}`.
2. DO holds: recent messages ring buffer, roster, host roles, slow-mode.
3. Client sends `{type:"chat", text, from, t}` (same shape as hub).
4. DO persists (optional R2/D1 later) + broadcasts to all sockets in the DO.
5. Media (if any) is **Cloudflare Calls / HLS** — chat is independent.

**Scale:** millions of connection-minutes; per-room fanout limited by DO best practices (shard by room; one DO ≈ one Space).

### B) Private DOJO chat (native)

1. Terminal `gy` already sends hub `{type:"chat", text, from, t}` → mesh relay.
2. SFU peers use the same JSON on WS (`type: chat`) or future DataChannel `chat`.
3. 16–32 peers: zero CF dependency; hex/glyph aesthetic intact.

### C) Hybrid showcase (both)

| Audience | Chat path | Media path |
|----------|-----------|------------|
| On stage (DOJO) | hub / SFU | hub burst + SFU lanes |
| In the stands (web) | CF DO | CF media downsampled |
| Bridge | worker or sidecar mirrors **host** chat into both | optional |

Bridge rule of thumb: **DOJO → CF** for public captions; CF → DOJO only for moderated “stage questions” (rate-limited).

## Shared message shape

Compatible with existing hub (`client.SendChat` / `hub.go`):

```json
{
  "type": "chat",
  "text": "hello dojo",
  "from": "qbit",
  "id": "peer-or-session",
  "t": 1710000000000,
  "room": "space:launch",
  "role": "host",
  "meta": { "reply_to": null, "pin": false }
}
```

Optional Space-like extensions (CF DO only until hub opts in):

| Field | Meaning |
|-------|---------|
| `role` | `host` · `speaker` · `listener` |
| `meta.pin` | host-pinned |
| `meta.reply_to` | thread id |
| `meta.reaction` | emoji reaction event (`type: reaction`) |

## Concurrency

| Plane | Target | Transport |
|-------|--------|-----------|
| DOJO interactive chat | **16–32** | `gy serve` WS / SFU |
| Public Space chat | **1k+** | CF Worker + Durable Object |
| Reactions | high rate, tiny payload | same as chat plane |

## Scaffolds

| Path | Role |
|------|------|
| [`chat/`](../chat/README.md) | Protocol + CF Worker/DO + full-flow runbook |
| `gy chat-bridge` | Thin DOJO → Space caption bridge (`chat_bridge.go`) |
| [`site/chat.html`](../site/chat.html) | Dual-path demo panel (Public Space / DOJO) |
| [`sfu/`](../sfu/README.md) | DOJO WS `chat` fan-out (lane-adjacent) |
| `hub.go` | Existing mesh `type: chat` relay |
| [`streams-capacity.md`](streams-capacity.md) | Hybrid media + chat tables |

### Full flow (one glance)

```bash
gy serve
cd chat/worker && npx wrangler dev
gy chat-bridge --hosts yournick
# open site/chat.html → Public Space
# gy --nick yournick → send chat (↗ appears on Space panel)
```

## Non-goals

- Do **not** put public chat through the media SFU.
- Do **not** push chat through FFmpeg/JAX.
- Do **not** force 1k CF chatters onto the terminal hub.
