# GrokYtalkY plugins (JSON manifests)

In-process hooks — no Go `.so` plugins. Built-ins ship in the binary
(`invert`, `mirror`, `threshold`, `heatmap`, `mesh-tag`, `quiet-roster`,
**`theme-vision`** — VisionPlugin: theme-reactive painter on `vision-take`).

## Install a manifest

```bash
mkdir -p ~/.config/grokytalky/plugins
cp examples/plugins/*.json ~/.config/grokytalky/plugins/
# or: export GY_PLUGIN_DIR=/path/to/plugins
gy doctor plugins
# in TUI:
#   /plugin list
#   /plugin on mesh-tag
#   /plugin style invert
#   /plugin reload
```

## Schema

| field | meaning |
|-------|---------|
| `name` | registry key |
| `description` | `/plugin list` blurb |
| `enabled` | default on/off |
| `style` | `invert` \| `mirror` \| `threshold` \| `heatmap` |
| `mesh` | `chat-prefix` \| `type-filter` |
| `prefix` | for `chat-prefix` |
| `types` / `drop_types` | for `type-filter` allow/deny lists |
| `level` | threshold 0–255 |

Style plugins participate in lab `m` / `/mode` cycle after built-in pixel modes.
Mesh hooks run on hub JSON (inbound `handleWS`, outbound `SendJSON`).

## theme-vision (builtin VisionPlugin)

Reacts to mesh/in-process `vision-take` **THEME** lines:

| Theme | Grade | Pixel hint |
|-------|-------|------------|
| breaking | red scan | scan |
| markets | green matrix | hex |
| conflict | hot red edge | neon |
| weather | cool cyan | dither |
| earthcam | scenic green | neon |
| … | … | … |

```bash
# auto on with vision takes:
export GY_VISION=1
export GY_VISION_THEME_STYLE=1   # set lab PluginStyle=theme-vision
export GY_VISION_THEME_PIXEL=1   # map theme → PixelMode when no STYLE line
# TUI:
/plugin list
/plugin style theme-vision
/plugin off theme-vision
gy doctor plugins
gy doctor vision
```
