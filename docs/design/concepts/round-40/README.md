# Round 40 — the topography as a city

Issue: #854. Brief: `BRIEF.md`. Round 30's `the-whole.html#s3` was the
ratified topography; round 39 carried it. This round redraws it as a city —
the whole estate on one big map — and gives the `survey` altitude stop a
job: height = importance.

## The idea in one paragraph

Buildings are the things on the network, shaped by type and sized by
traffic; districts are VLANs; a router's territory is a borough of
districts, and a second router is a second borough on the same map. Roads
are traffic — width for volume, colour for verdict — and the Internet is the
highway out of town. Height is importance, with two readings the operator
can switch between: **depended-on** (how many hosts talk to it) and
**watched** (the flag and watch weight the operator has put on it).
Flagged buildings light up; districts nothing logs sit unlit.

## Directions

| Concept | File | What it is |
|---|---|---|
| **Isometric** | `isometric.html` | True 2.5D: isometric plates, extruded cubes, roads as ground ribbons. The skyline is the native view; street level is the camera dropped into one district. |
| **Relief** | `relief.html` | One top-down city plan. At the lower stops height shows as roof brightness and contour rings; the survey stop tilts the same plan with a CSS 3D camera so the blocks rise. One drawing, one camera. |

Both prove the same four scenes on the same data story: `#survey` (the
skyline), `#street` (LAN district close up, hover card), `#estate` (both
boroughs and the road between them), `#alarm` (the UNPLANNED road lit,
Guest and Cams unlit). Screenshots: `shots/<direction>-<scene>.png`, plus
`<direction>-survey-watched.png` for the second importance reading.

Capture: from `frontend/`, `node ../docs/design/concepts/round-40/capture.mjs isometric` (or `relief`).

## What each direction is good and bad at (reviewer's notes, Fable 5)

- Isometric reads as a city at once and puts every building on the map as
  a solid thing; its cost is that a real layout needs a placement pass
  (plates must not overlap, roads must route between them) that a flat
  plan does not.
- Relief is one drawing with one camera, so the altitude slider is a
  single continuous motion and street level is just the plan zoomed; its
  cost is that a plan footprint sized by traffic becomes a wall when
  extruded — rb5009 is a slab, not a tower — so type-shaping would have to
  be redrawn for the tilt.

## Verdicts

Owner, 2026-09-03, on the first cut:

> I prefer isometric - with some caveats. I don't like the straight/angular
> connections. and we don't have to fit everything into one screen. This
> view is already designed to zoom in/out and pan, so we shouldn't clutter
> things together to squeeze them into a single screen.
>
> I also don't like every single thing being a cube. It would be really
> cool if we produced a bunch of generic looking devices; a PoE cam, a
> router, a server box, a workstation box, a laptop, a phone, a switch, etc
> etc.

Later the same day: "I think we should view these as two alternative views, not one replacing the other." The city is a second view beside the 2D topography, not its successor.

Relief dropped. Second cut of isometric (`isometric.html`, replacing the
first): curved roads, a map larger than the viewport with the scenes as
viewports onto it, and a device library — see `BRIEF.md` "Second cut".

Owner, 2026-09-03, on the second cut (`isometric.html`, commit f36fba1):

> YESSSSS FABLE. This is EXCELLENT! Approved.

**Ratified.** The buildable record is `docs/design/screens/city/DESIGN.md`.

## One layout (item 50, 2026-09-03)

Owner ruling: the 2D map and the city share one ground plan; only the
vocabulary changes. `one-layout.html` shows the round-40 model as a flat
plan beside the city, with an overlay registering the two to 0 px
(`shots/one-layout-*.png`). Recorded on #852.

Owner, on the example:

> To be honest the river reads as a road on both.
>
> I generally like this! 2D looks a bit cluttered.

Carried into the build: the river must read as water in both views
(#866); the flat plan at the `zones` stop carries less than the example
draws (#869).
