# Interface visioning — round 5 (the glue)

Under #634, continuing rounds 1–4 (see `../round-4/README.md` for the
ratified direction). Same format: self-contained HTML, no build step,
real motion, the same RouterOS-shaped data story as every prior round.

## What this round is

Rounds 1–4 settled the identity (Atlas, now live per #633) and the fall
(accepted on the live build, #616). What was never designed is **the
glue**: what holds the scenes together as one product, and which scene
is home. The owner's brief (2026-08-30): the rigid v0.4.0 rail + top bar
"isn't really doing it"; the shipped screens "don't tie together
correctly in style and identity" (login essentially unchanged since the
early versions); and "the map feels like it's going to be the major
driving force behind the new mikroview", while "the fall might be the
screen you want to see on login".

Both directions share the glue **byte-for-byte** — the same four glue
elements, so what is being compared is only the arrival:

1. **The door** — login reworked to the Atlas identity: the void, faint
   orbits, glass card, and the standing promise ("MikroView never
   connects to your router").
2. **The scene bar** — each scene's own chrome (#633): wordmark (opens
   the atlas, key `m`), scene name + epithet, status cluster; on Stream
   it also carries the retired toolbar's controls.
3. **The atlas overlay** — the one navigator: zones on dotted orbits
   around the router (click = reach into that zone's traffic, #438),
   destinations beneath. Mirrors the in-flight `AtlasNav.svelte`.
4. **The stream, columns squared** — round 4's alignment on the Atlas
   base: separate host/address and proto/port columns, right-aligned
   tabular numerals, fixed-width action badges.

- **`direction-p-maphome.html` — P: the map is home.** Sign in, arrive
  at your network (round 2's Atlas II map, the standing topography
  reference, carried forward verbatim); the stream underfoot; the fall
  one click deeper.
- **`direction-q-fallhome.html` — Q: the fall is home.** Sign in,
  arrive at the traffic pouring — the accepted live fall restated in
  the mockup's data story; the map one gesture away through the atlas.

The scene-tag annotations (top/bottom right of each scene) are round
apparatus, not design.

## Validation record

- Lane set (lan `#3987e5` · srv `#199e70` · guest `#d76a9e` · iot
  `#c98500`) validated on the Atlas surface `#06080e` (dataviz six
  checks): lightness band, chroma, normal-vision floor (worst ΔE 18.9)
  and contrast PASS; CVD separation worst pair 6.4 deutan — inside the
  6–8 floor band, legal here because lanes are always direct-labelled
  (zone cards, band headers) and never colour-alone.
- Verdict/heat identities and the alarm are the live app's own tokens
  (`frontend/src/app.css`), unchanged.
- `prefers-reduced-motion` disables all animation in both files.
- Screenshots in `shots/`, regenerated with
  `cd frontend && node ../docs/design/concepts/round-5/capture.mjs`.

## Owner verdicts

Pending.
