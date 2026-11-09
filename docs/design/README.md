# Admin design source (v2, 2026-11-09)

This folder is the design source of the admin UI. It was made as a Claude Design canvas
(https://claude.ai/artifact/KVowaWWCZhzo6qrtLrL5BY) and copied here so we can change it in the repo.

- `hp.css` is the design's own CSS (sections 1-18 are the real UI; 19-20 are only for the boards).
- `*.dc.html` are the boards: rationale, tokens, components, 10 screens, print sheet, developer notes.
  Each is one HTML file that links `hp.css`. Light and dark, desktop 1280 and phone 390.
- `canvas.json` is the canvas index (board names and positions).

## How to look at it
The boards need the canvas runtime (`support.js`), which is not in the repo. To view or edit them
as a canvas, publish this folder again with the Artifact tool (type Design), or open the
canvas link above. The production CSS is NOT this file: it lives in `internal/web/static/css/`
and is the port of `hp.css` (see `docs/ui-notes.md`, "Design v2").

## Changes made when porting
- Our class names keep the `c-` prefix.
- Status tokens are `--st-*` (the design had `--s-ok-*`, which clashed with our spacing tokens).
- Usage bars stay on the native `<meter>`, not the `pct-N` classes.
- Media queries are used instead of container queries.
- Where the design is wrong about the product, the product wins: the Router page has the
  MikroTik default plus 7 themes, and "over quota" is not a member status.

When you change the look, change the board here first, then the CSS.
