# Round 53 — the port filter and the event trace (#1018)

Round 49's living map (`#flat`, round 52's tunnel rule aside) with the
two tools #1018 decided on 2026-09-08 drawn onto it: **show me where a
port is used**, and **show me the path one event took**. Nothing new
is added to the map for either — the map dims to the answer.

## The rule

- **The port pill** sits bottom-left, where round 49's pills were
  before #981 took them. Idle it reads `⌕ port`. Clicking opens it
  into a bar of the same shape (owner, 2026-09-08: "make it look more
  like the bar" — "click and select"): the ports seen or named by a
  rule in the window as chips, click to select, several at once; a
  short field to type a list (`22,23 ↵`) instead; a divider; then
  `tcp` / `udp`, click to select. No Show button — the map filters as
  the selection changes. Active it collapses to
  `⌕ 445/tcp · 2 lines seen · 3 doors ✕`; the ✕ or Esc clears it.
- **Filtered, the map dims to where the port was seen** (owner: seen
  plus policy, "B"). A rib the port crossed keeps its verdict colour
  and width; every other rib is one thin grey line. Zone cards keep
  only the hosts seen on the port, lit, and their tally line reads
  `2 of 12 hosts on 445/tcp`. A card with nothing on the port dims
  whole. Nothing is removed.
- **A door marks every rule that names the port, seen or not**: two
  posts across the rib where the rule sits, the leaf open (green) for
  accept, a bar (red) for drop, labelled `#12 accept`. A door on a
  rib nobody used stays visible, dimmed less than the rib — "knowing
  where a door is open (even if unused) is useful information".
- **Nothing seen** is one line under the map, not an empty state:
  `no logged traffic on 3389/tcp in the window · one rule names it —
  #23 wan → any drop, the door on the WAN side`. The door is drawn.
- **The trace is one-hop, through the router**: the rib it came in
  on and the rib it left on, lit in the verdict colour at full width,
  everything else grey. A refused line ends at a ✕ on the router; the
  rib it *would* have taken is dashed with a note (`would have reached
  tom-desktop · never left the router`). An accepted line reaches its
  far end with a ring. The router's decision is a chip beside the
  router — `✕ REFUSED · #17 default drop` / `in: iot → out: — ·
  445/tcp · 22:04` — and the whole story is one crumb line at the top:
  who → who | port · verdict at rule · time | `and 13 more like it ▸`
  | Esc. The lit hosts keep a one-line tally (`cam-porch · 14× today`,
  `tom-desktop · never reached`).
- Both tools swap the legend for their own four entries.

## The shots

`node ../docs/design/concepts/round-53/capture.mjs` from `frontend/`.

- `flat` — round 49's map with the port pill idle.
- `port-pick` — the pill open as a bar: 445 and tcp selected.
- `port` — 445/tcp: LAN↔router↔Servers accepted, LAN→IoT refused,
  three doors (#12 accept on the Servers rib, #23 drop on WAN, #31
  drop on Guest — the last on a rib nobody used).
- `port-empty` — 3389/tcp: nothing seen, one door, one line.
- `trace` — cam-porch → tom-desktop 445/tcp, refused at #17: the IoT
  rib red to a ✕ on the router, the LAN rib dashed.
- `trace-accepted` — laptop-anna → Internet 22/tcp, accepted at #8,
  natted: LAN and WAN ribs green with flow, ring at the Internet end.

## Data story

Round 49's. 445/tcp: 2 lines, 14 drops, tom-desktop and laptop-anna,
nas, cam-porch. The traced refusal is round 49's UNPLANNED flag
(cam-porch → tom-desktop, #17, 14×). The traced accept is laptop-anna's
off-baseline ssh out, 21:26, `srcnat → 203.0.113.7:51820`.

## For the build

The parsed log line already carries everything drawn: in/out
interface, verdict, rule label, ports, NAT (`internal/routeros/
parser.go`); the doors come from the pushed filter-rule set (dst-port
match). No new data. Build issue once ratified.

## Open questions

Numbered for the owner's reply.

9. The city surface (round 47 tiles) is not drawn here: the port filter
   and trace are shown on the flat map only. Same rule there — dim to
   the answer — in a follow-on round after the build, or is flat-only
   enough for M11?
10. The trace opens from a flag (round 49's UNPLANNED chip) and from a
    stream line. Should a host's card also offer "trace the last line
    from here"?

## Verdict

Owner, 2026-09-08, verbatim. On the first picker: "the port picker is
ugly. Make it look more like the bar in 3" — "It's click and select,
first part is the port, click multiple ports to show multiple, (or
type the number, separated by comms and hit enter), second is the
protocol tcp/udp, no show button, it loads automatically." Redrawn
(670745c0). Then: "Rest looks good to me." Ratified as drawn: the
pill-bar picker, dim-to-the-answer with doors, the one-hop trace
with crumb and chip.

On the open questions: 9/10 — "Both, I think": the city surface gets
the same two tools, a follow-on round after the build. 11 — "It
should offer a simple trace button, not a sentence. I like the idea
but we need to be careful to ensure it's elegant and easy to use":
the host card gets a small trace button, drawn in that follow-on
round, not this build.
