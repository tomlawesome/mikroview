# Round 46 — flags and watchers marked on the city

Issue: #981. Round 40's `isometric.html` (ratified, #854) gave the city
height as importance, but a flagged host or a watched district looked
like any other until you read heights — the 2D map's ⚑ and ◉ marks had
no local counterpart on a building or a district plate.

## Direction C — paint the house

Directions A (tinted block + count disc) and B (ring + number) were
drawn first, in this same round — see **Superseded**, below. Seeing
both, the owner changed direction completely, 2026-09-06:

> items that have watcher notification get a purple outline, not too
> thick, not too thin, but bold enough to notice easily. The more
> watcher notifications, the bolder the line becomes, as well as the
> current taller.

> Items that have flag notifications, their colour changes red
> translucent. So in a green neighbourhood … a green house (host)
> would turn into a red one. If none of the other hosts there have
> flags, they'd all stay green. … When you click on the house to see
> the information card … then you get to see the number of
> flags/items. The more flags, the less translucent the item becomes.
> So it becomes taller, and bolder.

No discs, rings or numbers on the map; the counts moved to the
building's click card.

Direction C was then drawn and shot twice more, and corrected twice more
by the owner, all still 2026-09-06:

> I do like the colourisation and the outline, though I meant the
> actual item not the cube/tower it sits on.

> I think I've changed my mind on using height for the watchers/items.
> It looks rubbish just raising them on squares.

> we dont need a pedestal.

> Just drop the height concept altogether.

> It doesn't work, it looks rubbish with things sat on cubes.

So the marks moved from the plinth to the device symbol itself, and the
plinth — and the height it carried — came off the page entirely.
Removing height from the product (not just this mockup) is tracked
separately on #986. One more idea landed the same day, confirmed before
drawing:

> Maybe we could have some kind of throbbing glow for items that are
> raising activity spike flags instead.

Read as: a slow pulse on a flagged building's rim, but only where the
flag is an activity spike — activity is the one flag kind that is
*happening now*, so motion fits it and nothing else; other flags stay a
still red. `marks.html` draws direction C only — no toggle, one page.

Two more rulings landed on that drawing's first crops, still
2026-09-06. On an IoT puck still wearing round 40's halo ring and `✱1`
glyph:

> I said to remove these rings with numbers. Get rid of them
> completely.

Applied to the whole page: no halo, no count glyph, no tally glyph on
any building, card, pill or label, at any stop — wg0's roof label and
bridge badge lost their `◉1` too. Counts exist only as words on the
click card. And on a mark that stroked every face of every box the
symbol is built from:

> These overlapping planes are weird. The outlines are supposed to
> just be lines … I can't even tell what you're trying to show me.

A mark is now ONE line round the symbol's silhouette (see **Choices**).
The same pass also redraws the street pills (#991) and the camera as a
small home CCTV dome (#992) — both drawn here first, so the owner rules
on a picture before any product code moves.

### The rules

- Flagged (`b.flags > 0`): the device symbol itself — every shape it is
  built from — takes `--alarm` as fill, opacity
  `min(0.9, 0.45 + 0.15*(flags-1))`, stamped over its ordinary
  district-ink self; its outline (see **Choices**, below) strokes in
  the same ink, width `min(3, 1 + 0.5*(flags-1))`. Unflagged neighbours
  keep their own district colour untouched. The plinth underneath is
  gone — there is no plinth, and nothing marks it.
- Watched (`b.watch > 0`): the device symbol's silhouette strokes once
  in `--watch`, width `min(4, 1.5 + 0.75*(watch-1))`. On a building
  that also carries flags, the watch line is pushed outward from the
  silhouette by half of both stroke widths plus a hair, so it sits a
  touch proud of the flag rim — side by side, never on top of it.
- Activity spike (`b.spike`): a flagged building whose open flag is an
  activity spike also pulses — its rim's opacity breathes 0.45→1→0.45
  and a soft glow alongside it swells from almost nothing to a wide
  blurred bloom (1.5px at 0.12 → 9px at 0.75), both on a 2s ease-in-out
  loop. The swing is deliberately wide: a first draft breathed
  0.6→1/2px→6px and the dim and bright freeze-frames were nearly
  indistinguishable, which is no way to ratify a throb from stills.
  The red fill itself does not animate; only the rim and the glow do.
  Under `prefers-reduced-motion`, the pulse is dropped for a steady
  full-opacity rim and a steady mid-bright glow (6px at 0.55), not
  fully switched off — the mark
  would otherwise carry no signal at all under a rule that only exists
  to answer "is this happening now".
- No height anywhere, for anything: every device — building, router,
  gateway post — stands straight on its district plate. A marked
  building's plinth-free footprint is exactly the same as its unmarked
  neighbours'; nothing about a mark changes how tall anything stands,
  because nothing stands tall any more.
- District plates and name pills are unchanged from round 40 — the red
  house in a green district is the signal, not a second badge.
- The street scene's click card (round 40's `.hovercard` idiom) gains
  a counts line on a marked building, as plain words — cam-porch's
  reads `2 flags · 1 watch`, the flag words in `--alarm`, the watch
  words in `--watch`, no ⚑/◉ glyphs ("counts exist only as words on
  the click card"). tom-desktop's watch row reads `1 watch` the same
  way.

### What's on the map

Same hosts as round 40: cam-porch (2 flags, 1 watch — IoT, the alarm
scene's own subject), laptop-anna and tv-lounge (1 flag each, LAN),
doorbell (1 flag, IoT), cam-gate (1 flag, Cams), nas (watch only,
Servers), and the wg0 gateway post (watch only — its old `◉1` roof
badge is gone, the purple silhouette is the signal now). Added: **pihole** (Servers — plain green, previously
carried no marks) now carries 3 flags, so the opacity step from
cam-porch's 0.6 to pihole's 0.75 is visible on the same page without
touching the 0.9 cap. cam-porch and pihole also each carry the
activity-spike pulse (see **Choices** for why these two).

### Choices the spec left open

- **How a multi-shape symbol's outline is drawn**: one convex-hull
  silhouette. The library builds each symbol from several boxes and
  discs with no boolean union available to fuse them into one path; the
  first attempt stroked every constituent shape and the owner rejected
  it ("overlapping planes … I can't even tell what you're trying to
  show me"). Now every part contributes its corners (a disc samples its
  rim; the laptop's lid its four points) and Andrew's monotone chain —
  the same hull the borough ring already used — turns them into one
  polygon, stroked once (`hullPointsFor()`/`convexHull()` in
  `marks.html`). A hull cannot follow a concave silhouette (the gap
  between a workstation's tower and monitor is bridged), but one clean
  line that slightly rounds the shape beats a cage of true edges.
- **The ground shadow**: now that there is no plinth to cast one, each
  device keeps the soft dark diamond that used to sit under the plinth,
  sized to the device's own footprint radius — the closest thing the
  library already had to a "shadow", reused rather than inventing a new
  shape.
- **How the glow is drawn**: a second copy of the same outline strokes,
  blurred with a CSS `filter: blur()` and breathing its own
  `stroke-width`, rather than an SVG `feGaussianBlur` filter or a CSS
  `drop-shadow`. It stays in the same local, per-device coordinate
  space as the rim, so it scales and tracks the device correctly at
  every scene's zoom without a separate filter region to size by hand.
- **Which two hosts pulse**: cam-porch, because its 2 flags already
  include an activity spike, and pihole, the 3-flag demonstration host
  — both already the flagged hosts the capture crops sit closest to, so
  the pulse is checkable in the same shots that show the opacity step
  and the device-not-plinth correction.
- **The watch outline's "a touch proud"**: the hull's points pushed
  outward from its centroid by `rimW/2 + watchW/2 + 0.6px`
  (`offsetPoly()`), rather than scaling the whole mark — a scale factor
  moves a big symbol's line further than a small one's, where an offset
  in stroke widths puts the two lines side by side at every size.
- **The gate pill's one label (#991)**: the far end's display name is
  read from the rule text's `→ <name>` rather than a second id-to-name
  table — the rule strings already spell every far end out in full, so
  the pill can never drift from the rule it abbreviates. 13px mono in a
  24px pill; the rule number, text and ports move to the gate's click
  card when this is built.
- **The drop pill (#991)**: one plain label, `cam-porch · dropped`, at
  every stop that draws it (street and alarm) — the source is named
  because you are not standing on the building the drop is at; the port
  detail lives on the alarm callout and the click cards.
- **The dome camera's "radial gradient" (#992)**: approximated with two
  flat layers (a currentColor dome, a void-dark smoked centre) rather
  than a real SVG `<radialGradient>` — a gradient's stops live in
  `<defs>` and do not inherit the colour set where the symbol is
  `<use>`d, so a true gradient would paint every district's camera one
  fixed colour instead of recolouring per district like every other
  symbol. No post, no arm; base disc, dome, one lens dot, one highlight
  arc.
- **The capture-host font shim**: `'Liberation Sans'` sits in `--sans`
  after `'Segoe UI'` (#995's idiom) so this Linux capture host renders
  real weights where its own `system-ui` is a single face; the owner's
  Windows/Firefox still matches `'Segoe UI'` first, unchanged. The shim
  is mockup-only and must never be copied into the app.
- **Which host demonstrates 3 flags**: pihole — a plain, previously
  unmarked host in Servers (green), picked to avoid disturbing the
  already-marked cast that demonstrates the 1-flag/2-flag/watch-only
  cases.
- **Where cam-porch's card lives**: the brief asks for it on "the
  street scene", but cam-porch is IoT's, and round 40's `#street`
  camera is fixed on LAN, so cam-porch is never in its frame (same gap
  the old `capture.mjs` already worked around for its close crop). Its
  card floats free on the opposite side of the scene from
  tom-desktop's, with no leader line, purely to show the idiom's new
  second line.

## Superseded

Direction A (tinted block + count disc) and direction B (ring + number)
were drawn in this round first, side by side behind a page toggle. The
owner saw both and rejected them the same day: "I changed my mind
completely," and later, "the rings/circles ideas are both dead." No
shots were kept.

C as first drawn: marks on the plinth, with height — owner: the item,
not the cube it sits on; and no height.

C, second draft: the marks moved to the device symbol as asked, but a
(now flat, unmarked) plinth still stood under every device — owner:
"we dont need a pedestal … Just drop the height concept altogether …
It doesn't work, it looks rubbish with things sat on cubes." The plinth
is gone outright; every device stands straight on the district plate.

C, third draft: an IoT puck still wore round 40's halo ring and `✱1`
glyph ("remove these rings with numbers. Get rid of them completely"),
and the outline stroked every face of every part ("overlapping
planes"). Both purged: no glyph anywhere, one silhouette line.

## Capture

`capture.mjs`, adapted from round 40's: from `frontend/`,
`node ../docs/design/concepts/round-46/capture.mjs`. It shoots
`#survey`, `#street`, `#alarm` to `shots/paint-<scene>.png`, a 420×300
close crop of cam-porch (from `#alarm`, its own scene) to
`shots/paint-cam-porch.png`, a 260×220 close crop of pihole (from
`#survey`) to `shots/paint-3flags.png`, and three more crops of
cam-porch at the same 420×300 frame to show the activity-spike pulse a
still otherwise can't: `shots/paint-spike-low.png` and
`shots/paint-spike-high.png` freeze the pulse's dim and bright points
by giving it a negative `animation-delay` and then pausing it, and
`shots/paint-spike-reduced.png` shows the steady state
`prefers-reduced-motion` draws instead (`page.emulateMedia`).

## Verdict

<!-- owner's verdict goes here -->

Written by Claude Fable 5 (drawn by Claude Sonnet), 2026-09-06.
