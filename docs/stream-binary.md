# Binary / hex / pcap stream codec

Load and save video/audio at the **packet** level — not only as mp4 containers.

## Formats

| Ext | Kind | Notes |
|-----|------|--------|
| **`.gyst`** | Binary GYST packets | Default; concat header+payload |
| **`.gyhex`** | Text hex | Editable; one packet per line |
| **`.pcap`** | PCAP LINKTYPE_USER0 (147) | Each record = one GYST blob (Wireshark) |

### GYST header (32 bytes, little-endian)

| Off | Field |
|-----|--------|
| 0–3 | magic `GYST` |
| 4 | version `1` |
| 5 | kind: 1 rgb24 · 2 pcm16 · 3 jpeg · 4 hexlum · 5 meta |
| 6–7 | flags |
| 8–11 | width (or sample rate for pcm) |
| 12–15 | height (or channels for pcm) |
| 16–19 | seq |
| 20–27 | timestamp ms |
| 28–31 | payload length |
| 32+ | payload |

**hexlum** matches overview `hexframe` luminance grids (`liveHexCodec`).

## CLI

```bash
# sample video → binary stream
gy encode clip.mp4 out.gyst
gy encode clip.mp4 out.gyhex
gy encode clip.mp4 out.pcap

# still image
gy encode photo.jpg frame.gyst

# inspect
gy decode out.gyst

# play at binary level (packet player, scrub with j/k/l)
gy watch out.gyst
gy watch out.gyhex
gy watch out.pcap
```

## In TUI

```text
/rec                 start recording displayed frames (+ pcm if present)
/rec stop
/export out.gyst     write recording (or single frame)
/export out.gyhex
/export out.pcap
/load stream.gyst    play binary stream
/hexdump             show current frame as one gyhex line
/watch stream.pcap   same as /load for codec files
```

Drop `.gyst` / `.gyhex` / `.pcap` onto Terminal like any media file.

## Pipeline

```
camera / mp4 / yt-dlp
        │
        ▼
   RGB24 / PCM16 frames  ──encode──►  .gyst | .gyhex | .pcap
        ▲                                    │
        └──────── decode /watch /load ───────┘
```

## Wireshark

Open `.pcap` → USER0 packets. Payload starts with ASCII `GYST`.
