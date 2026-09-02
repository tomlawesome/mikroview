# Round 39 — the memory bar gets its slider

Built on round 38 verbatim (`build.py`); only the settings memory group
changes. Draws #796.

Owner, 2026-09-02: *"we should allow the user to set this figure much
higher, if they're willing to sacrifice the RAM for it … let the user
change the maxMemory using the slider in settings."*

## The drawing

- The hours bar is unchanged: it is what is held.
- Under it, a second track is what is allowed: 32 MiB up to what this
  host can spare (3.5 GiB in the story), on a doubling scale so 120 MiB
  sits a quarter of the way along rather than jammed left. The figure
  rides above the handle.
- Dragging proposes; nothing changes until **apply**. While a proposal is
  open: the row on the right reads what the figure buys at today's rate;
  the handle's old place stays as a dotted ghost; a sentence under the
  track says the consequence, with *apply · keep 120 MiB*.
- A shrink is shown on the hours bar itself: the hours that would let go
  dim, and the new oldest time is marked ("09:16 — the oldest that
  64 MiB would keep"). A grow has nothing to show on the bar yet, so the
  sentence says how long the reach takes to fill.
- States are section classes (`#set.mgrow`, `#set.mshrink`), set by the
  capture script.

Not drawn: the setup wizard's copy of the control (#796) — same track,
same sentence, once. A viewer's view of this group is not drawn either;
settings is an admin surface in this drawing.

## Shots

`settings`, `settings-memory`, `settings-memory-grow`,
`settings-memory-shrink`, plus round 38's set unchanged. Looked at each;
no collisions.

## Owner verdicts (2026-09-02)

- *"Ok, yes approved 39"* — accepted as drawn, after asking what
  "dragging only proposes" meant: the handle changes nothing until
  apply, so a shrink cannot lose hours to a slipped mouse.
