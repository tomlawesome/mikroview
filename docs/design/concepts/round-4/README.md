# Interface visioning — round 4 (convergence)

Continues rounds 1–3 (see `../round-3/README.md` for the full verdict
trail), under #482/#483/#385 phase 2. Same format: self-contained HTML,
no build step, real motion, the same RouterOS-shaped data story.

## The owner's round-3 batch (2026-08-23), which this round answers

- **M Core: dropped** — *"I like this graphically, but the waterfall
  just does this better."*
- **N Score: dropped** — *"cute, but it's just confusing to read and
  you've got to be a musician to appreciate it."*
- **O Waterfall: 'the fall' ratified as the hero.** The other O scenes
  were not kept.
- The convergence brief, in the owner's words: Atlas II refined **gets
  the waterfall as a hero live view; we keep the live view and the
  topography view, but the landing page is the waterfall** — perhaps
  reimagined as the star fall. Style identities for **two broad
  themes**: a water-based theme (*"networks flow, after all"*) and a
  similar but distinctly different space/universe theme. **Atlas II
  refined is just called Atlas from here.** The live view's columns
  need sorting so information aligns horizontally and vertically. The
  Reach view develops further, becomes part of the topography somehow,
  and gains recentring: click an item and it becomes the central
  focus, showing *its* possible connections — devices, ports, all.

## What this round is

Not a divergence round: the structure is settled and both files share
it. **Atlas** = the fall as landing page, the map, reach inside the
map, and the stream. What varies is the *soul* — two theme identities
on identical bones:

- **`atlas-water.html` — Water.** The fall over the brink; currents,
  surges, uncharted channels; basins on a harbour chart; the router is
  the harbour mouth. In birdcage's tongue a canary would be a
  *whirlpool* (naming lives in birdcage; no honeypot logic here).
- **`atlas-space.html` — Space.** The starfall under a faint starfield;
  trails with star heads, showers, unobserved sectors; clusters on
  dotted orbits; the router is the station. In birdcage's tongue a
  canary would be a *black hole*.

**The diff between the two files is deliberately small**: the token
block at the top of the stylesheet (surfaces, ambient accent, region
texture), the fall's mark rendering (currents vs star trails), and the
tongue. Everything else — markup, geometry, data story, the working
surfaces — is byte-identical. That is the design claim this round
makes: *a theme is a set of tokens, not a rewrite.* #492 (themes,
v0.4.0) ships against exactly this separation, and the #482 framework
choice must support it (CSS custom properties as the theme boundary).
Theme language is ambience only — headings, empty states, region
names. DROP is always DROP, a watch is a watch, and the stream/filter
surfaces are plain in every theme.

## The four scenes (same in both files)

1. **The fall / the starfall — the landing.** Round 3's ratified hero,
   kept close to what worked: the dial of eight boundary bands, the
   live edge on top, 15 minutes of memory below the brink. The
   incident: a current/light kindling at 13:52 in a channel/sector
   that has been dry/dark for 41 days.
2. **The map.** Lanes as basins/clusters, boundaries stamped with
   coverage (GAUGED·OBSERVED / QUIET / UNCHARTED·UNOBSERVED), port
   chips for what policy admits, flowing dashes for what is observed.
   The unplanned crossing is a red arc **cut at the lane's edge** by
   the default drop — its *intent* continues as a faint ghost that
   never arrives (Riverline's one honest detail, surviving). A slim
   fall ribbon runs under the map: the stream is never gone.
3. **Reach — the map, recentred.** The owner's integration ask: reach
   is not a separate view, it is the map with a question focused on
   one host. Inside the ring/orbit: lane-mates, no rule needed. Every
   crossing judged per direction with its ports; stop bars die at the
   membrane, pulsing where knocked right now; one-way rules visible
   (tom-desktop watches the camera on :554; the camera cannot look
   back). **Every node is a lens**: the nas node carries the recentre
   affordance — click and the strands redraw for *its* question.
   Reach & Compose survives as the dock: the observed denial drafted
   as a RouterOS command, printed, never run.
4. **The stream — columns squared.** The owner's alignment ask, done
   structurally: hostname and address are separate columns; protocol
   and port are separate columns; times, addresses and ports are
   right-aligned tabular numerals; action badges are fixed-width.
   Scan any column and the same kind of fact is always under the eye.

## Validation record

- Lane set (lan `#3987e5` · srv `#199e70` · guest `#d76a9e` · iot
  `#c98500`) revalidated on both surfaces (`#08131a` water, `#06070d`
  space): six checks pass.
- Heat identities (accept `#5aa7f0` / drop `#e05252`): separation ΔE
  22.1 deutan and contrast pass on both surfaces. The bright steps sit
  above the categorical lightness band **by design** — they are
  sequential-ramp endpoints, not series slots (the validator's scope
  note covers this); identity is carried by hue + labels, never by
  brightness alone.
- Theme accents (teal `#37b3c8` water, indigo `#7d8be8` space) are
  chrome-only — headings, nav, rules — never data marks, and the space
  indigo is kept visually distinct from NAT violet, which appears only
  on labelled marks.
- `prefers-reduced-motion` disables all animation in both files.
- Screenshots in `shots/`, regenerated with
  `cd frontend && node ../docs/design/concepts/round-4/capture.mjs`.

## What happens next

Owner reviews both themes at `round-4/` and returns a batch: theme
verdicts (either, both as shipped alternates, or a blend), plus any
scene corrections. With the direction and its demands now concrete —
the fall's continuous rendering, the map/reach geometry, token-boundary
theming, the aligned stream — **#482 is writable**: the ADR picks the
framework/design-system/styling approach proven against these scenes,
resets the bundle budget (92,000 → 200,000 gzipped) in the same PR, and
closes with owner ratification.
