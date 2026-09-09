# Round 54 — the port filter and the trace on the city, and a trace button on the host card (#1050)

Round 53's two tools carried onto round 49's city surface, plus the
host-card trace button #1018's verdict asked for. Same rule as round
53 — nothing new is added to the map, it dims to the answer — so the
city and the flat map answer the same question the same way.

## The rule

- **Filtered, the city dims to where the port was seen.** Districts
  with a host on the port stay lit; the rest go to outline. A road the
  port travelled keeps its verdict colour; the others thin to grey. Each
  district's plaque gets a one-line tally under it (`2 of 12 · 445/tcp`,
  `0 of 3 · 445/tcp`). Round 49's UNPLANNED callout and the far-bank
  peers come off: the filter is the story now.
- **Doors stand in the gates.** A rule that names the port is two posts
  in the district wall where its road enters (`#12 accept` on the
  Servers gate, `#31 drop` on Guest's, `#23 drop` on the WAN bridge
  deck) — the same open-leaf / bar drawing as round 53, on the wall
  instead of the rib. An unused door still stands.
- **Nothing seen** is the same one line as round 53, under the city.
- **The trace lights one route through the router.** Refused: the
  road from the sender is red to a ✕ at the wall the line stopped at;
  the road it would have taken is a dashed ghost with a note
  (`would have reached tom-desktop · stopped at the LAN wall`), the
  verdict chip sits beside the stop, and the two hosts keep their
  tallies on the plaques. Accepted: both roads green, a halo on the
  sender, a ring at the far end, the chip beside the router. Round
  53's crumb line runs across the top of the city too.
- **The host card gets `trace`** as its first action, a small pill
  button beside `reach`, `watchers`, `stream` — not a sentence. Its
  hover title says what it does: *draw the last line's path through
  the router*. The card's "last line" row is the line it will trace.
  Same card on the flat map and in the district.

## The shots

`node ../docs/design/concepts/round-54/capture.mjs` from `frontend/`.

- `city-port` — 445/tcp: LAN, Servers, IoT lit; Guest outlined; three
  doors; tallies under every plaque.
- `city-port-empty` — 3389/tcp: nothing seen, one door on the WAN
  bridge, one line under the city.
- `city-trace` — cam-porch → tom-desktop 445/tcp, refused at #17: red
  road from IoT to the ✕ at the LAN gate, ghost into LAN, chip above.
- `city-trace-accepted` — laptop-anna → Internet 22/tcp, accepted at
  #8: LAN road and WAN bridge green, halo on laptop-anna, ring at
  ether1, chip left of LAN.
- `flat-host` — round 49's flat map with cam-porch's card open, the
  trace button first.
- `city-host` — the same card open in the IoT district.

## Data story

Round 53's, unchanged.

## For the build

Same data as round 53 (parsed log lines, pushed filter rules). The
city needs: district tallies from the port filter, the door drawn at a
gate (rule → out-interface → district wall), and the ghost road for a
refused trace. The card button calls the trace with the host's last
line. Build issue once ratified.

## Open questions

Numbered for the owner's reply.

13. The refused trace stops the ✕ at the wall of the district it was
    going to (the LAN gate), not on the router as the flat map does —
    the router is off the road here. Keep the two surfaces different,
    or put the ✕ on the router in the city too?
14. The card's `trace` traces the host's last line only. A host with
    several recent lines (cam-porch has a refused one and an accepted
    one) traces the newest. Enough, or should the row under the card
    each carry the button?

## Verdict

Pending.
