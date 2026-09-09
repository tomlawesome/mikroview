# Settings surfaces (#490) — round 8: the bar, plainer (#829)

## Why

Round 7 was the first round not rejected: *"this is much closer but the
bar looks cluttered and complicated when we're trying to give 'clean and
simple' vibes. Right idea, slightly wrong execution. But MUCH better than
previous rounds."* So round 8 is round 7 with one thing redrawn — the bar
— and nothing else touched. The drawer, the family picker, the gradient
rule, the counting line, the says line, the receipt and the pills are
carried forward verbatim.

The reference is still GitLab's filter bar: tokens of field, verb and
value, typed or picked, a list under the bar that narrows as you type.
What comes off is the decoration round 7 put on it.

## What changed in the bar, and only there

| Round 7 | Round 8 |
|---|---|
| A token was three joined segments, each on its own tint of the value's ink | One quiet pill per token on `--bg-hover`, no segments |
| The value was a badge in its own box (`ActionBadge` for DROP) | The value is bold text in the ink the app gives it — address ink, DROP's ink, accept green, accent — with nothing drawn around it |
| `and` between every token | A gap, as in GitLab's own bar |
| A right-aligned hint line inside the bar | Gone; the list says what a line can be |
| A `field › verb › value` breadcrumb over the list and a note under it | Gone; the token being built shows the step |
| The bar's accent edge over the list's own hairline edge — two boxes | One box; the list opens inside it under one hairline |
| `destination address is classed as internal` | `destination classed as internal` — the columns' short nouns, and the verb without its `is` where it can drop it |
| An amber rail and edge on the unfinished token | Only the unfinished value is amber, with its caret |

Everything the value inks meant in round 7 still holds; they are now
text, not boxes.

## Scenes

| Scene | Shows |
|---|---|
| s1 | Port scan (copy) just cloned — one token, `state one of new`, and the placeholder |
| s2 | Garage probes mid-build — four tokens, the port one unfinished with `1-` typed in amber, the list open inside the box with two lines |
| s3 | The same bar at step one — `dst` typed, the field list down to `destination` and `dst port` |
| s4 | Complete and tried — the receipt, save live; the same detector to a viewer (no box, no `×`); and at 390 px |

Data story unchanged since round 5: the garage range `10.0.70.0/24` and
its two stragglers, `10.0.70.14 garage-cam` and `10.0.70.31 ev-charger`.

No new colour is minted — the bar uses `--bg-hover`, `--bg-elevated`,
the text tokens, `--accent`, `--now` and the value inks round 7 already
used — so the dataviz validator has nothing new to check.

## For the build

Unchanged from round 7 (family field on custom definitions, the
dropdown's keyboard model, shipped conditions exposed for clone, verbs
per field, placeholders per key mode), plus:

- **Field names are the columns' short nouns**: `source`, `destination`,
  `port`, `src port`, `action`, `chain`, `proto`, `state`, `in iface`,
  `out iface`, `time`, `day`, `rule`, `list`, `identity` — one per
  `engine.Field`. Each row in the field list carries the ink its value
  will wear.
- **Verbs drop their `is` where the word stands alone**: `within`,
  `between`, `one of`, `classed as`; `is` and `is not` keep it.
- **`--bg-hover` is the pill's only wash.** Hovering a token does not
  change it; hovering its `×` brightens the `×`.

## Verdicts

Owner, 2026-09-09: *"Editor looks great. Round 8 approved."*

→ Round 8 is the ratified design for #829's conditions editor. The
build follows this file and its "For the build" list, plus round 7's.
