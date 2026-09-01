# Interface visioning — round 31: the watchlist, managed

Under #691 (backend capability with no front-end home), phase 1, item 1.
Round 30's docket carries forward **verbatim**; this round adds the five
controls the watchlist has and round 30 drew nowhere — create, edit,
delete, promote observed places, and the observe-only toggle — all in
`watchlist-managed.html`. Nothing accepted is redrawn.

The rule the placements follow: **the docket has one editing surface,
the drawer.** Nothing opens a panel, a modal or a second page. A watch
is written, mended and removed in the row it lives in.

## What is placed

1. **`+ watch`** sits in the slot the flags tab already uses for
   `clear all` (top right, above the table). Same pill, the watch's own
   ink. It is the tab's one panel-level action; the flags tab's is
   `clear all`.
2. **The draft.** `+ watch` puts a row at the top of the table, open as
   a drawer, state `✎ not watching yet`. The drawer is the form: watch ·
   who · toward · window, and on the right **and it means** — `expect
   it` or `fence it`. The row's cells fill in as you type, so the watch
   is read the way it will be listed before it exists. `start watching`
   or `discard`; either way the draft row is gone.
   - `expect it` = a non-inverted entry (`invert: false`): the pathway
     should happen; the ring breaks when a kept window passes without it.
   - `fence it` = an inverted entry, which the backend always creates
     observing (`definitions.go:635`). `toward` greys out to *wherever it
     goes — it learns, you permit*; the `count broadcast, multicast and
     link-local too` option is `includeStructuralNoise`.
   - `window` is `always` or `between` a time range on chosen days —
     `window.start/end/days`.
3. **A flag writes the draft for you.** Round 30's `watch this pathway`
   and `watch this source` in a flag's drawer switch to the watchlist
   tab and open the draft filled: pathway → `expect it` with who and
   toward (host · :port) from the flag; source → `fence it` with who.
   The operator adds a name, or not, and starts it.
4. **The learning watch** is the state an inverted entry lives in until
   fenced. Its chip is the watch ink, dashed: `◌ learning — 4 places
   seen · 2 permitted`. Its drawer's side column is **where it has
   reached** — every observed destination with its port and count,
   each with `permit` (promote one) or `✓ permitted`; `permit all four`
   promotes the rest at once. `fence now · 2 permitted` is the
   observe-only toggle, named for what it does and carrying the count
   so nobody fences with an empty list by accident. Once fenced the chip
   reads `◉ fencing — 3 places permitted` and the same button reads
   `learn again`.
5. **Edit and remove** end every watch's action row, quiet, pushed to
   the far right. `edit` swaps the drawer's story for the form,
   pre-filled, with `save` and `cancel`. `remove` uses round 28's
   `clear all` gesture: one click arms it red as `confirm — its matches
   stay`, a second click removes, any other click disarms. No modal.
6. **Mend is edit with the fix already typed.** Round 30's `mend — widen
   window` on the broken watch opens the same form with the window
   start moved to cover what was seen (23:30, marked as changed) and a
   **why this mend** note on the right saying which nights and which
   times. Saving turns the chip to `◉ watching — ring mends tonight`.
7. **Paused** is drawn: chip `‖ paused` in the dim ink, and round 30's
   `pause watch` reads `resume watch` in that state.

Two rows join the data story to carry the new states — `tv-lounge
fenced` (learning) and `guest tv casting` (paused). The chrome's eye
count moves from 7 to 9 to match; the audit log gains the entries those
actions leave.

## Deliberately not here

- No confirmation for `start watching` or `save` — both are reversible
  from the same drawer.
- No per-place *remove* from the permitted list: the backend has no
  demote (only `Promote`), so drawing one would be #750's kind of gap.
- Coverage-derived breakage (`ring broken — no logging visible`) keeps
  round 30's broken chip; only the wording differs and that is data.

## Screenshots

`shots/` — captured by `capture.mjs`, viewed and clean:
`watchlist`, `draft-blank`, `draft-fence-window`, `draft-from-flag`,
`learning`, `learning-permit-one`, `fenced`, `broken`, `mend`, `mended`,
`remove-armed`, `paused`.

No new data palette: the watch ink, the alarm ink and the dim ink are
round 30's, so the dataviz validator has nothing new to check.

## Verdicts

Owner, 2026-09-01, on all six placements: *"Approved."* Round 31 is
ratified as drawn and is the surface #691's watchlist build is faithful
to.
