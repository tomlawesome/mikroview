# Navigation (#486) — design round 3

Round 2 closed 2026-08-23. Verdicts, verbatim:

> "The menu open icon should be much nicer, more interesting and
> networking appropriate, it should be vertically centralised,
> always. I like it in total though, but it would be nice to see mock
> ups in both light and dark; you don't need to generate extra
> content, just alternate for the screenshots. More work to be done
> but this is acceptable for now. The arrow should restore persistent
> state and you select that state from the footer."

Q III (`direction-q3-rail.html`, `?theme=light` for the light set) —
same four scenes, three changes:

- **The handle**: a hub on the screen's edge with three links fanning
  inward — the network waiting to come back out; the hub carries the
  same pulse as the receiving dot (still under reduced motion).
  30×84px, glass, **vertically centred on the viewport, always** —
  independent of scroll and page. Wears the open-flag badge; first in
  tab order.
- **Restore, not drawer** (owner decision): the handle re-applies the
  persistent undocked state — same density, same scroll, focus on the
  current page. The round-2 drawer and ⇥ pin are dropped. **States
  change in the footer only**: ⇔ density, ⇤ dock.
- **Light and dark**: one file, `?theme=light` adds a token-swap
  class — no extra content, per the verdict. Light lane steps
  re-validated (see below). Identity work stays #492's.

## Validation record

- Dark: unchanged from rounds 1–2 (`round-1/README.md`).
- Light surface `#eef1f7`: the dark lane steps failed contrast
  (guest 2.88:1, iot 2.71:1), so light uses its own steps —
  lan `#2f77d3` · srv `#12855d` · guest `#c2508a` · iot `#a06a00`:
  lightness band, chroma, normal-vision floor (18.9), contrast all
  PASS; CVD separation WARN (srv↔guest 6.6 deutan, the same 6–8
  floor band as dark), covered by the same always-labelled relief.
  This is the dataviz rule working as intended: dark mode's steps are
  selected, not flipped — and so are light's.
- `prefers-reduced-motion` disables the hub pulse and all animation.
- Screenshots in `shots/` (`q3-<scene>-<theme>.png`), regenerated with
  `cd frontend && node ../docs/design/screens/navigation/round-3/capture.mjs`.

## Open with the owner (round-3 batch)

1. The handle mark (hub + three links, breathing): right kind of
   thing, or push further?
2. Round 1's question, in plainer words this time: the rail's Flags
   row shows a red count (6). The Watchlist row currently shows
   nothing, even while a watch is broken — you only find out via the
   flag it raises. Should the Watchlist row also show a small dot
   while any watch is currently broken? (A dot, never a number — one
   red count on the rail, told once.)
3. Wordmark: kept, per round 2.

## Owner verdicts — round 3 closed (2026-08-23)

> "Your themes are just so extreme, it's really hard for a human to
> read/see as everything in dark is SO dark and everything in light
> is SO bright."

clarified, after a first misread (an app-token recalibration was
started and discarded):

> "My point was the back ground for each demo page is practically the
> same colour as the screenshots you're showing. Give the light theme
> a grey background and the dark one a navy blue"

- **The demo canvas, not the app tokens, was the problem** → round 4:
  navy canvas behind the dark frames, grey behind the light ones; app
  surfaces unchanged.
- **The handle: fine for now** ("we'll change it later").
- **Broken state: red outline on the icon** instead of the proposed
  dot — the Watchlist icon wears a red ring while a watch is broken.
- Wordmark: kept (round 2).
