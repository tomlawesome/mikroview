# Round 50 — the tunnel column (#890)

Where the second tunnel goes, and the third, and the fifth. Everything
else is round 49's `#flat` unchanged: the Internet card, the router and
the zone row do not move, and no ink or glyph is new.

## The rule

- Tunnels stack **below wg0** in the same card at the same width, one
  column pitch apart, **busiest first** — wg1 below wg0 because it
  carries less, l2tp-out below that.
- Each gets **its own strand to the router's right shoulder**, the way
  the zones fan from its bottom edge (252, 268, 284, 300 down that
  edge). Round 49's rule reads them and nothing is written on them: wg0
  white (quiet on purpose), wg1 thin and dim (established), l2tp-out
  bright with the ring at the end it arrived at (off baseline today).
- **Three, then a "more" card**: the rest fold into one card in the next
  slot, same size, a shade dimmer because it is a list and not a tunnel,
  with one strand because the tunnels it stands for still reach the
  router. It counts them, names them, and opens into a list card beside
  it, one row each, in round 49's card idiom. Nothing is hidden.
- **Pitch.** The zone row's lane pitch is 268 px centre to centre, 52 px
  between cards; at that gap the fourth slot lands on the Guest rib and
  then the zone row, so the column is tightened to a 76 px pitch (56 px
  card, 20 px gap): slots at y 208, 284, 360 under wg0 at 132. The last
  card ends at 416, clear of the Guest rib (crossing at y≈431) and of
  the zone row at 484. wg1 has no watch bar for the same reason — the
  bar would close that 20 px gap.

## The shots

`node ../docs/design/concepts/round-50/capture.mjs` from `frontend/`:
each scene full-frame (2800×1720) and cropped to the top-right quarter,
the column and the router's shoulder (`-crop.png`, 1800×1400).

- `two-tunnels` — wg0 and wg1, one slot apart, two strands.
- `three-tunnels` — l2tp-out in the third slot, bright, with its ring.
- `more-tunnels` — the fold: three tunnels, one dimmer card for two more.
- `more-tunnels-open` — the same, the fold opened into its list card.

## Verdict

Owner, 2026-09-08, verbatim: "It's ok but the column idea needs to go -
just place them in the map, we have pan and zoom, and the default zoom
should be set to be dynamically adjusted so it fits the map broadly in
the view at the starting level."

So: the strands, the busiest-first order and the fold survive as ideas;
the stacked column under wg0 is dead. Round 51 places each tunnel in
the map as its own stop, and the map opens at a zoom that fits the
whole of it. #890.

Drawn by Claude Opus 5, 2026-09-08.
