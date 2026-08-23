# Metrics — the ratified design (#488, under #518)

Ratified by the owner 2026-08-23, round 2 (`round-1/` and `round-2/`
beside this file carry the mockups, screenshots and the verbatim
verdict trail; the same trail is on #518). This document is the
consolidated record the build implements from. The mockups are
reference for execution quality; where this text and a mockup detail
disagree, this text wins.

## The model

Metrics is **three views of one data set**, chosen in the page header:

| View | What it is | Default |
|---|---|---|
| **Seismograph** | Horizon strips on one shared time axis, brink at the right | **Yes** |
| **Register** | Vertical ribbons on shared minute-rows, brink at the top | No |
| **Table** | The same hour as sortable, copyable figures | No |

The choice is a **per-user preference**, persisted and applied before
first paint, never changed by the app on its own — the same grammar as
the rail's density states (#486). The cursor's selected minute
survives every view switch. The old overlay charts and the three cards
are gone; the cards' totals (top rules, top talkers, by device) open
the Table view, and the Register carries them as its ledger strip.

## Shared grammar (every view)

- **Identity is the label, colour is meaning.** Two chart inks only:
  traffic blue, refused red (drop · reject — the fall's grammar). No
  cycled hues anywhere; every screen survives greyscale; the Table is
  the identity-without-colour proof.
- **Amber is time**: the brink edge and the cursor. Never a series.
  Alarm red stays reserved for badge/ring semantics.
- **Per-series scale, declared** beside the series name, **floored at
  12/min** so a series that whispered all hour draws as a thread,
  never inflated to look busy.
- **The cursor reads a whole minute at once** across every series, and
  Enter opens the fall at that minute. Keyboard: arrows move a minute,
  Shift ten, Home/End the brink. Each move is announced with the
  minute and its figures.
- **Flags share the traffic coordinate space.** Rates are ribbons or
  traces; episodes are discrete ticks (×N when a minute carried
  several); the silent types keep a labelled hairline each — running
  and quiet, visibly, never twelve empty charts and never hidden. The
  flag-type list is the detector registry (`frontend/src/lib/types.ts`),
  labels abbreviated only where a gutter demands it.
- **Episodes are not open work**: metrics counts episodes raised in
  the window; the rail badge counts open unexcluded flags. Both are
  true; labels say which is which.
- **Sharp**: marks are filled paths on a whole-pixel grid; axes,
  baselines and rules use crisp edges so hairlines land on device
  pixels; redrawn on DPR change, never upscaled from a raster.
  This and the Table peer view are #488's done-when.
- **Reduced motion**: the brink's breath and the paper feed / ribbon
  arrival are the only motion; both become instant.
- **Honesty**: everything shown arrived; nothing was provoked.
  Validated palettes and surfaces are recorded in
  `round-2/README.md`.

## Seismograph (default)

One full-bleed drum, no tiles, no cards, no boxes. Each series is a
horizon strip (~56px): its scale folds into three opacity bands of its
one ink, deepest band carrying the shape. Time runs left→right; the
amber brink is the right edge where the paper feeds; a new minute
pushes the hour leftward. Series name, now-value and declared scale
sit in the left gutter. Flags view: live rows sort to the paper's top
with their ticks; silent rows sink, dimmed, ending in their zero
count. The cursor is a vertical amber line that opens a reading beside
every strip.

## Register

The hour read downward, the way the app already reads: the brink is
the top edge (#363's newest-first), minutes are shared rows, and every
series is a vertical ribbon with a calligraphic smoothed edge — one
instant is one straight line across the page. Column header: name,
now-value, declared scale. TRAFFIC and REFUSED group brackets replace
a legend. The ledger strip below carries the magnitude answers
(honestly not a time series). The cursor is a horizontal amber row
with the minute's cross-section as a column of figures at the right.

## Table

A peer, not a fallback: the same hour and the same totals, sortable
and copyable, refused columns in refused ink, the cursor's minute
highlighted amber and selected across view switches.

## Superseded (considered and closed)

- **Round 1, direction T — the counting house** (killed 2026-08-23):
  small multiples inside the existing shell and cards; conservative by
  construction. Its surviving ideas (two-ink identity, the quiet fold,
  the linked cursor, amber-as-time) are absorbed above.
- **"The cards stay"** (owner decision at filing, reopened at the
  round-1 kill, broken by both ratified directions): the cards'
  answers live in the Table and the Register's ledger.
