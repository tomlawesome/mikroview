# Setup wizard — round 9 (#1374): one ink per thing

Owner, on round 8 (verbatim in `../round-8/README.md`): both kept. AC
"should be developed further"; AD's enrolment line is "a nice idea badly
implemented" — "I just meant rethink the enrolment line". On colour,
"mostly to AD but AC a bit too": "Use more mikroview colours. Try giving
the 'succes' things a mix of mikroview colours instead. And just be a
bit more vibrant instead, with greying out used to denote things. I'm
not saying don't use green at all, I'm just saying try to be more
playful with the colour whilst still being clear about what it means."
And on AC's closing bar: "This bar is weird and I don't like it. Also the
text 'Stay on the fall' is stupid, just say finish or something".

What this round does with that, in one decision: **every artefact the
wizard makes gets one ink, and keeps it everywhere it appears.** Six
inks, all app.css tokens that already mean the same kind of thing in the
app (`inks.css` has the reasoning per ink):

| Thing | Ink | Why that one |
|---|---|---|
| certificate trusted | `--accent` (MikroView blue-grey) | the router now trusts MikroView — MikroView's own ink |
| logs flowing | `--accept` (green) | the one green, kept for the moment the fall starts |
| router state pushed | `--fall-nat` (purple) | the push draws the boundaries; purple is the fall's "the router rewrote it" |
| nightly backup | `--now` (amber) | a clock thing; amber is time |
| the token, your answers | `--log` (decision blue) | a decision you made |
| rules tagged | `--lane-guest` (pink), each rule's dot in its own lane | the lane family |
| not yet · not now · set aside | `--fg-dim`, dashed, struck | greyed out denotes it |

The ink follows the thing through the step list, the "so far" chips, the
track, the closing ledger and (in AF) the top bar's chips — so a colour
reads as *what* arrived, not just *that* something did.

Served from the design host, port 8309:

| | Concept | Open |
|---|---|---|
| **AE** | **The ledger, in colour** — round 8's AD with the enrolment line rethought | `http://192.168.11.30:8309/screens/wizard/round-9/ae-ledger.html` |
| **AF** | **The fall comes alive, finished in the panel** — round 8's AC without the bar | `http://192.168.11.30:8309/screens/wizard/round-9/af-alive.html` |

Answer the five questions, Copy, watch. Prototype strip at bottom left:
scenario, speed, Restart. `?demo` prefills every answer.

## The track (both)

Round 8's enrolment line was four identical green boxes with a separate
ruler above them. Here they are one thing, `track.js`: a wire from Copy
to Done with a station per proof. A station is a grey ring until its
event lands, then fills in its ink and the wire colours up to it; the
amber NOW cursor travels the gap to the next one; a "not now" station is
dashed and struck; a refused sender is the alarm ink at the logs station.
Under each done station on the finish screen: undo.

## AE — The ledger, in colour

Round 8's AD kept as drawn, with:

- the track in place of the ruler and the four boxes; under it, one
  observation line for the latest arrival in that arrival's ink, and
  the stream;
- step-list receipts in the ink of what they record (named: decision
  blue; enrolled: green; first push: purple; backup: amber);
- "so far" chips per answer in their ink, "not now" greyed and struck;
- the closing ledger: each row's ring and receipt in its ink, set-aside
  rows greyed and struck; the compact track above it; **Finish**.

## AF — The fall comes alive, finished in the panel

Round 8's AC kept, and the bar is gone. The panel that asked the
questions is the panel that finishes: "rb5009 is sending", the track
with undo under each station, "undo everything" as before, and
**Finish** — which closes the panel and leaves the operator on their live
fall. The top bar's chips, now in the same inks (cert · logs · push ·
dark · backup · rules), are what remains; "Run setup… reopens the ledger"
sits at the right. The router's-turn panel carries the track too. Colour
elsewhere is as round 8: the fall's columns already wear lane ink.

## Gates

- `drive.mjs` runs both through the happy path (including Finish), the
  not-now path, the wrong-address recovery and ahead-of-review; no
  console errors. `shots/` is the record.
- No new colour: every ink is an `app.css` token. The six-ink set was
  chosen for distinctness on the dark canvas by eye across the shots;
  not run through the dataviz validator as a categorical palette — do
  that before a build, since these six will sit side by side.
- Reduced motion honoured (the wire and cursor stop animating).

## Honest gaps

As round 8: the boundary set is the story's, and AF's fall is a rendering
of the fall's grammar, not `Fall.svelte`. "Add another router" is an
alert. Station labels are short (`certificate`, `logs`, `push`) so six
fit in a 640px body; their stamps truncate at narrow widths.

## Verdicts

None yet.

Written by Fable 5.1, 2026-09-27.
