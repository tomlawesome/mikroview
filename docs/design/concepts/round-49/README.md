# Round 49 — the living topology (#1016)

Proposal, not yet ratified. One rule, drawn on both surfaces of the map:
**what a boundary element looks like is what its own boundary's logging
is.** Verdict is the colour; coverage is the material. Nothing is written
on the map that the material already says.

`index.html` is the mockup, nine scenes down the page; `shots/` are the
captures (`node ../docs/design/concepts/round-49/capture.mjs` from
`frontend/`). The 2D half is round 30's `#s3` markup and CSS; the city
half is round 40's engine (projection, device library, river, walls). The
build ports this markup and CSS; it does not build from an impression.

## The rule, per element

| Element | logged | quiet on purpose | dark |
|---|---|---|---|
| 2D rib half | verdict ink, solid (`--accept` .55 / `--alarm` .7) | `--quiet` white, solid, .3 | `--dark` grey, dashed `3 6`, .5 |
| City wall edge | district ink, round-40 faces | white translucent faces, no lamp | grey translucent, dashed top edge, grey posts, no lamp |
| City gate | accent posts, one lamp `--now` | grey posts, no lamp | grey posts, no lamp |
| Bridge | accent deck, lamps | white deck, no lamps | grey deck, dashed rails |
| District plate | district ink | district ink | grey dashed **only when every boundary is dark** |
| Road | drawn (verdict ink) | none | none — a road across a dark boundary would claim a log line never written |

- A **2D rib is two halves** split at its midpoint; the half nearest a
  card carries that card's own direction across the boundary. `rb → iot`
  logs (green top half), `iot → rb` does not (grey dashed bottom half).
  The escalated unplanned rib is undivided, red, glowing, with its
  callout, as in round 30.
- A **city wall edge** takes the worst of its gates' two directions
  (dark > quiet > logged) because a wall has no direction; the card
  lists both. Ungated edges draw normally.
- **Plaques say name · subnet** only. LOGGED / DARK / QUIET are gone from
  the plaque; the wall says it. `no rule table pushed` stays as a dim
  second line because it is a different fact from dark (DESIGN.md's
  #865 wording).
- **Lens row** is two pills, `⚑ flags` and `◉ watch`, off by default
  in nothing (both on in the mockup). Traffic, policy and coverage as
  modes are gone — coverage is always on, traffic is always on, policy
  went in slice C.
- **Hosts at the services stop**: a dot row inside each lane card
  (ten dots then `+N`), lane ink for live, grey for quiet, white
  translucent for quiet on purpose; red halo when flagged and the flags
  pill is on; purple ring when watched and the watch pill is on. Every
  dot is a host, clickable to its reach. Count line under the dots.
- **Presence** (city and 2D alike): a host not heard for the quiet
  window is drawn grey with a dashed footprint and `quiet · 14 min`;
  one the operator marked `quiet on purpose` is white translucent. Both
  stay on the map; neither is a data claim beyond "not heard".
- **Cards** are the one interaction. Boundary card (dark): what the rule
  does, both directions, actions `declare quiet on purpose ▸ · rules ▸ ·
  stream ▸`, pin. Pinned it opens the declare form: reason (required),
  `both directions ☑`, `Declare`, who. Quiet card: the reason quoted,
  who and when, `undeclare ▸`. Host card: presence, last/first seen,
  events, `mark quiet on purpose ▸ · dismiss ▸`. Line card (reach):
  port / proto / accepted / dropped table from slice D's
  `reachLineSummary`, the totals line, `:22 refused by #17 default drop`;
  the composer becomes `draft the rule ▸` on a refused strand's card.
- **Reach**: no strand pills; the crumb wording is the city's on both
  surfaces (`name · ip · reaches N · reached by N · refused N · Esc
  surfaces ▸`). A refused strand is red and ends in a ✕ mark; nothing is
  written on the strand — the card carries the ports.

## Scenes

| id | what it shows |
|---|---|
| `flat` | services stop, both pills on: split ribs, host dot rows, Guest dark both ways, WireGuard quiet |
| `flat-declare` | the Guest → wan boundary card pinned, declare form open |
| `flat-quiet` | the WireGuard card: reason, who, undeclare |
| `flat-host` | tv-lounge quiet 14 min, its card, watch pill off |
| `flat-reach` | tom-desktop's reach, the nas line card |
| `city` | ◆ city: Guest an island, wg0 a white footbridge, lamps where logging is |
| `district` | Guest district: grey dashed wall, no road, the boundary card with the form |
| `street` | LAN street: tv-lounge grey with dashed footprint, printer-old white |
| `reach` | standing on tom-desktop; the nas line card at street |

## Data story

LAN logs both ways; Servers logs; IoT logs in (`wan → iot`, `srv ↔
iot`) but `iot → wan` is dark; Guest is dark both ways; WireGuard was
declared quiet ("peers are trusted; logging them is noise" · tom ·
2026-09-01); Workshop logs, `pc-bench` is quiet; Cams has no rule table
pushed. Hosts: `tom-desktop` watched, `laptop-anna` and `cam-porch`
flagged, `tv-lounge` quiet 14 min, `printer-old` quiet on purpose.

## Open questions

Numbered for reply; the counter continues from the session.

4. **Drop ink.** The map's dropped-only ink moves from amber `--drop`
   to red `--alarm` so that amber stays the lamp's colour and grey /
   white stay coverage. Keep amber for drops and find another lamp
   colour instead?
5. **Direction on a wall.** A 2D rib shows both directions (two
   halves); a city wall shows the worse of the two and the card lists
   both. Acceptable, or should the wall carry both (e.g. inner face and
   outer face)?
6. **Ungated edges** of a district draw in the district's ink. The
   alternative is every edge grey unless a gate lights it, which makes
   a one-gate district almost all grey. Keep as drawn?
7. **Declare defaults to both directions** (`☑`). One direction
   declared and the other still dark would leave the wall grey and the
   card explaining why; default both?
8. **`no rule table pushed`** stays on the plaque as a dim line. Keep,
   or move it into the card too?

## Superseded

- Coverage as a lens (round 30's `coverage` tab, DESIGN.md "The lenses
  in the city"): the material carries it always.
- Policy lens: removed in slice C; the walls and gates already are the
  rules.
- LOGGED / DARK / QUIET words on plaques and lane captions.
- Ports written along a reach strand: the line card carries them.

Written by Claude Fable 5, 2026-09-06.
