# Round 58 — the confidence rating moves into the drawer (#1231)

Owner, 2026-09-13, on the built flags tab: the `72` beside `▲ ACTIVITY
SPIKE` "looks like an event count." Direction: out of the row, into the
drawer, say it is a confidence rating, use colour. Round 47's
`campaigns.html` is the base and `build.py` copies it verbatim; the row
loses its number and the story loses its "Scored N." sentence, nothing
else moves.

## The drawing

The drawer's side column, under the sparkline it already had, gains one
block:

- `CONFIDENCE` label, left; `72 HIGH` right — the number bold, the band
  word beside it, both in the band's colour.
- A 4px meter to 72 of 100 in the same colour.
- One quiet line: *how far this hour sits from cam-porch's own usual ×
  how much history backs that (14 days here). The detector's number, not
  a verdict.* — the two factors `emaConfidence` multiplies
  (`internal/engine/baseline.go`), in the words round 47 used.

Bands are the drawing's, not the engine's: low 0–39, moderate 40–69,
high 70–100.

Two directions, differing only in colour:

| File | Colour | Why |
|---|---|---|
| `one-hue.html` | one blue, three lightness steps — `#5c6f9c` / `#9db8e8` / `#e2ecff` | confidence is a magnitude, so a sequential ramp; distinct from every warm type ink and from the status greens/reds |
| `three-bands.html` | cool / amber / hot — `#7f93bd` / `#f5a623` / `#ff5470` | reads as a rating at a glance; the hot step is the alarm ink, which already appears in the same drawer as a type ink |

`bands.html` shows the three bands side by side for both, at 18 / 46 /
72.

**One data change.** DROP SURGE scores 46 instead of 71, so a moderate
band appears in a real drawer; its story gains the clause that explains
it — *this boundary is volatile, so 3.1× clears the line without being
far outside its own spread* — which is what a moderate score means.

## Palette check

Validator, dark, surface `#06080e`. Both sets pass CVD separation,
the normal-vision floor and contrast ≥ 3:1; the lightness-band and
chroma-floor checks are categorical rules and fail by design here — the
one-hue set is a sequential ramp (lightness monotonic 0.55 → 0.78 →
0.94), and the three-bands set is a status-style palette where the
number and the word always travel with the colour.

```
one-hue      CVD worst adjacent ΔE 15.9 (protan) · normal 16.9 · contrast all ≥ 3:1
three-bands  CVD worst adjacent ΔE 12.8 (deutan) · normal 20.6 · contrast all ≥ 3:1
```

## Scenes (`shots/`)

- `<dir>-flags` — resting; every row carries its type alone.
- `<dir>-spike-high` — ACTIVITY SPIKE's drawer, 72 high.
- `<dir>-surge-moderate` — DROP SURGE's drawer, 46 moderate.
- `bands` — the three bands, both palettes.

## Verdicts

Pending.
