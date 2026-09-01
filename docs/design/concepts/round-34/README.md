# Interface visioning — round 34: verdicts and exclusions

Under #691 (backend capability with no front-end home), phase 2, item 4.
Round 33's docket carries forward **verbatim** and its flags tab gains
what `Flags.svelte:19-22` lists as having no home: the verdict row,
exclusions, and the recently-cleared list. In `verdicts-exclusions.html`.

The rule the placements follow: **a verdict is how a flag is cleared,
and an exclusion is a verdict that outlives the flag.** Nothing opens a
panel, a modal, a sub-tab or a second page; the flag row's layout does
not change (owner, 2026-08-31, #688).

## What is placed

1. **The verdict trio** sits at the right of every flag drawer's action
   row, after round 30's `open in stream ▸ · watch this pathway · clear
   with a note`, under a quiet `call it` label: `expected` · `noise` ·
   `real`. One segmented control, the drawer's own pill grammar.
   `POST /api/flags/{id}/verdict {verdict}`.
2. **Called expected or noise**: the backend clears the flag
   (`store.go:927`), so the row dims where it stands — type ink off,
   caret cell reads `✓ called noise · undo` — and the chrome's ⚑ counts
   down. `undo` is `DELETE /api/flags/verdict/{id}` and puts the row back
   exactly. Dimmed rows stay until you leave the tab; that is the
   recently-cleared list, in place rather than elsewhere. `clear with a
   note` dims the same way, reading `✓ cleared`.
3. **Called real** keeps the flag open (the backend leaves it), so the
   row says so where the type is — `✱ UNPLANNED  REAL · YOU` — and the
   story leads with *Called real at 13:58 by you. It stays open until it
   is cleared; a fresh episode asks again* (`store.go:640`). Clicking
   `real` again takes it back.
4. **`never again`** is the quiet verb after the trio, and uses round
   28's arm-then-confirm: one click reads `confirm — REPEATED DROPS never
   fires again for cam-porch · tcp/445` in alarm ink; a second is
   `POST /api/flags/{id}/clear-permanent`. The row dims as `✓ never again
   · listed below` — no undo on the row, because the pair now lives in
   the list below and is undone from there.
5. **The exclusions body** is a second `<tbody>` under the flags, under
   the same quiet heading round 33 uses: *never again · 2 pairs mikroview
   no longer flags* with a `show them` pill; hidden until asked for.
   `GET /api/flags/exclusions` → `{id, type, target}`. Each row is in the
   flag row grammar with the type's ink off — mikroview does not look at
   these — and reads `never again — since Mon, tom`. Its drawer tells
   why in prose, lists *the pair* (flag · target · since · by), and has
   one verb: `let it fire again` → `DELETE /api/flags/exclusions/{id}`.
   Sort and filter leave this body alone.

## Deliberately not here

- No campaign grouping and no density picker: neither has a backend
  field, and round 30's table has one density.
- No reputation or evidence panel, no per-flag abuse check: the port
  scan's story already carries the reputation snapshot the flag was
  raised with (*known scanner ranges say nothing about this address*),
  which is the ratified form. `GET /api/lookup/ip/{ip}` on demand is a
  stream or entity round's question.
- No `confidence` figure: it is a detector fact and belongs with the
  detector in the engine room, not on the flag row.
- No note on a verdict: the API takes none. `clear with a note` stays
  the way to leave one.

## Gaps the drawing exposes

- A verdict writes **no audit entry** (`handleFlagsVerdict`,
  `internal/api/flags.go:96`, unlike clear and clear-all). The drawing
  says *the audit log has this line*; recorded on #750.
- Exclusions carry no `since` or `by` (`Exclusion{ID, Type, Target}`,
  `store.go:362`). The row and drawer draw both; recorded on #750.

## Screenshots

`shots/` — captured by `capture.mjs`, viewed and clean: `flags`,
`called-noise`, `called-expected`, `undone`, `called-real`,
`never-again-armed`, `never-again-done`, `exclusion-drawer`,
`let-it-fire-again`.

No new data palette: the trio and `never again` are round 30's pills,
the armed state is round 28's alarm ink, the dimmed row is round 33's
set-aside opacity, and the exclusions body's ink-off type is `ink-3` —
the dataviz validator has nothing new to check.

## Verdicts

2026-09-01, owner: "Yeah love this, good job."

Same day, on reflection: "These buttons need to be elegantly worked into
the row before the drawer opens … you have to open the drawer to select
an answer - it's unnecessary clicks." Round 35 moves the trio into the
row; everything else here stands.
