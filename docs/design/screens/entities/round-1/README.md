# Entities (#489, under #518) — round 1

One direction this round (letters run on; U and V were metrics
round 2).

## Direction W — the census

Entities is the network's dictionary — where raw keys the traffic
already spoke (IPs, rule labels, ports) get their human names. The
current page stacks four lists (Named entities, then Discovered
rules / hosts / ports), so one thing lives in two places and naming
teleports it between them. The census dissolves the split:

- **Three tabs by type — Hosts · Rules · Ports** (the house tablist,
  arrow keys), quiet outlined counts, never alarm-filled. Type is the
  stable identity; named/unnamed is a passing state, so it is a fold
  within the tab (unnamed first, the census's to-do list), never a
  place of its own.
- **Naming is a row state**: inline field, Enter saves, Esc cancels,
  the row keeps its place and focus and settles into the named fold
  without a scroll jump. One row edits at a time; tags optional at
  christening.
- **Filter + lens**: one filter field reading key, name and tags; an
  "Unnamed only" lens that composes with it; a live match count; `/`
  focuses the filter. Filtered-empty names both knobs and offers to
  drop either.
- **The census meets the hour**: last-seen times, event counts and
  first-seen facts come from ingested traffic (203.0.113.44 wears its
  13:52 burst; r7 its 88 hits). Nothing probes or resolves — every
  fact arrived on its own.
- **Viewer (#490 grammar)**: READ-ONLY — ADMINS EDIT declared once in
  the header chip; Name it / Edit / Remove / Add entity absent, never
  disabled; the roll itself unchanged.

Scenes, dark and light:

1. **The page** — Hosts tab, the two folds, filter/lens/add, amber
   seen-ticks.
2. **Naming in place, and the Rules tab** — the editing row state,
   and the same roll grammar carrying rules (keys verbatim from
   RouterOS, hit counts as their "last seen").
3. **The viewer, and the lens** — the #490 read-only census, and a
   composed filtered-empty that names both halves and offers a door
   out of each.

## Palette

No data palette on this page — there are no charts. Colour stays
meaning-only per the house grammar: accent for interactive, amber
solely as the seen-within-the-hour tick (time's colour), alarm
appearing nowhere (an unnamed entity is work waiting, not an
emergency). Identity is text — the key, the label, the type tab — so
the dataviz categorical validator does not apply; text wears text
tokens throughout.

## Verdicts

None yet — awaiting the owner.
