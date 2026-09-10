# Round 49 — the living topology (#1016)

Proposal, not yet ratified. Two rules, drawn on both surfaces of the map:

1. **What a boundary element looks like is what its own boundary's
   logging is.** Coverage is the material: solid ink, white, or grey
   dashed. Nothing is written on the map that the material already says.
2. **Colour is the verdict; brightness is the baseline.** A line
   (source → destination · port · proto) seen on 3 of the last 14 days
   is established and recedes; a line off that pattern is bright, with
   a throbbing ring where it arrived. Refused is red. Nothing is ever
   removed, only dimmed — the sieve for "traffic that should not be
   happening" on a network that otherwise talks on the same routes and
   ports every day.

`index.html` is the mockup, ten scenes down the page; `shots/` are the
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

### Brightness (the baseline)

| Line state | rib / road / strand | dot |
|---|---|---|
| established (accepted, on the pattern) | `--accept`, thin (0.6×), opacity .26–.28, no flow | lane ink |
| off-baseline (accepted, not on the pattern) | `--accept`, full width, opacity .8–.85, flow dashes, `.halo` ring in `--accept` at the end it arrived at | bright |
| refused | `--alarm`, ✕ where the rule stopped it | — |
| escalated unplanned | `--alarm`, glow, callout | — |

A rib, road or dot is as bright as the brightest line it rolls up: one
off-baseline line among a thousand lights the one place it happened.
The header counts them (`⟡ off-baseline today · 3`). The card on a bright
rib lists the lines, first-seen time and count, and offers `expected ▸`
— a reason that stays said, and the line is established from then on.
Line count never grows with traffic: one rib per zone pair, one road per
district pair, one lane per host to its gate; only the reach draws per
line, for one host by choice.

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
  window is drawn grey with a dashed footprint and `quiet · 26 h`;
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
| `flat-host` | tv-lounge quiet 26 h, its card, watch pill off |
| `flat-reach` | tom-desktop's reach: established strands dim, the nas strand bright (one new port), its line card |
| `flat-new` | the rb5009 → Servers rib hovered: the roll-up card, the one off-baseline line, the expected form |
| `city` | ◆ city: established roads dim, the two carrying today's new lines bright with rings; Guest an island, wg0 a white footbridge, lamps where logging is |
| `district` | Guest district: grey dashed wall, no road, the boundary card with the form |
| `street` | LAN street: tv-lounge grey with dashed footprint, printer-old white |
| `reach` | standing on tom-desktop; the nas line card at street |

## Data story

LAN logs both ways; Servers logs; IoT logs in (`wan → iot`, `srv ↔
iot`) but `iot → wan` is dark; Guest is dark both ways; WireGuard was
declared quiet ("peers are trusted; logging them is noise" · tom ·
2026-09-01); Workshop logs, `pc-bench` is quiet; Cams has no rule table
pushed. Hosts: `tom-desktop` watched, `laptop-anna` and `cam-porch`
flagged, `tv-lounge` quiet 26 h (window 24 h), `printer-old` quiet on purpose.
Off-baseline today, 3 lines: `tom-desktop → nas · 5001/tcp` (accepted,
first seen 21:26), `laptop-anna → Internet · 22/tcp` (accepted),
`cam-porch → tom-desktop · 445/tcp` (refused, the unplanned pair).

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
11. Baseline rule — owner: "ok let's see it" (2026-09-07); drawn in this
    cut, awaiting the verdict on the drawing.

## Decisions (owner, 2026-09-07)

- **The purpose** of flags, watchers and this map is one thing: make
  potential threats easy to see and whittle the noise down to genuine
  ones, without arbitrarily hiding anything that could be bad. Traffic
  more than devices: an established network sits and talks on the same
  routes and ports, so what matters is a line off that pattern.
- **Quiet** is 24 hours of nothing, not minutes; configurable, 24 h
  default. Presence is housekeeping, not the sieve.
- **Established** is a line (source → destination · port · proto) seen
  on 3 distinct days out of the last 14; configurable. Anything else is
  off-baseline. Learned from recurrence, never maintained by hand.
- New devices: maybe worth marking, but secondary to new traffic.

- **Off-baseline, not new devices, is the sieve** (2026-09-07): the
  worry was a mess of lines on a busy network; the answer is the roll-up
  above — brightness changes, line count does not. Drawn in this cut.

## Superseded

- Coverage as a lens (round 30's `coverage` tab, DESIGN.md "The lenses
  in the city"): the material carries it always.
- Policy lens: removed in slice C; the walls and gates already are the
  rules.
- LOGGED / DARK / QUIET words on plaques and lane captions.
- Ports written along a reach strand: the line card carries them.
- The `⚑ flags` / `◉ watch` lens pills: no toggle, the marks come and go
  with the data (owner, 2026-09-08, #981).

Written by Claude Fable 5, 2026-09-07.
