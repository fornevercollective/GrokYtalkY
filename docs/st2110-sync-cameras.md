# ST 2110 · PTP · Sync clocks · Camera tether

Basis coverage for venue IP (GrokYtalkY `gy venue`) and production planning.

## Suite map

| Standard | Role | GrokYtalkY |
|----------|------|------------|
| **ST 2110-10** | System timing, SDP, RTP | Multi-essence SDP, `ts-refclk`, SyncClockReport |
| **ST 2059-1** | Align essence to PTP epoch | Documented; lock is facility GM/BC |
| **ST 2059-2** | **PTP profile required by 2110** | Signaled; not AES67 default profile |
| **ST 2110-20** | Uncompressed video | Tightened fmtp: sampling, depth, TCS, RANGE, PAR, exactframerate; uyvy/v210 RTP |
| **ST 2110-21** | Traffic shaping TPN/TPNL/TPW | `--tp 2110TPN\|TPNL\|TPW` (software signals; no HW shaper) |
| **ST 2110-22** | CBR compressed video | Lab H.264 only (not claiming 22) |
| **ST 2110-30** | PCM audio (AES67 constrained) | `--audio-rtp` / `--sink st2110-30` |
| **ST 2110-31** | AES3 transparent | Facility gateway |
| **ST 2110-40** | ANC / captions | Program bus → mark/tally/bus DID 0x5F; `--anc-rtp` · `OnANC` |

```bash
gy doctor st2110     # suite + PTP gaps
gy doctor sync       # synclock report (honors GY_PTP_*)
gy doctor nmos       # IS-04/05 scaffold + senders
gy doctor cameras    # manufacturer tether matrix
gy venue --sink st2110 --profile 2110-20 \
  --audio-rtp rtp://239.100.1.10:5006

# facility PTP lock (honest when GM attached via ptp4l/BC):
export GY_PTP_LOCKED=1 GY_PTP_DOMAIN=127 GY_PTP_OFFSET_NS=200 GY_PTP_IFACE=eth0
# optional registry post: GY_NMOS_REGISTRY=http://registry:8080 GY_NMOS_POST=1 gy doctor nmos
```

## PTP dependency (non-optional for production 2110)

1. **All essences share one PTP domain** (video 20, audio 30, ANC 40).
2. Profile is **SMPTE ST 2059-2**, not the AES67 media profile alone — even though 2110-30 *transports* like AES67.
3. **ST 2110-30** adds:
   - media clock ↔ RTP timestamp **offset = 0**
   - follower-only mode signaling
   - channel-order in SDP: `channel-order=SMPTE2110.(ST)` etc.
4. Software `gy venue` defaults to **free-run** + `ts-refclk:localmac` until a grandmaster is attached. That is honest; it is **not** production-compliant timing.
5. Hybrid plants: PTP → sync-pulse generators still feed SDI islands (blackburst / tri-level / word-clock).
6. **NMOS** (IS-04 discovery / IS-05 connection): `gy doctor nmos` emits a resource bundle (node, 2110-20/30/40 + mid-lane senders). Set `GY_NMOS_REGISTRY` + `GY_NMOS_POST=1` to POST; full controller is facility-side.

### Broadcast synclock stack

```
PTP Grandmaster (ST 2059-2)
        │
        ├─► ST 2110 fabric (20/30/40) ── media clocks aligned
        │
        └─► Sync generator ──► SDI genlock / audio WC (legacy islands)
```

| Clock | Legacy | 2110 fabric |
|-------|--------|-------------|
| Video | Blackburst / tri-level | PTP-derived (2059-1) |
| Audio | Word-clock / AES | PTP media clock @ 48 kHz |
| Timecode | LTC/VITC | PTP epoch + RTP |

## ST 2110-30 (audio) basis

- Transport: RTP linear PCM **L16** or **L24** @ 48 kHz (typical).
- Packet time Level **A** ≈ **1 ms** (narrow); B/C denser facility packs.
- SDP example fields gy writes:
  - `a=rtpmap:97 L24/48000/2`
  - `a=fmtp:97 channel-order=SMPTE2110.(ST)`
  - `a=ptime:1`
  - `a=ts-refclk:…` shared with video in multi-essence SDP

Silence is valid continuity for AES67 receivers when no mic is mapped yet.

## Camera manufacturers (tether → IP)

Major families that can tether into a DOJO / venue path. Full table: `gy doctor cameras`.

| Mfr | Path to 2110 | PTP | Typical gy ingest |
|-----|----------------|-----|-------------------|
| **Sony** Venice/FX | SDI/fiber → gateway | Via switcher | cam/SDI capture |
| **ARRI** Alexa live | SDI/fiber → truck 2110 | Via switcher | SDI → capture |
| **RED** V-RAPTOR/KOMODO | SDI → converter | n/a native | SDI/USB proxy |
| **Blackmagic** URSA/Studio | **Native 2110** + converters | **Native** | 2110 fabric / SDI |
| **Canon** C/EOS cinema | SDI/HDMI → ATEM/gateway | Via switcher | capture |
| **Panasonic** VariCam/EVA | SDI/fiber | Via switcher | capture |
| **Grass Valley / Ikegami / Hitachi** | Studio fiber/IP **native** | **Native** | Facility spine |
| **BirdDog / NDI cams** | NDI (2110 via convert) | n/a | NDI tools / sfu |
| **PTZ** (Optics/Marshall/…) | NDI\|HX / RTSP / USB | n/a | watch/cam |
| **Phones** | UVC / Continuity | n/a | gy cam only |

**Rule of thumb:** cinema bodies without native PTP enter ST 2110 through **SDI → 2110 IP converter** or the OB switcher; only a subset of studio/Box + Blackmagic IP lines speak 2110+PTP on the camera head.

## ST 2022-7 hitless (dual destination)

```bash
gy venue --sink st2110 \
  --rtp   rtp://239.100.1.10:5004 \
  --rtp-b rtp://239.100.2.10:5004
```

| Piece | Behavior |
|-------|----------|
| Encode | **One** ffmpeg process |
| Fan-out | `-f tee` to primary + secondary RTP |
| SDP | `a=x-gy-2022-7-*` dual-path announce |
| Sidecar | `$TMPDIR/gy-venue/st2022-7.json` |

**Honest limit:** professional hitless merge wants bit-identical packets on both paths from a multi-NIC packetizer. gy dual-dest tee is **path diversity from a single encode** — the right software span; facility gateways can still re-clone for full 2022-7.

## ST 2110-40 ANC (program bus capture)

Cleanest starting point — **not** a full caption engine:

| Capture | DID/SDID | Content |
|---------|----------|---------|
| mark-as-ANC | `0x5F` / `0x01` | UTF-8 `cgf:…` program |
| tally / mode | `0x5F` / `0x02` | live/hold/black + slot + flags (preview/caption) + conductor |
| bus snapshot | `0x5F` / `0x03` | compact JSON |
| preview | `0x5F` / `0x04` | PVW mark/label when armed (`/preview`) |
| caption | `0x5F` / `0x05` | UTF-8 on-air text (`/caption`) — not CEA-708 |

```bash
# capture point = conductor take/hold/black (hub type:program)
gy venue --sink st2110 --anc-rtp rtp://239.100.1.10:5008
gy venue --sink st2110-40 --anc-rtp rtp://239.100.1.10:5008
# sidecar: $TMPDIR/gy-venue/st2110-40-anc.jsonl
```

`VenueSink.OnANC` fires after every program update. CEA-708 / full VANC inserters remain facility-side.

## What gy does *not* claim

- Running a PTP grandmaster or boundary clock
- NMOS IS-04/05 registry
- Full CEA-708 captions / standard SMPTE DID registry packing
- ST 2110-31 AES3
- Hardware traffic shapers (2110-21 TPN is **signaled**, not enforced)
- Bit-identical multi-NIC 2022-7 packet cloning (tee is best-effort hitless diversity)

Those remain **facility** layers; gy keeps essence RTP + program bus + honest sync gaps.
