# Round 51 — tunnels in the map (#890)

Round 50's answer, redrawn to the verdict: no column, no fold. Each
tunnel is placed in the map as a stop of its own, and the map opens at
the zoom that fits whatever it holds. Round 49's `#flat` is otherwise
unchanged — Internet, router, zone row and wg0 to the pixel.

## The rule

- Tunnels sit **in a row to the right of wg0**, the same card, the zone
  row's own 52 px gap between them (240 px pitch), **busiest nearest the
  router** — wg1, then l2tp-out, wg2, gre-site-b.
- Each gets **its own strand to the router's right shoulder**, the way
  the zones fan from its bottom edge (252, 268, 284, 300, 316 down that
  edge). Round 49's rule reads them: wg1 and wg2 thin and dim
  (established), l2tp-out bright with the ring at the end it arrived at
  (off baseline today, and the header counts it), gre-site-b grey and
  dashed with a dashed footprint (quiet 3 d — the host rule).
- **The frame fits the map.** The stop's own frame is round 49's
  1400×720. Anything placed beyond it widens the opening view until it
  is in, with the city's 40 px of air (`cityFitS`), and the view never
  opens closer than 1:1 — a map that fits the frame is drawn at the
  frame, unmoved. Two tunnels open at 95 %, three at 82 %, five at 65 %.
  From there pan and zoom are the operator's, exactly as in the city.
- **A fit chip** bottom right says how far out the view is and, once
  zoomed by hand, offers `⤢ fit` to go back. At the frame it is quiet.

Nothing folds and nothing is hidden: a sixth tunnel is one more stop and
one step further out.

## The shots

`node ../docs/design/concepts/round-51/capture.mjs` from `frontend/`:
each scene full-frame (2800×1720) and cropped to the top-right quarter
(`-crop.png`, 1800×1400).

- `two-tunnels` — wg1 beside wg0; the map opens at 95 %.
- `three-tunnels` — l2tp-out next, bright, with its ring; 82 %.
- `five-tunnels` — wg2 and gre-site-b; 65 %, everything in view.
- `zoomed` — the same five at 1:1, panned to the router's shoulder,
  gre-site-b off the right edge, l2tp-out's line card open.

## Data story

Round 49's, plus one line: `laptop-anna → 10.97.0.12 · 445/tcp` through
l2tp-out, first seen 22:04, 3×, off the baseline — so the header reads
**off-baseline today 4**. gre-site-b has not been heard for three days.

## For the build

The 2D map (`frontend/src/components/Topography.svelte`) has a fixed
`viewBox` and no pan or zoom yet; the city's `cityFitS`
(`frontend/src/lib/city/project.ts`) is the fit rule to lift. That is
an implementation issue, not this round's.

## Verdict

_(pending)_

Drawn by Claude Fable 5, 2026-09-08.
