# Setup wizard — round 6 (#1374): three live concepts

Owner, after round 5: "spin me up three new concepts please. Be brave,
go wild!" Three clickable prototypes on one shared engine
(`engine.js`: the data story, the simulated router, the state machine).
Same three moves in each — **Connect** (tell · paste · watch), **Logging**
(proposed from the pushed rule table), **Done** (evidence ledger, Undo
per row) — same scenarios, same numbers. What differs is the identity.

Served from the design host, port 8309:

| | Concept | Open |
|---|---|---|
| **W** | **Console** — the router's own lines are the interface | `http://192.168.11.30:8309/screens/wizard/round-6/w-console.html` |
| **X** | **Blueprint** — the network is drafted; evidence inks it | `http://192.168.11.30:8309/screens/wizard/round-6/x-blueprint.html` |
| **Y** | **Analyzer** — setup is reading an instrument | `http://192.168.11.30:8309/screens/wizard/round-6/y-analyzer.html` |

Drive each: fill the three fields, Continue, Copy, watch. The dashed
strip is prototype furniture only: scenario (happy path · router sends
from another address · RouterOS ahead of review), speed (real time · 4×
· 12×), Restart. `?demo` on any URL prefills the fields.

Lessons carried from rounds 4–5: no sentence-form entry, no chatty
headlines, standard-sized controls, one voice per surface. The
structure the owner has not objected to is kept: the RouterOS version is
learned from the push, not asked; one paste covers certificate, stream,
push and backup; rule tagging is the only second paste and is proposed,
not hunted.

## W — Console

A narrow instrument panel on the left carries the form and, later, the
evidence list and the rule table. The rest of the screen is a live
console: it announces what it is listening for, and setup is the section
markers appearing in it — `✓ certificate fetched`, `✓ enrol line …
stream open` followed by the raw line that proved it, `✓ first push`
followed by what the push contained, laid out as a key/value block. Real
lines stream underneath; each logging prefix has a colour, so the moment
a newly tagged rule fires is visible in the stream. Nothing is drawn;
the product's honesty is the aesthetic.

## X — Blueprint

The whole screen is a drawing sheet: grid, zone letters, a notes panel
top-left carrying the form, a title block bottom-right. MikroView and the
router are drawn in construction lines. Each proof inks something: the
certificate fetch inks the link, the enrol line lights the router and
starts a "lines received" counter, the push draws the interfaces and
fills the rule schedule. Numbered balloons with leader lines carry the
evidence and its time; the revisions table in the title block records
each one as a lettered revision; Done stamps the sheet **ISSUED**. A
different palette, deliberately: blueprint blue and one ink, alarm
colour reserved for a refused sender.

## Y — Analyzer

A settings strip on top (the four fields inline, then read-only once
set), a time ruler, and one lane per thing that must arrive:
certificate, enrol line, state push, backup, a log-rate trace, then one
lane per logging prefix. A NOW cursor moves; marks land on lanes at the
moment each proof arrived; pulses on prefix lanes are driven by the
simulated stream, so tagging a rule adds an armed (dashed) lane that goes
live on its first line. Steps open in a side drawer over the lanes, so
the instrument is never hidden. The metaphor is MikroView's own: it
reads a stream, and setup is watching the channels come up.

## The data story

MikroView at `192.168.13.15:8080`; `rb5009` at `192.168.13.1`, other
address `192.168.254.1`; Copy at 14:02:50, certificate 14:02:58, enrol
14:03:04, first push 14:03:21, backup 14:03:22. Times come from a story
clock, so they read the same at any prototype speed. RouterOS **7.24.4**
(7.25.1 in the ahead-of-review scenario). 47 filter rules, 3 logging
(`in-ssh`, `est-rel`, `icmp`), five proposed (`fwd-drop`, `wan-in`,
`nat`, `guest-lan`, `wg-in`). Interfaces `ether1` WAN, `bridge` LAN (31
leases), `wg0` (3 peers). Backup nightly 03:00.

## Gates

- `drive.mjs` runs all three through the happy path, the wrong-address
  recovery and ahead-of-review; no console errors. `shots/` is the
  record of that run, not the thing to review.
- Reduced motion honoured (draw-on and flow dots off).
- Prefix colours are an eight-step categorical set used only for prefix
  tokens and pulses; the app's accept/warn/reject keep their meanings.
  Not yet validated with the dataviz checker — do that before a build.

## Honest gaps, for whichever is chosen

Same as round 5: the push carries no board name; the rule proposals
assume the tune-logging analyser can run on the pushed table alone;
"Open the fall" and "Add a router" are alerts here.

## Verdicts

None yet.

Written by Fable 5.1, 2026-09-26.
