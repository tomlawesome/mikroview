# Interface visioning — round 36 (the chrome sweep)

`the-whole.html` is round 30 verbatim plus a home for every unmounted
caption, note and control on #691 item 6 that carries **information or
an ability**. Nothing already liked is redrawn. Built by
`/tmp/r36-build.py`-style substitution on round 30's file; shots by
`cd frontend && node ../docs/design/concepts/round-36/capture.mjs`.

The rule the round follows: a statement the software can truthfully
make about what it is showing is interface (#445, round 3); an
explanation of how to read a display is documentation (round 5, round
30 §5). Only the first kind gets drawn.

## What is new, by scene

### The fall (`#s2`) — three statements, item 6.1

- **The window cap** is a third chip in the fall's head, dim, present
  only when the window holds more than the fall was given: *○ the most
  recent 5 000 events — this window holds more*. The range itself is
  already the time gutter; "newest at the top" is documentation.
- **An empty band says so.** `wg1 → bridge1` is "quiet by choice" with a
  watch holding, so it is empty here — round 30's one faint dash is
  gone (the only data change this round) — and the band states it on a
  plate in the quiet ink: *nothing in these 15 m · logged — quiet, not
  dark*. Ink-3, never the dark red: quiet is a fact, not a fault.
- **The quieter count** sits beneath the household band's port labels,
  *+6 quieter ▸*, and is the way into the stream for that boundary.

### The topography (`#s3`) — the degraded note, item 6.4

A state (`#s3.degraded`), not furniture; the resting map is untouched.
When no `/ip address` table has been pushed: the router card carries
the one statement, where its other pushed-table facts live — *no
address table pushed — zones from boundaries · Run setup… ▸ adds it* —
and every address slot holds what it truly holds: zone cards read *from
boundaries* in place of a CIDR, the WAN and WireGuard cards *no address
pushed*. No note floats over the drawing.

### Metrics (`#s4`) — the cross-section and the ledger, items 6.9–6.10

- **The cross-section is the hourline.** Its job — read the minute under
  the cursor across every series — was already the hourline's; it now
  reads every series: *13:52 · 52 accepted · 9 refused · 6 natted · 2
  flag episodes — unplanned · ring broken*. No aside is drawn, and its
  "Pick a minute…" instruction stays struck.
- **The ledger is the table view's foot.** The register keeps time; the
  table owns magnitude. Beneath the minutes: top rules, top talkers, by
  device, by protocol, the hour by action, episodes by flag type —
  bars, no boxes, in the inks the table already uses. The built app
  mounts `MetricsTotals` there already; the drawing now says so.

### The stream (`#s5`) — the hand, items 6.8 and 6.13, and #749

- **The whisper's line carries the verbs.** The whisper commands the
  stream and its seek is what stops the lines following, so the
  controls sit right of its facts: `following · pause · group` in the
  spans' segmented idiom and `wipe` as a quiet pill. The stat keeps to
  facts — *34/s now · drops 26% · top talker cam-porch · ring holds
  41 m* (the old "buffer %", said as reach); *autoscroll on* leaves the
  prose and becomes the `following` verb.
- **Following is two-way.** A seek or a fence turns it off and the pill
  reads `follow` in the now ink until taken; clicking it follows again,
  clears the cursor and the window. That is #749's defect, drawn shut.
- `paused` holds the lines and the stat counts what waits; `group`
  folds repeats of the same line into the first with `×n`; `wipe`
  empties the lines held here and the table says so: *nothing since
  14:02:11 — wiped here, by you · the server's ring still holds every
  line*.
- **Column resize draws nothing at rest.** The boundary reveals itself
  under the hand — a hair on the header's edge and a `col-resize`
  cursor — and is otherwise invisible.
- Max-age is not here: the span pills already are it (#703).

## Not drawn, by decision

Explanations go to the docs, not the chrome (round 5; round 30 §5):
the audit log's intro, the watchlist's heading and intro, the second
"Filters ▸" trigger (the box is always on screen). The stream's foot
band is left exactly as round 30 has it, pending the owner's answer on
#717's *"I hate it, remove it"* (#691, question 47).

Surfaces with no home at all — entities' rules and ports tabs, the
read-only viewer declaration, filter presets, CSV export, the device
status strip and uptime badge — are round 37's.

## Validation

- No new colours: quiet-t is `--ink-3`; the ledger's inks are the
  table's own (`--accent`, `--lan`, `--fall-accept`, `--fall-drop`,
  `--nat`, `--alarm`).
- Twelve shots in `shots/`, each looked at: the router card's note
  overran its card on the first pass (now two lines, taller card), the
  ledger's one-router line truncated (fixed), the held state was too
  quiet to notice (now in the now ink).
- `prefers-reduced-motion`: no new motion this round.
- No apparatus: the degraded state is set by the capture script, not by
  anything on the page.

## Owner verdicts (2026-09-02)

- On the round: *"Love the ledger but put them at the top not beneath.
  Rest is great."* Accepted as drawn, with one change for round 37:
  the ledger sits above the table view's minutes, not beneath them.
- On the fall's band header `WATCH HOLDING ✓` (round 30's wording):
  *"I don't like the term 'watch holding' with the tick. Lose the tick,
  and suggest a better term"*, then, choosing from the options: *"Yes,
  watched in green says everything we need, and then watch broken"*.
  So the healthy state reads `WATCHED` in the accept ink, no tick; a
  broken watch reads `WATCH BROKEN` on the same line. Round 37 carries
  it; the built `Fall.svelte` prints the old text and needs the same
  change (follow-up issue).
- The stream's foot band (#717's *"remove it"*) stays open: the owner
  asked for more detail before deciding.
