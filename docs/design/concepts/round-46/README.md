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
building's click card. `marks.html` now draws direction C only — no
toggle, one page.

### The rules

- Watched (`b.watch > 0`): the building's outline — its plinth's
  silhouette, not the halo — strokes in `--watch`; width
  `min(4, 1.5 + 0.75*(watch-1))` px.
- Flagged (`b.flags > 0`): every face of the building's plinth takes
  `--alarm` as fill, opacity `min(0.9, 0.45 + 0.15*(flags-1))`,
  replacing its district colour; rim stroke `--alarm`, width
  `min(3, 1 + 0.5*(flags-1))`. Unflagged neighbours keep their own
  district colour.
- Both: alarm fill and rim, with the watch outline drawn a touch proud
  of the rim (the plinth radius plus 1.6 units) so both stay visible
  on a building that carries both.
- District plates and name pills are unchanged from round 40 — the red
  house in a green district is the signal, not a second badge.
- The street scene's click card (round 40's `.hovercard` idiom) gains
  a second line on a marked building — cam-porch's reads `⚑ 2 flags ·
  ◉ 1 watch`, ⚑ and its count in `--alarm`, ◉ and its count in
  `--watch`.

### What's on the map

Same hosts as round 40: cam-porch (2 flags, 1 watch — IoT, the alarm
scene's own subject), laptop-anna and tv-lounge (1 flag each, LAN),
doorbell (1 flag, IoT), cam-gate (1 flag, Cams), nas (watch only,
Servers). Added: **pihole** (Servers — plain green, previously
carried no marks) now carries 3 flags, so the opacity step from
cam-porch's 0.6 to pihole's 0.75 is visible on the same page without
touching the 0.9 cap.

### Choices the spec left open

- **What "the building" means for a stamped device symbol**: the
  library draws routers, cameras, pucks and the rest from different
  shapes with no shared outline geometry, so both marks are drawn on
  the plinth (the pedestal every device stands on, already a plain
  three-face box: left, right, top) rather than the small device icon
  on top of it — the icon keeps its district ink, as street furniture,
  not the house.
- **Flag opacity split across the plinth's three faces**: applied
  uniformly rather than keeping the base plinth's left/right/top
  shading ratios — the instruction reads as one opacity value, and a
  flat tint is quieter than inventing a shading ratio the spec never
  gave.
- **How "outside the rim" is drawn**: the watch outline reuses the
  plinth's own diamond shape enlarged by a fixed 1.6-unit margin,
  rather than computing an exact fused outline of plinth-plus-symbol —
  there's no accessible silhouette data for the symbol library to fuse
  against, and the offset diamond reads as one outline in every case
  drawn.
- **Where cam-porch's card lives**: the brief asks for it on "the
  street scene", but cam-porch is IoT's, and round 40's `#street`
  camera is fixed on LAN, so cam-porch is never in its frame (same gap
  the old `capture.mjs` already worked around for its close crop). Its
  card floats free on the opposite side of the scene from
  tom-desktop's, with no leader line, purely to show the idiom's new
  second line.
- **Which host demonstrates 3 flags**: pihole — a plain, previously
  unmarked host in Servers (green), picked to avoid disturbing the
  already-marked cast that demonstrates the 1-flag/2-flag/watch-only
  cases.

## Superseded

Direction A (tinted block + count disc) and direction B (ring +
number) were drawn in this round first, side by side behind a page
toggle. The owner saw both and rejected them the same day: "I changed
my mind completely," and later, "the rings/circles ideas are both
dead." No shots were kept.

## Capture

`capture.mjs`, adapted from round 40's: from `frontend/`,
`node ../docs/design/concepts/round-46/capture.mjs`. It shoots
`#survey`, `#street`, `#alarm` to `shots/paint-<scene>.png`, plus a
420×300 close crop of cam-porch (from `#alarm`, its own scene) to
`shots/paint-cam-porch.png`, and a 260×220 close crop of pihole (from
`#survey`) to `shots/paint-3flags.png`.

## Verdict

<!-- owner's verdict goes here -->

Written by Claude Fable 5 (drawn by Claude Sonnet), 2026-09-06.
