# Round 52 — the tunnel cluster (#890)

Round 51's answer, redrawn to its verdict and the two lines after it:
not a column, not a row, not scattered — one group, in one place, that
takes the free space around what is already there. Round 49's `#flat`
is otherwise unchanged.

## The rule

- Tunnels are **one group where wg0 is**, the space right of the router.
  They fill it the way a town fills in around a crossroads: staggered,
  no two in a line, **busiest nearest the router**, taking the free
  space beside wg0, beside the Guest zone, between the Internet card
  and wg0, under Guest in the bottom band — before anything spills past
  the frame.
- Each gets **its own strand to the router's right shoulder** (gre to
  the top edge beside the Internet rib), read by round 49's rule: thin
  and dim established, bright with a ring off baseline, grey dashed
  not heard. A strand threads between cards where it can, and runs
  under a nearer card where it must, as the Guest rib runs under the
  Guest card.
- **The frame fits the map** (round 51's rule, now on all four edges):
  it grows only as the group does — two, three and five tunnels open at
  100 %, 100 % and 97 %; eight at 97 %.
- **The fit chip** (round 51) stays.

## The shots

`node ../docs/design/concepts/round-52/capture.mjs` from `frontend/`:
each scene full-frame (2800×1720) and the top-right quarter
(`-crop.png`, 1800×1400).

- `two-tunnels` — wg1 below and right of wg0.
- `three-tunnels` — l2tp-out below, nearer the router, bright, with its ring.
- `five-tunnels` — wg2 beside l2tp-out; gre-site-b (quiet 3 d) in the
  pocket between the Internet card and wg0.
- `eight-tunnels` — wg3 beside Guest, site-c beside wg0, ovpn-legacy
  under Guest. The group is full; a ninth grows the frame.

## Data story

Round 49's, plus one line: `laptop-anna → 10.97.0.12 · 445/tcp` through
l2tp-out, first seen 22:04, 3×, off the baseline — header reads
**off-baseline today 4**. gre-site-b not heard for three days. wg3,
site-c and ovpn-legacy exist only in the eight-tunnel scene.

## For the build

The placement is a packing rule, not hand positions: nearest free
188×56 pocket to the router that touches no card, bar or rib, busiest
first. `Topography.svelte` has a fixed viewBox and no pan or zoom; the
city's `cityFitS` is the fit rule to lift. Implementation issue once
ratified, not this round's.

## Verdict

_(pending)_

Drawn by Claude Fable 5, 2026-09-08.
