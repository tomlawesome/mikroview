# Round 56 — where a trace starts, the list of lines like it, and where the ✕ stands (#1050)

Round 54's successor: the owner's three picks on #1050 (note 14852,
2026-09-09) drawn on both surfaces. Round 55 is #1058's. Everything
from rounds 49–54 is carried verbatim; this round moves one ✕, adds
one list and one card, and relabels one callout.

## The rule

- **B1 — where it starts.** The stream row keeps its ⌖. Roads and the
  unplanned callout keep `trace ▸`: it draws the pair's newest line and
  opens the list at once, so the operator lands on the picker, not on a
  random line. A host card has no trace button; it keeps `stream ▸`.
  Round 54's card button is gone.
- **A1 — the list.** The crumb's "and N more like it" opens one list
  under the crumb, in place. Two columns: *same line* — same sender,
  destination, in-interface, out-interface and port, which is what the
  built count already matches on (`Topography.svelte`, `traceAsk`);
  *same minute* — the sender's other lines in that clock minute,
  showing what differs. Newest first, eight rows at most per column,
  the traced row marked; each column ends in "more in the stream ▸"
  with those filters already set. ↑↓ moves, ↵ redraws the trace where
  it is, Esc clears list and trace. The same HTML on both surfaces.
- **C1 — where it stops.** The ✕ stands where the rule is drawn: at the
  far end of the rib / in the destination district's gate when the log
  names an out-interface; on the router when it names none. This moves
  round 53's flat ✕ from the router to the LAN end of the rib (the log
  names `out: lan`), and puts an input drop's ✕ on the router on both
  surfaces. The chip sits by the ✕. No ghost path when the ✕ is at the
  gate — there is nowhere left to go; two grey words say what would
  have happened.

## The shots

`node ../docs/design/concepts/round-56/capture.mjs` from `frontend/`.

- `flat-start` — round 49's map; the callout ends in `trace ▸`; the
  pair card open on the red rib, `trace` its first action.
- `city-start` — the same callout and card on the city.
- `flat-trace` — the refused line, ✕ at the LAN end, chip beside it,
  dashed ring on tom-desktop, no ghost path.
- `flat-trace-list` — the same with the list open.
- `city-trace-list` — round 54's city trace, untouched, with the list.
- `flat-trace-input` — an input drop: Internet rib red to a ✕ on the
  router, chip beside it.
- `city-trace-input` — the same on the city: highway and WAN bridge
  red, flow inward, ✕ at the router's door.

## Data story

Round 53's, with one correction and one addition:

- **Correction.** Round 53's chip read `in: iot → out: —` for the
  cam-porch → tom-desktop drop. A RouterOS forward-chain log names both
  interfaces; it now reads `in: iot → out: lan`, which is what puts
  the ✕ at the LAN end. `out: —` is an input-chain line.
- **Addition.** The input drop: `192.0.2.61 → rb5009 (203.0.113.7)
  22/tcp`, `in: ether1`, no out, refused at `#1 input drop`, 22:03:58,
  41 more like it today — the SSH knocking behind round 49's
  "▲3.1× drops · wan-in".
- The list: 13 same-line drops between 21:52:13 and 22:04:31, all
  #17; four same-minute lines from cam-porch, RTSP to the NAS and one
  DNS lookup, all accepted at #26.

## For the build

- Flat map, built (#1018): move the ✕ and the chip to the rib's far
  end when the event carries `outInterface`; keep them on the router
  when it is empty. Drop the ghost half when the ✕ is at the far end.
- The list: one component fed by the same match the "N more like it"
  count already uses, plus a same-minute query on the sender; mounted
  under the crumb on both surfaces. Picking calls the existing trace
  request with the chosen event id.
- City: the trace drawing itself is not built yet (`City.svelte` has
  no trace); round 54 + this round are its spec. Gate position comes
  from the rule's `outInterface` as `gates.ts` already does.
- Callout: relabel is already `trace ▸`; it must now open the list.
- Pair card: new on both surfaces — the road's card with `trace` as
  the first action. Host card: unchanged from round 49.

## Open questions

Numbered for the owner's reply (session counter continues).

2. *Same minute* is drawn as the clock minute (22:04:01–22:05:00).
   The proposal said ±30 s around the traced second. Which?
3. The list drops over the router on both surfaces while open. Fine
   as a transient, or anchor it to one side of the crumb so the
   router stays visible?
4. The input drop puts a red halo at the top of the Internet rib to
   mark the sender's side. Keep, or leave the Internet card plain
   since the sender is off the map?

## Verdict

Pending.
