# Round 55 — the decommission offer and the ghost segment, on both surfaces (#1058)

#460's offer, receipt and grey ghost were shaped for the flat map on
2026-08-29 (#485's body) before the city existed. The owner ruled on
2026-09-09: *"We need to draw the city offering and ensure any common
elements are the same between both views."* This round draws them side
by side. Rounds 49–56 are carried verbatim (the file is round 56's with
this round's block appended); nothing already on either surface moves.
Round 56 was drawn first, so the numbering is out of order.

## The rule

- **One offer, one card.** When a push stops carrying a segment, the
  fall's card is the same HTML component on both surfaces: what left
  (address, rule, leases), the receipt from the last 24 h, the question,
  and three answers — *Watch it*, *no — let it go*, and the note that
  six quiet hours retire it by themselves. Nothing is watched until the
  operator says so.
- **The receipt is a replay.** "In the last 24 h a watch here would have
  caught 3 lines — all from 10.0.70.14, last named garage-cam." It is the
  only argument the offer makes; it is drawn in watch ink because it is
  what the watch would have shown.
- **The ink is the watch's state.** Grey before the watch starts,
  watch-purple *holding*, alarm-red *broken*. The state names and inks
  are the fall's own, so the ghost reads like any other watched thing.
- **Each surface keeps its own geometry.** On the flat map the ghost is
  a fifth lane, hollow and dashed, with its last-known hosts as rings;
  the four live lanes re-space to make room and go back when it leaves.
  On the city it is a plate with no fill, a dashed wall all round, and
  its last-known hosts faded inside; the borough counts it (*4 districts
  · 1 ghost*). Same tally line, same watcher pip, same legend entry.
- **A straggler breaks the watch.** Any line to or from the dead range —
  even one a rule accepts — is a violation: it turns the ghost red, draws
  the line in alarm ink (an arc on the flat map, a road on the city), and
  is named with the host that last had the address. The clock resets.
- **Force-remove lives on the ghost's card, not on the offer.** The offer
  is a yes/no; there is nothing to remove yet. On the ghost's card it is
  the first action, marked hot, and its confirm state is the whole
  contract: the ghost leaves the map now, the watch goes on in the
  watchlist until it retires, and it can only be forgotten from there.
- **The leader is heavier than round 49's.** A ghost is faint by design, so the line from its card to it is accent ink at 1.8px, and the card sits beside the ghost on both surfaces.
- **Retirement is silent.** Six quiet hours and the ghost leaves by
  itself; the map goes back to round 49's, with one note line and an
  *undo* for the hour after.

## The shots

| scene | what it shows |
| --- | --- |
| `flat-offer` | five lanes, the fifth hollow and grey; the offer card with the receipt |
| `city-offer` | the same moment: Garage's plate an outline beside Guest; the same card |
| `flat-ghost` | the watch taken: lane, tally and pip in watch purple; header ◉ 7 → 8 |
| `city-ghost` | plate, wall and pill in watch purple; legend names it; no card |
| `flat-straggler` | lane red, a red arc ghost → Servers, the callout, the straggler's card |
| `city-straggler` | the same line as a red road ghost gate → Servers' IoT gate; ○2 in the header |
| `flat-retire` | 03:52, 4 h of 6 h; the ghost's card in its force-remove confirm state |
| `city-retire` | the ghost's card on the city before the press; force-remove first and hot |
| `flat-gone` | 04:11: four lanes again, the note line, *undo ▸* |

## Data story

Round 53's estate, plus one segment. Garage, `10.0.70.0/24`, sat behind
rb5009 with three hosts — garage-cam `.14`, garage-door `.20`,
ev-charger `.31` — and rule #29 logging garage → servers. It was folded
into IoT on 1 Aug and the devices readdressed; the 22:07 push tonight is
the first that no longer carries it. The receipt: a watch on the range
over the last 24 h would have caught 3 lines, all from `.14`. At 22:41
`10.0.70.14` talks to the nas on 554/tcp, arriving on iot and accepted by
#26 — a camera with a static address the move did not touch. The watch
breaks and the clock resets; it is readdressed by 23:50. At 03:52 the
watch has held for 4 h of 6 h; at 04:11 the ghost retires by itself.

## For the build

- One `card` component for the offer, the ghost and the straggler, on
  both surfaces; the watchlist row inside a card is the same row the
  watchlist draws.
- Ghost state is `none | holding | broken`; the inks are `--dark`,
  `--watch`, `--alarm`; both surfaces read the same state.
- Flat map: a `ghost` lane kind; lanes re-space by count (four at
  285/577/848/1116, five at 188/444/700/956/1212) and the ribs' control
  points follow their lane. The straggler is the unplanned pair's arc
  in alarm ink.
- City: a district with `ghost` set draws plate, wall and plaque in
  the state ink, hosts with `pres: 'ghost'`, and no road from the router
  (the router no longer has one). The straggler road runs from the
  ghost's gate to the destination district's gate for the interface the
  log names.
- The header counts move with the watch: ◉ +1 while it holds, ○ +1 and
  ⚑ +1 while it is broken.
- Force-remove closes the ghost, not the watch; the watchlist row keeps
  `decommission · holding · N h of 6 h` and is the only place to forget
  it early. Retirement writes the note line and keeps *undo* for 1 h.

## Open questions

1. Should the receipt window be 24 h, or the watch's own 6 h? 24 h is
   drawn: it is the argument, and 6 h can be empty on a quiet night.
2. On the city the ghost's position is where the district was drawn —
   here beside Guest. Should a ghost move to the borough's edge instead,
   so live districts keep the middle?
3. The straggler's card offers *trace* first (round 56's rule for a pair
   card). Should it be *watch this host* instead, since the range is dead
   and the host is the thing left to follow?

## Verdict

(awaiting the owner)
