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
   own column head `CALL IT`: `expected · noise · real` as three quiet
   verbs in the row's mono at `ink-3`, dot-separated the way the chrome
   is. No boxes — the docket speaks in text and dotted underlines, and
   three boxed pills a row were the heaviest thing on the page. The
   row's hover lifts them to `ink-2`; a verb's own hover takes the
   accent and the where-link's dotted underline; the chosen one keeps
   the accent with a small filled dot after it. No filter under this
   head: verdicts are given, not searched. A click on a verb is a
   verdict and never toggles the drawer (`closest('a, button')` in the
   row's click guard). `POST /api/flags/{id}/verdict {verdict}`.
2. **Called expected or noise** dims the row where it stands, and the
   cell the trio occupied now reads `✓ called noise · undo` — the
   column never moves. `undo` puts the trio back.
3. **Called real** lights the `real` pill, marks the type
   `✱ UNPLANNED  REAL · YOU`, and the story leads with *Called real at
   13:58 by you…* whether or not the drawer is open.
4. **`never again`** stays in the drawer, now alone at the right of the
   action row: it is the one verdict that wants a second look and a
   confirm, and the drawer is where the evidence is. Its arm-then-
   confirm, the exclusions body and `let it fire again` are round 34's,
   unchanged.

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

No new data palette: the verbs are the row's own mono in `ink-3`,
`ink-2` and the accent — the dataviz validator has nothing new to
check.

## Verdicts

2026-09-01, owner, on the first cut (three bordered pills a row): "Could
we make this more aesthetically pleasing in keeping with Mikroview's new
UI?" — reworked in place to the quiet text verbs above, before any
acceptance.
