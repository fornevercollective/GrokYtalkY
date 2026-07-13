# gy stream-x — X.com streaming asset

Use **GrokYtalkY as a reusable encoder/publisher** for [X Media Studio](https://studio.x.com) / Periscope RTMP.  
Other creators can seat on your stage; you (or an allowed guest workflow) publish A/V to X.

## Why “asset”

| You run | Guests get |
|---------|------------|
| `gy serve` + `gy stream-x` | Stage seats, waveforms, chat, captions |
| RTMP key (when ready) | Shared egress to `ca.pscp.tv` without their own key plumbing |
| Host mute controls | Predictable stage audio |

## Quick start (operator)

```bash
gy stream-x init --space https://x.com/i/spaces/1AJEmmANrPeJL
# when Media Studio Sources → RTMP key is ready:
echo "$STREAM_KEY" > ~/.config/grokytalky/x-stream-key && chmod 600 ~/.config/grokytalky/x-stream-key
# or auto-pull:
export GY_X_STREAM_KEY=…          # or GY_X_STREAM_KEY_FILE / GY_X_STREAM_KEY_URL
gy stream-x key                   # env|file|json|clipboard|url
gy stream-x offer --public        # mesh type:space-asset
gy stream-x start --in clip.mp4
# mac cam+mic:
gy stream-x start --in "avfoundation:0:0"
```

Ingest (Canada defaults):

- **RTMPS** `rtmps://ca.pscp.tv:443/x/<key>`
- **RTMP** `rtmp://ca.pscp.tv:80/x/<key>`

## Key auto-pull order

1. `GY_X_STREAM_KEY`
2. `GY_X_STREAM_KEY_FILE` / `~/.config/grokytalky/x-stream-key`
3. `~/.config/grokytalky/x-rtmp.json` (`{"stream_key":"…"}`)
4. `GY_X_STREAM_KEY_URL` (optional vault; `Authorization: Bearer $GY_SPACE_TOKEN`)
5. Clipboard (`gy space key --pull` / burst **Auto-pull key**)

Hub (operator only):

```bash
export GY_SPACE_TOKEN=secret
gy serve
curl -s "http://127.0.0.1:9876/api/space"           # public roster · never leaks key
curl -s "http://127.0.0.1:9876/api/space/key?token=secret"
```

## Host mute + listeners

| Control | CLI / TUI | Mesh |
|---------|-----------|------|
| Mute seat | `gy space mute speaker:3` · `/space mute cohost:0` | `space-mute` |
| Mute all | `gy space mute all` · `/space mute all` | `space-mute-all` |
| Unmute | `gy space unmute all` | same |
| Listeners | `gy space listeners list\|add n\|rm n` | `space-listener-join/leave` + `listener_list` on roster |

Burst page: per-seat **mute** buttons, **Mute all / Unmute all / Self mute**, named **listener chips**.

## Guest path (other users)

1. Open `burst.html` (or `gy`) → **Connect** to the same hub.
2. **Seat me** as speaker/co-host (or listener).
3. Operator: **Offer asset** + `gy stream-x start --in …`.
4. Operator may `gy stream-x guest alice` to allowlist.

Mesh never carries the raw stream key — only `ready: true/false` on `space-asset`.

## Related

- [`docs/burst.md`](burst.md) — Spaces stage on burst
- [`site/burst.html`](../site/burst.html) — UI
- `gy space` · `gy space-rtmp` · `gy stream-x`
