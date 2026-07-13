# GitHub Pages

- **Home (stream hero):** https://fornevercollective.github.io/GrokYtalkY/
- **Docs:** https://fornevercollective.github.io/GrokYtalkY/docs.html
- **DOJO e2e:** https://fornevercollective.github.io/GrokYtalkY/dojo.html
- **Chat demo:** https://fornevercollective.github.io/GrokYtalkY/chat.html
- **Home (sections · install · keypoints):** https://fornevercollective.github.io/GrokYtalkY/#conversation
- **Burst orb / Spaces:** https://fornevercollective.github.io/GrokYtalkY/burst.html
- **Live News glyph wall:** https://fornevercollective.github.io/GrokYtalkY/livenews.html

## Source

- Workflow: `.github/workflows/pages.yml`
- Content: `site/`
  - `index.html` — conversation hero + vwall
  - `docs.html` — full documentation (sidebar) — keep in sync with TUI `?` help pages
  - `dojo.html` / `chat.html` / `burst.html` / `livenews.html` — demos
  - `stream-hero.js` · `dojo-room.js` · `styles.css`

## Repo markdown (not Pages-rendered, linked from docs)

| File | Topic |
|------|--------|
| `docs/streams-capacity.md` | Hub / SFU / CF lanes · venue CLI |
| `docs/st2110-sync-cameras.md` | 2110-20/30 · PTP · cameras · 2022-7 |
| `docs/stream-binary.md` | gyst / gyhex / pcap |
| `docs/burst.md` · `chat.md` · `companion.md` | Feature deep-dives |

## Keep in sync

When shipping features, update:

1. TUI multi-page help (`ui_help.go` — keys/stream/forge/venue/cli/docs)
2. `gy --help` (`main.go` printHelp)
3. `site/docs.html` sidebar + sections
4. Relevant `docs/*.md`

## Deploy

**Settings → Pages → Build and deployment** → **GitHub Actions**

Push to `main` deploys. Manual: Actions → Deploy GitHub Pages → Run workflow.
