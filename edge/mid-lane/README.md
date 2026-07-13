# gy-mid-lane — reference edge worker (public audience)

Completes the **v1.45 mid-lane hook** for **public / Colossus-shaped viewers**.

| Plane | Where | Scale |
|-------|--------|-------|
| Interactive DOJO | `gy serve` rooms · `gy-sfu` | **16–32** (hub) / **~50–200** (SFU) |
| **Public mid-lane** | **this worker** | **1k+** edge viewers |
| Full HD web | CF Calls / HLS (you attach later) | broadcast |

**Never** put 1080p on the hub. This worker only fans **program bus** + **hexlum/gyst** mid-lane JSON.

## Architecture

```
 conductor TUI ──type:program──► gy hub (room=dojo)
 stream-pub hexlum ─────────────► gy hub
                                      │
                                      │  gy mid-lane --room dojo
                                      ▼
                              POST /mid (token)
                                      │
                                      ▼
                         CF Durable Object MidLaneRoom
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
              WS viewers         GET /state         (optional CF Calls)
              glyph/hex UI       last PGM snapshot
```

## Local run

```bash
# terminal A — DOJO
cd /path/to/GrokYtalkY
gy serve
GY_ROOM=dojo gy          # conductor /take /caption

# terminal B — edge worker (pick a free port; wrangler default 8787 may clash with chat)
cd edge/mid-lane
npm install
npx wrangler dev --port 8788

# terminal C — bridge (dry-run first)
gy mid-lane --room dojo --edge http://127.0.0.1:8788/mid --dry-run
gy mid-lane --room dojo --edge http://127.0.0.1:8788/mid
```

Optional token:

```bash
# wrangler.toml [vars] EDGE_TOKEN = "secret"
# or: npx wrangler secret put EDGE_TOKEN
export GY_EDGE_TOKEN=secret
gy mid-lane --room dojo --edge http://127.0.0.1:8788/mid --token secret
```

## Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | liveness |
| `POST` | `/mid` | ingest from `gy mid-lane` |
| `GET` | `/state?room=dojo` | last program + hexlum + viewer count |
| `WS` | `/ws?room=dojo&nick=viewer` | public receive-only stream |

## Envelope (from gy mid-lane)

```json
{
  "type": "mid-lane",
  "room": "dojo",
  "lane": "program",
  "seq": 12,
  "caption": "TAKE NEXT",
  "mark": "cgf:…",
  "mode": "live",
  "program": { "type": "program", "bus": { } },
  "via": "gy-mid-lane"
}
```

Hexlum lane: `"lane": "hex"` + `gyst` payload (already 13²/25² scale).

## Deploy

```bash
cd edge/mid-lane
npx wrangler deploy
# set EDGE_TOKEN secret in production
gy mid-lane --room dojo --edge https://gy-mid-lane.<you>.workers.dev/mid --token …
```

## Demo viewer

Open [`../../site/mid-lane.html`](../../site/mid-lane.html) (or Pages) → set worker origin + room → Connect.

## What this is not

- Not DOJO mesh authority (conductor stays on `gy`)
- Not Space chat (see `chat/worker`)
- Not SFU media tracks (see `sfu/` — jam scale next rung)
- Not NMOS / ST 2110 (venue plant path)

## Sequencing

1. **This worker** — public audience path (done)  
2. **SFU hardening** — jam 50–200 (TURN, metrics, multi-room)  
3. Facility PTP/NMOS when a real plant is attached  
