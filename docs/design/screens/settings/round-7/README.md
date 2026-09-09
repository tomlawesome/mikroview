# Settings surfaces (#490) — round 7: the builder bar in mikroview's inks (#829)

## Why

Rounds 4, 5 and 6 were all rejected, and all three for the same thing:
*"Generic boxes/cards on the flat blueish background. Zero use of colour
anywhere in connection to the entirety of the rest of the UI. Pretty much
the entire feel. Mikroview works hard to break away from this in almost
every part of it. The one part it doesn't is the engine room and you've
chosen to copy that."* The owner also named the screen to aim at — the
docket, *"though for a very different purpose"* — and repeated the
instruction the earlier rounds had walked away from: *"I explicitly asked
for a GitLab style builder/bar. The behaviour should be very similar but
the style should be uniquely Mikroview."* So round 7 keeps only round 6's
sentence grammar, its engine facts and its data story, and throws away
every layout decision the three rejected rounds made.

## The rules this round proposes

- **A detector row opens like a flag on the docket.** The family ink is
  the row's left rail and the drawer's left rail, one unbroken line, and
  the drawer is open space — no card, no panel, no border around it.
- **Filing the detector under a flag family is the first thing you do,
  and it colours everything.** `lib/flagPalette.ts`'s seven inks are the
  picker: seven glyphs in their own inks beside the name, the chosen one
  solid and the rest at 34%. The choice then runs through the rail, the
  glyph, the name, the docket's `.btbar` gradient rule under it, the
  `{Count}` stamps, the tick strip and the try/save glyphs.
- **One bar, GitLab's behaviour, the stream's clothes.** It is
  `FilterBar.svelte`'s `.fbox` — `--bg-elevated`, one hairline, 7px, mono
  placeholder, `--accent` and a soft ring while you are in it. It is the
  only boxed input on the whole surface.
- **A token is three joined segments — field, verb, value — and the value
  wears the ink the app already gives that thing.** `action is DROP` ends
  in `ActionBadge.svelte`'s DROP badge, verbatim; an address is the
  stream's address ink; a port is `--accent`; `internal` is `--accept`;
  a time of day is `--now`; interfaces and chains are `--fg-muted`. Field
  and verb sit in `--fg-dim` on a 9% wash of the same ink, so the token
  reads as one coloured object rather than three chips.
- **The dropdown is not a card.** Rows of text on `--bg-elevated`, hung
  directly under the bar with one `--hair-2` edge closing the bottom, a
  `.lab` breadcrumb of the three steps across its top. Field rows carry
  the ink their values will wear. Typing narrows; a typed value goes on
  one mono line inside the bar with the caret and its hint at the right.
- **No group headings, no labelled form fields, no bordered panel.**
  Every label on the surface is the docket's `.lab` (600 9px mono,
  0.14em, uppercase, `--fg-dim`). The counting numbers keep the bench's
  own dashed click-to-edit; try and save are the docket's `.v` pills;
  clone and remove are `.olink` text.
- **Unfinished is `--now`, never `--alarm`.** The waiting token takes an
  amber left rail and edge, its value trails an amber caret, and one
  amber line beside the two greyed pills says which line to finish.
- **Trying is a receipt, where THE EPISODE sits.** `TRIED · 24 H`, how
  many times it would have fired, the same SVG tick strip in the family
  ink, and the hosts it caught in mono.

## Scenes

| Scene | Shows |
|---|---|
| s1 | Port scan (copy) just cloned — the copied-from line, filed as SCAN, one `connection state is one of new` token, 20 ports / 60 s, not tried yet |
| s2 | Garage probes mid-build — four tokens, the port one unfinished with an amber edge and `1-` typed, the dropdown open at **value** with its hint, `finish the port line` beside the waiting pills |
| s3 | The same bar at **field** with `dst` typed, the list down to destination address / destination port, first highlighted, each row in its own ink |
| s4 | Complete and tried — the receipt and its ticks, save live; below the bench the same detector as a viewer sees it (no `×`, no ring, no pills) and at 390 px |

Data story unchanged since round 5: the garage range `10.0.70.0/24` and
its two stragglers, `10.0.70.14 garage-cam` and `10.0.70.31 ev-charger`.

Every colour here is one the app already ratified — the flag-family
palette, the action badges, `--accept` / `--now` / `--accent` — so no new
data palette is minted and the dataviz validator has nothing new to
check.

## For the build

- **The family picker means custom detectors need a stored family.**
  `flagPalette.familyOf` falls back to the operator-authored `custom` ink
  for anything not in `FLAG_FAMILIES`; this drawing has the author
  choosing one of the seven instead, so the custom-detector API needs a
  `family` field, `familyOf` needs to read it for custom definitions, and
  the docket, the fall and the map then pick the ink up for free.
- **Dropdown keyboard model.** The bar is one focus stop. Typing narrows
  the current step; `↑ ↓` move the highlight; `enter` takes the
  highlighted row and advances a step; a typed value is committed by
  `enter` straight from the bar; `shift ⇥` steps back; `esc` closes and
  leaves the token as it was. Each token's `×` is a real button, so
  removal already has a keyboard path.
- **Clone from a shipped declarative detector needs the API to hand back
  that detector's conditions.** `BuildShippedDeclarativeDefinition` has
  them; the clone response or a GET must expose them, or the copy arrives
  with an empty bar. Cloning a *code* detector carries scope and numbers
  only, and its copied-from line says so:
  `COPIED FROM INTERNAL RECONNAISSANCE · SCOPE AND NUMBERS · ITS
  CONDITIONS ARE BUILT IN — WRITE THEM HERE`.
- **Verbs are offered per field**: `is · is not · is one of · is not one
  of · is within · is between · is classed as`. `is between` covers time
  of day as well as ports — one grammar, `1-1024` or `22:00-06:00`, with
  the hint following the field.
- **Placeholders are the engine's closed set per key mode** — `{Count}`
  plus the key's own fields — offered on the says line and inserted on
  click.
- The dropdown is drawn here pushing the drawer down so one screenshot
  can show the bar, the list and the actions together; in the build it
  overlays, anchored to the bar's own `getBoundingClientRect()`.

## Deviations from the brief

- The brief's example family line reads `▲ GARAGE PROBES`, but the
  detector is filed as **scan**, and `flagPalette.ts` gives scan the `✱`
  alarm mark. The drawing uses `✱` so the glyph matches the ink the
  docket will draw; changing it back would put a mark on this screen that
  the docket contradicts.
- `{Count}` / `{SourceAddress}` keep their mixed case inside the
  `.stamp`-style token rather than being uppercased, since they are the
  literal placeholder text the engine substitutes.

## Verdicts

Owner, 2026-09-09: *"this is much closer but the bar looks cluttered and
complicated when we're trying to give 'clean and simple' vibes. Right
idea, slightly wrong execution. But MUCH better than previous rounds.
We're getting somewhere now."*

→ Round 8 keeps everything round 7 got right — the docket-flag drawer,
the family ink running through the surface, a GitLab-behaviour bar in
the stream's clothes, the dropdown that is not a card — and redraws only
the bar so it reads clean and simple. Nothing else moves.
