# GrokYtalkY plugins (JSON manifests)

In-process hooks — no Go `.so` plugins. Built-ins ship in the binary
(`invert`, `mirror`, `threshold`, `heatmap`, `mesh-tag`, `quiet-roster`).

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
