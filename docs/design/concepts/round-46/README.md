# Round 46 — flags and watchers marked on the city

Issue: #981. Round 40's `isometric.html` (ratified, #854) gave the city
height as importance, but a flagged host or a watched district looked
like any other until you read heights — the 2D map's ⚑ and ◉ marks had
no local counterpart on a building or a district plate.

## The idea in one paragraph

`marks.html` carries round 40's survey, street and alarm scenes (reach,
walls and estate are dropped — this round is about the mark, not the
rest of the city) with one page-level toggle: **marks: tinted block** /
**marks: ring and number**. Both directions are drawn in the same
markup for every marked building and district; the toggle just sets
`data-marks` on `<body>` and CSS shows one. A building with no open
flag and no watcher carries no mark at all.

## Owner's two ideas

2026-09-06, first:

> No flags, no watchers anywhere. We could just turn the houses/gated
> blocks red/purple/mixed and drop a circle with a number on/near
> them?

A minute later, second:

> Or maybe just a colourised circle outline with a colourised number
> by them would work well.

Both are drawn — the round doesn't pick for the owner.

## Direction A — tinted block

The building's own plinth takes the ink: left face and rim favour an
open flag (alarm), right face favours a watcher; a building with both
shows alarm left, watch right, rim alarm. A filled count disc sits on
the roof — two side by side when a building carries both marks. A
district whose buildings carry marks gets its wall rim in the same ink
at 0.8 opacity, and its name pill gains the counts.

## Direction B — ring and number

The building keeps its own colour. Round 40's existing halo becomes
the mark: a ring in the ink, with the count beside it in the same ink.
A district's plate keeps its own colour too — no rim tint — but its
name pill gains the same counts, ringed in the ink.

## What the numbers mean

A building's flag count is its **open (uncleared) flags**, resolved to
the host the same way `lib/city/importance.ts`'s `watchedImportance`
resolves a flag to a building — via the flags store's own
`extractSourceIp`. Its watch count is its **enabled watchlist entries**
scoped to that host's address. The two are tracked and drawn
separately here (⚑ vs ◉); `watchedImportance` itself folds both into
one weight for the plinth's *watched* height reading — the marks are a
finer-grained view of the same underlying signal, not a new one.

A district's count is the sum of its own buildings' counts — flags
summed, watch summed, independently.

## What's already on the map

Same data as round 40: cam-porch (2 flags, watched — IoT's, and the
alarm scene's own subject), laptop-anna and tv-lounge (1 flag each,
LAN), doorbell (1 flag, IoT), cam-gate (1 flag, Cams). nas (Servers)
already carried `watch: 1` with no flags in round 40's data — it just
had no local mark to show it; round 46 is what makes that watched-only
case ("the purple-only case") visible for the first time, on nas and
also on tom-desktop and wg0, round 40's other watch-only entries.

## Choices the spec left open

- **The gap between a district's plate and its plinth tint's exact
  opacity stack**: unspecified where a building is both dark
  (Cams/Guest-style dim rendering) and marked — the mark's tint is
  drawn at full strength regardless, on the view that a flag should
  read clearly even in an unlit district; the quieter option would
  have been to dim it with the rest of the plinth, but that risks
  hiding the one thing the round exists to surface.
- **Disc/ring gap when a building carries both marks**: not given a
  number: used 2px edge-to-edge for the tinted discs, 3px after the
  first ring's number for the ring direction (matching the one gap the
  spec does give, between the two ring groups).
- **Vertical clearance above a building that already has a label**: a
  name+ip card (street), the alarm-lit callout, or the alarm scene's
  own flag chip. The mark now sits bottom-anchored just clear of
  whichever label is present, growing upward as it scales with zoom,
  rather than centred through it — the spec didn't anticipate the
  collision, but its own instruction ("if a count overlaps a name
  pill, fix the offset") pointed at the fix.
- **The capture's cam-porch close-up**: the brief asks for it "at
  street", but cam-porch is IoT's, and round 40's `#street` camera is
  fixed on LAN — cam-porch is never in its frame. The crop is taken
  from `#alarm` instead, where cam-porch is the scene's own subject.
- **District wall-rim and pill-ring colour when a district mixes**:
  alarm wins (same rule as a building's own rim), so a district with
  both flags and watchers among its buildings reads as alarm-toned
  overall, consistent with "both → mixed... → alarm" in the issue.

## Capture

`capture.mjs`, adapted from round 40's: from `frontend/`,
`node ../docs/design/concepts/round-46/capture.mjs`. For each of
`tint` and `ring`, it clicks the toggle and shoots `#survey`,
`#street`, `#alarm` to `shots/<dir>-<scene>.png`, plus a 420×300 close
crop of cam-porch to `shots/<dir>-cam-porch.png`.

## Verdict

<!-- owner's verdict goes here -->

Written by Claude Fable 5 (drawn by Claude Sonnet), 2026-09-06.
