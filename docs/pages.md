# GitHub Pages

- **Home (stream hero):** https://fornevercollective.github.io/GrokYtalkY/
- **Docs page:** https://fornevercollective.github.io/GrokYtalkY/docs.html

## Source

- Workflow: `.github/workflows/pages.yml`
- Content: `site/`
  - `index.html` — conversation hero with live camera/video half-blocks
  - `docs.html` — full documentation (sidebar)
  - `stream-hero.js` — getUserMedia + file → canvas stream
  - `styles.css`

## Repo settings

**Settings → Pages → Build and deployment**

- Source: **GitHub Actions**

Push to `main` deploys. Manual: Actions → Deploy GitHub Pages → Run workflow.
