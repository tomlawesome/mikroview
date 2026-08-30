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

## The owner's first batch (2026-08-30), and what it did to the round

- **The door (v1) rejected in both directions**: "It needs to be much
  nicer. I'd like a short snappy, fast to load across all platforms
  login/logout like Orbit has, but more in tune with Mikroview's
  purpose." Rebuilt as one sub-second beat: instant paint, the brink
  draws, a miniature fall begins, underline credentials, no card, no
  chrome; the way out storyboarded as three beats ending "your
  router's logs keep arriving".
- **The P/Q framing rejected**: "direction p and q are basically the
  same except they're both missing some of the screens, when they both
  need all those screens."
- **The deck, from the owner's own read of the stacked format**: "a
  vertical scroll and snap concept? I think the order no longer
  matters — which is really cool, because the user can select their
  OWN preferred order." Promoted to the design as **direction R
  (`the-deck.html`)**, superseding P and Q: every scene a
  full-viewport card in a scroll-snapped vertical deck, the operator's
  own order, sign-in landing on their first card; the atlas stays the
  jump navigator and gains the deck-order editor. The stacked-scene
  format of rounds 1–5 was review apparatus; R adopts it deliberately
  as the interaction model.

## The owner's second batch (2026-08-30), and what it did to R

- **Topography ⇄ atlas chart unified**: "should [the atlas chart] be
  a reduced version of the Topography graph from a different viewing
  angle? ... maybe it's not even its own screen, and we just have a
  cool looking slider/click toggle that switches (with a cool
  transition) between the graph and explore (map) views?" Built: the
  deck's topography card carries an EXPLORE ⇄ ORBIT toggle — one SVG,
  one node set, two coordinate systems; the nodes travel on toggle.
  #485's Map and the atlas's chart become two angles of one graph.
- **The destinations footer rejected**: "don't like the presentation
  of the links at the bottom ... those do belong in some kind of menu
  somewhere ... I don't want a cookie cutter boring menu." Built: the
  far orbit — destinations drawn as diamond bodies on the atlas's
  outermost orbit, same chart language as the zones; the footer grid
  is gone. (Fallback candidate if this misses: a radial menu around
  the router station.)

## The owner's third batch (2026-08-30)

- **Door v2 accepted** ("Congratulations on the new login, great
  work") with two amendments, both applied as v3: the falling pattern
  covers the whole screen behind the login, and the amber draws as a
  thin box framing the wordmark (1.5px) instead of the underline.
- **The rigid top bar rejected again in card form**: "can we have the
  information presented in more of a free floating way?" Applied: no
  strip, no border, no glass — wordmark and scene name float top-left,
  status cluster floats top-right, stream controls float beneath it.
  The build's SceneBar.svelte follows once the round is ratified.

## Owner verdicts

- Door: v3 standing (v2 accepted + full-screen fall, boxed wordmark).
- P/Q framing: rejected, superseded by R (the deck).
- Direction R, the topography toggle, the far orbit, floating chrome:
  pending.
