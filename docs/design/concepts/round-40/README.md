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

_Recorded verbatim from the owner as they arrive._
