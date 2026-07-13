# Powerhouse stack — GrokYtalkY ↔ overview · blank · grok-cli · Qbpm

Clean route for DOJO / venue / research / jam without duplicating authority.

```
                    ┌─────────────────┐
                    │  grok-cli multi │  notes · Ollama · Railway stages
                    │  staging        │
                    └────────┬────────┘
                             │ GROK_* / MCP
         ┌───────────────────┼───────────────────┐
         ▼                   ▼                   ▼
   ┌──────────┐       ┌──────────┐        ┌──────────┐
   │ overview │       │  blank   │        │   Qbpm   │
   │ research │       │ stagehub │        │ jam graph│
   └────┬─────┘       └────┬─────┘        └────┬─────┘
        │                  │                   │
        │     tools manifest · stageforge      │ live WS
        └──────────────────┼───────────────────┘
                           ▼
                    ┌──────────────┐
                    │ GrokYtalkY   │  hub :9876 · forge · program bus
                    │ gy serve     │  dual Glyph · venue ST2110/NDI
                    └──────┬───────┘
                           │ type:program · gyst · ANC
                           ▼
                    venue · sfu · agent · Pages
```

## Authority rules (do not fork)

| Concern | Owner |
|---------|--------|
| On-air PGM/PVW/caption | **gy program bus** (`/conductor` · `/take` · `/caption`) **per hub room** |
| Chat → caption (opt-in) | **chat-bridge `--program-caption`** merges caption only (no take) |
| Edge mid-lane | **gy mid-lane** → `GY_EDGE_URL` (program + hexlum envelopes) |
| Forge identity | **cgf:** marks + lattice on hexlum |
| ANC mark/tally/preview/caption | **OnANC** from program bus only |
| Research notes / AI iterate | overview / blank / grok-cli |
| Graph / Strudel jam graph | Qbpm |
| Multi-stage agent jobs | blank stageforge + grok-cli Railway stages |

## Shared contract

See `integrations/powerhouse-protocol.json` (mesh types + env + ports).

| Mesh type | Direction |
|-----------|-----------|
| `program` | gy hub → venue / Qbpm / blank multiview |
| `gyst` hexlum | gy hub → dual Glyph / agent / SFU |
| `chat` | hub ↔ Space / blank thread |
| jam pattern | Qbpm BC `qbpm-jam` · optional hub chat |

## Launch recipes

### DOJO core
```bash
gy serve
GY_ROOM=dojo gy                    # companion / forge in room
gy venue --sink st2110 --anc-rtp rtp://239.100.1.10:5008
# edge mid-lane (public ladder — reference worker in edge/mid-lane)
cd edge/mid-lane && npm i && npx wrangler dev --port 8788
gy mid-lane --room dojo --edge http://127.0.0.1:8788/mid
# open site/mid-lane.html → Connect
```

### blank + StageForge (+ optional gy)
```bash
cd ~/dev/blank && ./Launch-StageForge.command
# with gy hub staged: STAGEFORGE_GY=1 ./Launch-StageForge.command
```

### overview research + blank tools
```bash
cd ~/dev/overview && npm run dev
# Tools page lists GrokYtalkY forge/venue entries (blank-tools-manifest)
```

### grok-cli multi-stage (notes + gy)
```bash
cd ~/dev/grok-cli-main && ./scripts/powerhouse-stage.sh
# stages: notes-backend → gy serve → optional venue
```

### Qbpm jam + gy program awareness
```bash
# qbpm on :8796; open console:
# window.qbpmGy.connect('ws://127.0.0.1:9876/')
# receives type:program → live caption/tally strip
```

## Env (common)

| Var | Tool | Purpose |
|-----|------|---------|
| `GY_HUB` | all | default `ws://127.0.0.1:9876/` |
| `GY_CAP` / `GY_ROLE` | gy | capability handshake |
| `GROK_CLI_URL` | gy TUI | notes backend |
| `XAI_API_KEY` | gy / grok-cli | Grok |
| `STAGEFORGE_GY` | blank | stage `gy serve` service |
| `QBPM_GY_HUB` | Qbpm | program bus WS URL |

## Docs map

| Doc | Where |
|-----|--------|
| This file | `GrokYtalkY/docs/powerhouse-stack.md` |
| 2110 / PTP / cameras | `GrokYtalkY/docs/st2110-sync-cameras.md` |
| Capacity / lanes | `GrokYtalkY/docs/streams-capacity.md` |
| Pages | https://fornevercollective.github.io/GrokYtalkY/docs.html |
| blank StageForge | `~/dev/blank/stageforge.yaml` |
| overview tools | `~/dev/overview/src/tools/blank-tools-manifest.ts` |
| Qbpm forge Pages | https://fornevercollective.github.io/Qbpm/ |
