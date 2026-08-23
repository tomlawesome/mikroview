# Metrics density (#488, under #518) — round 2

A fresh design, not a revision of round 1's T (killed; verdict verbatim
in `../round-1/README.md`). Letters run on: this round is **U** and
**V**. Both directions carry the ratified shell (#486) forward verbatim,
break the reopened "cards stay" decision with the reason stated, keep
the round-1 data story (the hour 13:02–14:02, brink 14:02:11, 7.4
events/s, drop's 13:52 burst at 88/min against a median of 12), and
prove the same three scenes, dark and light. The sixteen flag types in
scene 2 are the real detector registry (`frontend/src/lib/types.ts`).

## Direction U — the register

The hour read **downward**, the way the app already reads: the fall
drops and the brink is the top edge (#363), so metrics stops being a
wall of little left-to-right charts and becomes one instrument in the
same orientation. Every series is a vertical ribbon on shared
minute-rows — one instant is one straight line across the whole page,
so "did accept move when drop burst?" is answered by reading straight
across, not by comparing eight separate axes.

- Per-column scale, declared under each name, floored at 12/min so a
  series that whispered all hour draws as a thread, never inflated.
- The cards become **the ledger**: their answers (which rule, who
  talked, which device) as one strip that owns magnitude and is
  honestly not a time series.
- Flags share the register's coordinate space: rates are ribbons,
  episodes are discrete ticks (×2 when a minute carried two), the
  twelve silent types keep a labelled hairline each — silence visible
  without twelve empty charts.
- Scene 3: one amber cursor row is the whole minute as a column of
  figures; the table is a peer view and the cursor's minute stays
  selected across the switch.

## Direction V — the seismograph

Metrics as a **recording instrument**: no tiles, no cards, no boxes —
one full-bleed drum of horizon strips, every series a needle trace on
shared paper, the brink at the right edge where the paper feeds. Each
strip folds its own scale into three opacity bands of one ink, so seven
series (or sixteen flag types) hold a screen at 56px each with their
shapes intact — the highest-density answer to #488's done-when that
still reads.

- Horizon folding declared per strip ("scale N"), same 12/min floor;
  a steady near-ceiling series draws as the solid uneventful bar it
  truthfully was, and drop's burst saturates its bands.
- Flags: live rows sort to the paper's top with episode ticks; the
  silent twelve sink, dimmed, hairlines with names and zero counts.
- Scene 3: the cursor lifts every needle at once — readings open in
  the gutter beside each strip; table peer as in U, and it is also
  where the old cards' totals now live.

## Shared grammar (both directions)

- Identity is the label, colour is meaning: two chart inks only —
  traffic blue, refused red. No cycled hues anywhere; greyscale-safe;
  the table proves identity without colour.
- Amber is time: the brink edge and the cursor. Never a series.
- Device-pixel-sharp clause and keyboard reach written into scene 3 of
  both, per #488's done-when.

## Palette validation (dataviz six-checks validator)

Chart inks, categorical, `--pairs all`, validated against **round 2's
actual chart surfaces** (the register/drum draw on the frame surface,
not round 1's tile):

- **Dark** (`#3987e5`, `#e66767` on `#06080e`): ALL CHECKS PASS —
  worst-pair CVD ΔE 19.2 (protan), normal-vision ΔE 29.0, both ≥ 3:1
  contrast.
- **Light** (`#2a78d6`, `#e34948` on `#eef1f7`): ALL CHECKS PASS —
  worst-pair CVD ΔE 21.6 (protan), normal-vision ΔE 32.3, both ≥ 3:1
  contrast.

V's horizon bands are opacity steps of a single ink (sequential within
a strip, deepest band carries the shape), not new hues; text wears text
tokens throughout; alarm red stays reserved for badge/ring semantics.

## Verdicts

None yet — awaiting the owner.
