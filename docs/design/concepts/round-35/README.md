# Interface visioning — round 35: verdicts in the row

Under #691 (backend capability with no front-end home), phase 2, item 4.
Round 34's flags tab carries forward **verbatim** except for one move,
asked for by the owner on viewing it: the verdict trio leaves the
drawer and sits **in the flag row itself**, so a call costs one click
and never needs the drawer opened. In `verdicts-in-row.html`.

Round 34's rule holds — **a verdict is how a flag is cleared, and an
exclusion is a verdict that outlives the flag** — and gains a corollary:
**a verdict is a row's verb, not a drawer's.**

## What moved

1. **The trio is the row's last cell**, before the caret, under its
   own column head `CALL IT`: three glyph chips, each in its verdict's
   own ink — `✓ expected` in the ok green, `~ noise` in the amber the
   docket uses for *now*, `✱ real` in the alarm ink the flag mark wears.
   Hover fills a chip with a wash of its colour and lifts it a pixel.
   No filter under this head: verdicts are given, not searched. A click
   on a chip is a verdict and never toggles the drawer
   (`closest('a, button')` in the row's click guard).
   `POST /api/flags/{id}/verdict {verdict}`.
2. **A verdict lands as a stamp.** This is a docket, so a call is
   pressed onto it: the chip's word becomes a small rubber stamp
   in the same ink — `NOISE`, `EXPECTED` — square, thumping in at twice its size
   (`stamp-in`, 280 ms, overshoot) while the row flashes a wash of that
   colour and settles dim. The stamp takes the trio's place in the same
   cell, with `undo` beside it, so the columns never move. `undo` puts
   the trio back. `clear with a note` stamps `CLEARED` in the quiet ink.
3. **Called real** stamps `REAL` in the same column, in alarm ink,
   with `undo` beside it and the caret still there — the flag stays
   open, the row keeps its ink, and its own colour bar swells from 3 px
   to 7 px as a lens whose ends arc back into the bar, so there is no
   stepped edge against the rows above and below. The story leads with
   *Called real at 13:58 by you…*. To call it something else later:
   `undo`, then the chip. One rule: every stamp lives in `CALL IT`.
4. **`never again`** stays in the drawer, now alone at the right of the
   action row: it is the one verdict that wants a second look and a
   confirm, and the drawer is where the evidence is. Its arm-then-
   confirm is round 34's; done, it stamps `NEVER AGAIN` in the quiet
   ink and the pair appears in the exclusions body below, which with
   `let it fire again` is unchanged.

Nothing else changes: the row's columns keep their order, the drawer's
`open in stream ▸ · watch this pathway · clear with a note` are round
30's, and the rules under "Deliberately not here" and "Gaps" in
round 34's README still apply.

## Screenshots

`shots/` — captured by `capture.mjs`, viewed and clean: `flags`,
`called-noise`, `called-expected`, `undone`, `called-real`,
`never-again-armed`, `never-again-done`, `exclusion-drawer`,
`let-it-fire-again`. Calls in `called-noise` and `called-expected` are
made with the drawer closed.

Palette: the three verdict inks are existing tokens — `--ok`, `--now`,
`--alarm` — used here for the first time as a trio side by side; the
dataviz validator has nothing new to check, but the build should keep
the three as tokens, not literals. `prefers-reduced-motion` turns off
the stamp, the flash and the hover lift.

## Verdicts

2026-09-01, owner, on the first cut (three bordered pills a row): "Could
we make this more aesthetically pleasing in keeping with Mikroview's new
UI?" — reworked in place to quiet dot-separated text verbs. Owner:
"Very bland... was hoping for something cooler." — reworked again to
the glyph chips and stamps above, before any acceptance. Owner: "I like
it, except for the offset/angled nature. It is kind of cool, but it's
also a bit cheesey." — the stamps now sit square; the thump stays. Owner: "id prefer it if
the bar were thickned by using an arc, so we don't get a jutted edge" —
the real row's bar now swells as a lens.

2026-09-01, owner: "33. stamps are good." On where `real`'s stamp lives:
"36. your recommendation" — it moved into `CALL IT`; owner then asked why `retract` rather than `undo` — same word now, same call.

2026-09-01, owner: "38. approved."
