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

### The rules

- Flagged (`b.flags > 0`): the device symbol itself — every shape it is
  built from — takes `--alarm` as fill, opacity
  `min(0.9, 0.45 + 0.15*(flags-1))`, stamped over its ordinary
  district-ink self; its outline (see **Choices**, below) strokes in
  the same ink, width `min(3, 1 + 0.5*(flags-1))`. Unflagged neighbours
  keep their own district colour untouched. The plinth underneath is
  gone — there is no plinth, and nothing marks it.
- Watched (`b.watch > 0`): the device symbol's own outline strokes in
  `--watch`, width `min(4, 1.5 + 0.75*(watch-1))`, scaled up 1.09× from
  the device's own centre so it reads as a touch proud of the flag rim
  on a building that carries both.
- Activity spike (`b.spike`): a flagged building whose open flag is an
  activity spike also pulses — its rim's opacity breathes 0.6→1→0.6 and
  a soft glow alongside it breathes too, both on a 2s ease-in-out loop.
  The red fill itself does not animate; only the rim and the glow do.
  Under `prefers-reduced-motion`, the pulse is dropped for a steady rim
  and glow held at the bright end, not fully switched off — the mark
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
touching the 0.9 cap. cam-porch and pihole also each carry the
activity-spike pulse (see **Choices** for why these two).

### Choices the spec left open

- **How a multi-shape symbol's outline is drawn**: the library builds
  each symbol from several boxes and discs (a workstation is a tower
  plus a monitor; a camera is a post, an arm and a body) with no
  boolean union available to fuse them into one path. Both the flag rim
  and the watch outline stroke *every* constituent shape the symbol is
  actually built from instead — where two are adjacent (the laptop's
  base and lid, the camera's post/arm/body) the shared seam gets a
  double line, but the assembly reads as one outline in every case
  drawn. `outlineParts()` in `marks.html` does this generically from
  the same box/disc objects the visible symbol is stamped from, so it
  never drifts out of sync with the artwork.
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
- **The watch outline's "a touch proud"**: scaled 1.09× around the
  device's own centre, rather than the plinth version's fixed 1.6-unit
  margin — device symbols vary far more in local size than the plinths
  did (a puck and a router chassis are not close), so a proportional
  offset holds up across all of them where a fixed one would not.
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
