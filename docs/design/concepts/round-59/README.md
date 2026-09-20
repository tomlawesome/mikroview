# Round 59 — the rating and the note in one drawer (#1231, #1232)

2026-09-13. Round 58's ratified rating (three bands) and #1232's notes box
land in the same drawer; the owner asked for them drawn together so one
layout is ratified and #1232 builds from it. `build.py` reads
`round-58/three-bands.html` and writes `combined.html`; nothing above the
note band moves.

## The layout

- **Rating** stays under the sparkline in the right column — it is the
  sparkline's number (round 58, unchanged).
- **Note** is its own full-width band beneath both columns, above the
  buttons: label, a box, one hint line (`kept with the verdict you choose
  next · editable later · goes if the verdict is undone` — the three
  owner rulings on #1232). The box grows as you type; the drawer grows
  with it and the buttons move down. No inner scroll.
- **Read-back**: when the flag returns with a note from last time, it
  renders in full above the box under `you wrote last time · checked
  2 sept`, as prose with a quiet left rule. The box beneath is for this
  time.
- `clear with a note` leaves the drawer: #640 removed it and #1232 says
  the box is not that — you write first, then call it.

## Scenes (`shots/`)

- `combined-spike-empty` — ACTIVITY SPIKE, 72 high, box empty with its
  placeholder.
- `combined-surge-readback` — DROP SURGE, 46 moderate, last time's note
  read back and a two-line draft in the box (the growth on screen).

Only the two scored drawers carry the band in this round; the other flag
drawers are not in its scenes.

## Verdicts

Pending.
