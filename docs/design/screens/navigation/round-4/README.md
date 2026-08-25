# Navigation (#486) — design round 4

Round 3 closed 2026-08-23 (verdicts verbatim in `../round-3/README.md`):
the demo pages' background sat at practically the same colour as the
frames; the owner ordered a **grey canvas for light, navy for dark**.
And the broken-state question resolved: **a red outline on the icon**,
not a dot.

Q IV (`direction-q4-rail.html`, `?theme=light`) — content identical to
round 3, two changes:

- **The canvas is never the surface**: the demo page behind the frames
  is navy (`#1b2340`) in dark and grey (`#c3c9d4`) in light, so the
  mockups read as objects on a desk, not colour fields. App surfaces
  inside the frames are exactly the ratified tokens. (For the build:
  this is a mockup-presentation token, `--canvas`, not an app token.)
- **Broken is a ring, not a number** (owner decision): the count badge
  belongs to Flags alone; anything else in a broken state wears a red
  outline on its icon, in every density, cleared when the break
  clears. Watchlist wears it here — the egress-cadence watch is
  broken in the data story. aria-label carries the reason.
- The handle stands for now ("we'll change it later"). Wordmark kept.

## Validation record

Unchanged from round 3 (`../round-3/README.md`): both lane sets, both
surfaces; the canvas is chrome behind the demo, never a data surface.
Screenshots in `shots/` (`q4-<scene>-<theme>.png`), regenerated with
`cd frontend && node ../docs/design/screens/navigation/round-4/capture.mjs`.

## Open with the owner (round-4 batch)

1. Canvas tones right? (Navy `#1b2340` · grey `#c3c9d4` — easy to
   nudge either way.)
2. The broken ring as drawn (1.5px alarm outline, 2px off the icon):
   right weight, or heavier?

## Owner verdicts — round 4 closed (2026-08-23)

> "1. they're gine. 2. SLightly heavier, and... Red outline goes
> round the icon and the word, if that view is selected, icon only if
> the bar is icon only."

- **Canvas tones stand** (navy `#1b2340` · grey `#c3c9d4`).
- **The ring**: slightly heavier, and it encloses icon + word in
  icons+text density, the icon alone in icons only → round 5.
