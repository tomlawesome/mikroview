# Round 47 — the flags tab: campaigns, the scored number, flags by type

Issue: #988 (owner rulings of 2026-09-06). Round 35's
`verdicts-in-row.html` is the base — round 34's table with the trio in
the row, as approved 2026-09-01 — and `build.py` copies it verbatim and
adds the three things ruled on. Nothing already liked is redrawn: every
round 34/35 row, drawer, stamp, exclusion and filter is carried as it
was.

One data change. A seventh open flag, `▲ ACTIVITY SPIKE 72` on cam-porch
at 11:10, two hours before the rest. It is there to prove the campaign
rule's other half (same source, outside the window, so its own row) and
to carry the issue's own confidence example. The chrome's `⚑ 6` becomes
`⚑ 7`.

## The drawing (`campaigns.html#s7`, flags tab)

**1. Campaign rows.** A campaign is the flags one source IP raised
inside one 30-minute window: each flag within 30 minutes of the last,
same source, regardless of type. cam-porch's UNPLANNED, OUTBOUND and
REPEATED DROPS (13:28 → still arriving) fold into one row:

- FLAG: `⁂ CAMPAIGN · 3 flags`, in the worst flag's ink (alarm).
- WHERE: the source — `cam-porch · 10.0.20.14`.
- EVIDENCE: one word per type inside, each in its own type ink, then
  the span (`13:28 → still arriving`).
- COUNT: the sum (`26×`). AGE: from the oldest flag (`24 m`). The row
  sits where its newest flag would.
- CALL IT: the round 35 trio. A call on the campaign row calls every
  flag inside it — the stamp reads `NOISE · all 3 · undo`, and undo
  undoes all three. Opened, each member still carries its own trio, so
  one flag can be called differently.
- Click the row and it opens to its flags, one step in with a dash,
  each the round 34 row and drawer verbatim; a quiet line under the
  campaign row says why they are one: *one source, three flags, each
  inside 30 minutes of the last — one campaign. cam-porch's ACTIVITY
  SPIKE at 11:10 is two hours from these, so it keeps its own row.*
- Filtering: a campaign shows if it or any flag inside matches; when
  only members match it opens to just those (see `filtered-by-type`).
- The FLAG column is pinned at its resting width, so opening a
  campaign never moves WHERE or EVIDENCE.

**2. The scored number.** Beside the type, only where a detector
scored the flag: `▲ DROP SURGE 71`, `▲ ACTIVITY SPIKE 72` — bold, full
ink, in a hairline pill (first cut was dimmed; owner, 2026-09-06: "the
number needs to be much more visible"). The
other five carry nothing — no dash, no band, no word. In the drawer one
line under the story says where it came from: *Scored 71. How far this
hour sits from the wan boundary's usual, and how much history backs
that — 14 days here. The detector's number, not a verdict.* The span
line ends `· scored 71`. Scored flags are the baseline family only
(`emaConfidence`, `internal/engine/baseline.go`): activity spike, rule
spike, low-and-slow scan, off hours, global spike.

**3. Flags by type.** A strip across the table's full width, above the
column heads: `BY TYPE · 7 OPEN`, then one cell per open type — mark,
type name in its ink, the count, and a thin bar of count against the
largest type. Click a cell and the table filters to that type (the
FLAG filter reads the type; the others dim); click again to clear.
Types with nothing open have no cell; the strip recounts as calls are
made (`campaign-called-noise`: four cells) and disappears at zero.

## Amendments to the issue's design, and why

- **No stacked bar.** The issue's proposal had one bar in type inks.
  Round 30's ratified type inks are a warm family that only the label
  tells apart, so adjacent segments cannot be read by colour. Validator,
  dark, surface `#06080e`, the six type inks:

  ```
  [FAIL] Lightness band         outside band: #ff5470 0.686 · #ff9e64 0.787 · #f072c8 0.727 · #e0765a 0.681 · #e8b05a 0.792 · #b8c56a 0.794
  [PASS] Chroma floor           all 6 >= 0.1
  [FAIL] CVD separation         worst adjacent #b8c56a↔#e8b05a ΔE 1.9 (deutan) · tritan 5.4
  [FAIL] Normal-vision floor    worst adjacent #b8c56a↔#e8b05a ΔE 8.1 (normal) — below 15
  [PASS] Contrast vs surface    all 6 >= 3:1
  ```

  A normal-vision floor below 15 is a hard fail with no secondary
  encoding allowed, so the strip is one labelled cell per type: identity
  by label, the ink decorative, the bar a single-hue magnitude. Nothing
  is decoded by colour alone.
- **Counts open flags, not this hour's episodes.** The engine room's
  by-type sum (`EngineRoom.svelte`) counts episodes this hour; here the
  strip counts what the table holds, because a click filters the table
  and the two must agree.
- **Campaign age and position.** Not specified on the issue; drawn as
  age from the oldest flag, position by the newest — the campaign is as
  old as its first flag and as current as its last.

## Held, not drawn

The drawer's reputation snapshot: one click away via the IP popover
(owner, 2026-09-06). Not designed back in; nothing in this round makes
that route longer.

## Scenes

| Scene | Get there | What it shows |
|---|---|---|
| Resting | `campaigns.html#s7` → flags | strip, campaign collapsed, seven open flags in five rows |
| Campaign open | click the campaign row, then UNPLANNED | members one step in, the rule line, the round 34 drawer |
| Scored | click ACTIVITY SPIKE | `72` beside the type; *Scored 72.* in the drawer |
| Filtered by type | click `repeated drops` in the strip | strip dims to one cell, campaign opens to its one match |
| Campaign called noise | click `noise` on the campaign row | `NOISE · all 3 · undo`; strip reads `4 open` |
| Undone | click `undo` on the campaign row | all three restored, strip back to seven |

## Screenshots

`shots/` — captured by `capture.mjs` at 1600×1000, viewed and clean:
`flags`, `campaign-open`, `scored`, `filtered-by-type`,
`campaign-called-noise`, `campaign-called-noise-open`,
`campaign-undone`. `prefers-reduced-motion` turns off the bar and cell
transitions; round 35's reductions still apply.

## Verdicts

2026-09-06, owner, in chat on the first cut: the row colour line broke
at the campaign's rule line — fixed the same day. "Scored: the number
needs to be much more visible" — the dimmed number became the pill
above, same day. Then: "Everything else is great." Asked whether that
ratifies round 47 with the number as now drawn: "yes it does."
**Ratified 2026-09-06.** Then, same day: "the numbers still need to
be brighter" — the number went to pure white at 13px on a stronger
pill; then "I don't want a pill, just the number" — the pill is gone and
the bare white number sits beside the type (`.fmark .conf`).
