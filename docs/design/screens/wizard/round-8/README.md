# Setup wizard — round 8 (#1374): the ledger refined, and the fall comes alive

Owner, after round 7 (verbatim in `../round-7/README.md`): Z is "a good
implementation of the original wizard style, and I do like it" — a
candidate to polish "using more Mikroview colour and design language";
and "I'd really like you to explore another full screen version that
has more journey. More user story." What every round so far failed on:
"the colour profile and Mikroview design language. Mikroview uses
beautiful, elegantly delivered visuals for clear display, and colour to
bring it alive. All of these mostly just use the boilerplate theme,
green and blue."

The lesson taken: the wizard's own green/blue receipts are not the
identity. The identity is the fall — mono boundary headers with
WATCHED in green and DARK — NO LOG RULE in red, a lane ink under each
column, hatching where nothing is logged, traffic-blue and refused-red
peaks and marks, the amber NOW line, the chips in the top bar, the rail
of rotated section names. `fall.css` and `fall.js` port that grammar
from `Fall.svelte` value for value (the scout's file:line trail is in
the commit), and both concepts draw with it. Same shell, same engine,
same five questions and one paste as round 7; the router address is
now checked as it is typed (#1380).

Served from the design host, port 8309:

| | Concept | Open |
|---|---|---|
| **AD** | **The ledger, refined** — round 7's Z with the fall in it | `http://192.168.11.30:8309/screens/wizard/round-8/ad-ledger.html` |
| **AC** | **The fall comes alive** — full screen; setup is the fall lighting up | `http://192.168.11.30:8309/screens/wizard/round-8/ac-alive.html` |

Answer the five questions, Copy, watch. Prototype strip at bottom left:
scenario (happy path · router sends from another address · RouterOS
ahead of review), speed, Restart. `?demo` prefills every answer.

## AD — The ledger, refined

Round 7's Z, kept as drawn, with the app's colour where the wizard
touches the fall:

- The push question shows **what the push makes of the fall**: the
  boundary strip and six column headers as they will stand — three
  watched, three dark — against the one grey column you get without it.
- "So far" is a row of the fall's chips, in the decision ink.
- After the paste, arrivals land on a **NOW ruler** — copied, certificate,
  enrol line, first push, backup — with the amber cursor moving, above
  the observation lines.
- **Tagging is done on the fall itself**: the columns the push drew, dark
  ones hatched; touch one and it arms in its lane colour ("tagged —
  waiting for a line"); the block underneath rewrites; after the paste
  each column turns on as its first line lands.
- The address field says what is wrong while you type — no port, no
  name, four numbers — and Next stays off until it is a bare address.
- Where setup stands wears the boundary strip and headers above the
  ledger, so "5 watched, 1 dark" is the first thing read.

## AC — The fall comes alive

Full screen, no modal. The fall is the setup screen, and it starts dark:
one hatched column, "unknown — waiting for a router", NOW standing
still, a chip saying no router yet. A glass panel in the middle asks the
five questions with Z's anatomy. The frame answers each move:

- **Name** — the chip bar carries it; the column is now "rb5009 · all
  traffic".
- **Copy** — the clock starts; the axis reads NOW.
- **Certificate, enrol line** — chips; the first lines rain down the one
  column, grey, address-only.
- **The push** — the rain splits into six named boundaries: watched ones
  in their lane ink with peaks (:22 SSH, :8291 Winbox, :443 HTTPS), dark
  ones hatched with the fall's own words, "blank because nothing is
  logged — not because nothing is sent"; a chip counts "3 dark
  boundaries — nothing logged".
- **Light the dark boundaries** — touch a hatched column to tag its rule;
  the panel holds the block; after the paste each column turns on as its
  first line lands and the chip counts down.
- **Done** — the panel folds to a ledger strip along the bottom (chips
  with undo beside each) and the operator is left standing in their live
  fall. The story is the product's opening argument: fix what you log,
  and watch the picture come alive.

Refused sender and RouterOS-ahead-of-review are the same #1132 box and
caution line as round 7, in the panel; the chip bar shows "refused ·
192.168.254.1" in alarm ink.

## The data story

As round 7, plus the boundaries the push reveals: `ether1 · input`
(in-ssh logging; wan-in proposed), `bridge → ether1` (est-rel; fwd-drop),
`ether1 → bridge` (dark; nat), `bridge-guest → bridge` (dark;
guest-lan), `wg0 · input` (dark; wg-in), `any · icmp` (icmp). Lane inks
per boundary from `--lane-*`; peaks and marks in `--fall-accept` /
`--fall-drop`; dark annotation and DARK label in `--fall-drop`; NOW in
`--now`. RouterOS 7.24.4 read from the push.

## Gates

- `drive.mjs` runs both through the happy path, the not-now path, the
  wrong-address recovery and ahead-of-review; no console errors.
  `shots/` is the record of that run.
- No new colour: every ink is an `app.css` token. The fall pair was
  validated for the fall (`concepts/round-3/README.md`); the lane inks
  in the #634 round. Not re-validated here for the small fragments in
  AD — do that before a build.
- Reduced motion honoured.

## Honest gaps

The boundary set is the story's, not derived from a real pushed table
(the analyser that would derive it is #1374's build question). The
fall behind AC is a rendering of the fall's grammar, not `Fall.svelte`
itself: a build would mount the real component with the wizard panel
over it. "Stay on the fall" and "Add another router" are alerts.

## Verdicts

**Owner, 2026-09-26 (verbatim),** answering question 2 of that session, after
asking whether AC is meant to sit on top of the real fall (yes: a build
mounts the real `Fall.svelte` under the panel), with the screenshot
`shots/owner-verdict-enrol-line.png` (the four green ledger rows:
certificate fetched, enrol line, first push, nightly backup):

"AC should be developed further. AD - the enrolment line is a nice idea
badly implemented. Use more mikroview colours. Try giving the 'succes'
things a mix of mikroview colours instead. And just be a bit more vibrant
instead, with greying out used to denote things. I'm not saying don't use
green at all, I'm just saying try to be more playful with the colour
whilst still being clear about what ti means"

- **AC** kept: developed further in round 9.
- **AD** kept too, not dropped (owner correction, verbatim: "? AD is not
  dropped at all"). Its enrolment line is a good idea to redraw.
- Colour, for round 9: the success rows are all one green; use a mix of
  MikroView's own colours, more vibrant, with greying out carrying
  meaning (done, not applicable), while still reading clearly.

**Owner clarification, 2026-09-26 (verbatim):** on the AD line, "I just
meant rethink the enrolment line." On the colour paragraph, "this line
applied mostly to AD but AC a bit too."

- Round 9 carries both AC and AD forward. AD: rethink the enrolment line,
  and the colour note applies in full. AC: develop further, colour note
  applies lightly.

**Owner, 2026-09-26 (verbatim),** on AC's done strip (screenshot
`shots/owner-verdict-done-strip.png`: the bottom bar of chips with undo
links, "start again", "Add another router", "Stay on the fall"):
"This bar is weird and I don't like it. Also the text "Stay on the fall"
is stupid, just say finish or something"

- Round 9: rethink AC's finished state; the strip goes. The closing
  button says "Finish" (or similar), not "Stay on the fall".

Written by Fable 5.1, 2026-09-26.
