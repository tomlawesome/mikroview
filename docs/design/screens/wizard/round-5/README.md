# Setup wizard — round 5 (#1374): "First light", a live prototype

Round 4's two screenshot sets were dropped ("extremely underwhelming,
lack creativity and have zero ambition to do something incredible";
owner, 2026-09-26), and the owner asked for "a live mockup this time
actually, not just screen grabs". This round is one clickable prototype,
direction **V**, driven by a simulated router.

**Open it:** `first-light.html`, served at
`http://192.168.11.30:8309/screens/wizard/round-5/first-light.html`.
Type a name, an address and any password, click **Write the paste**,
then **Copy**, and watch. The dashed strip bottom-right is prototype
furniture: pick a scenario (happy path; router sends from another
address; RouterOS ahead of review), a speed, or restart. `?demo` on the
URL prefills the story's values.

## The idea

Setup is the network coming into view. The operator gives the three
things the router cannot tell us, makes **one paste**, and then watches
MikroView draw the router from what arrives: the certificate fetch
draws the link, the enrol line lights the router and starts the
log-line counter, the first push unfolds its interfaces, its rule table
and its reported RouterOS version. Every line in the evidence column is
something that happened; nothing is a promise. The picture the operator
watches being built is the seed of the topography they land in.

Three moves, not five:

1. **Connect** — tell (a sentence, three fields), paste (one block, one
   Copy), watch (evidence arrives; the picture draws itself).
2. **Tune logging** — after the push MikroView holds the rule table, so
   it *proposes* the rules to log, each with its reason and prefix, and
   the operator unticks rather than hunts. One paste. Skippable.
3. **Done** — the evidence ledger with Undo this step… per row, Take me
   to the fall, Add another router, Start setup again…

## What the owner asked for, and where it lands

- *"as few steps as possible … ask once, reuse"* — three fields, two
  pastes (one if tuning is skipped). MikroView's own address is
  prefilled from the browser. Name, address and paste are reused
  everywhere; nothing is asked twice.
- *"check everything we can, ask for as little information as we can"*
  — the RouterOS version is **not asked**. The picker changes no byte of
  any command (`internal/routeros/commands.go` takes a dialect and never
  branches on it; `SetupWizard.svelte` ~838 says so), and the router
  reports its version in the first push, so the standing check runs on
  arrival: `reviewed`, or `ahead of review` with the warning shown
  there. This reopens the "version picked first and gated" ruling on
  that evidence — see Questions.
- *"show the right information at the right time … don't show things
  before they need to see it"* — one field's help shows only while it
  is focused; the paste's caption appears with the paste; the
  "Nothing yet?" drawer opens on request, and opens itself only when
  MikroView has something to say (lines refused from another address).
- *"don't keep showing them things we categorically know they've done"*
  — done moves leave the column; the rail ticks them; the picture keeps
  them.
- *"help recover from mistakes"* — the wrong-address scenario: MikroView
  names the address that is knocking and offers to use it; one click
  re-arms and the sequence completes. Undo this step… on the ledger
  carries round 4's §5 pair (what MikroView forgets; the lines that
  undo it on the router) verbatim.
- *"much more impressive / visually appealing"* — a full-screen dark
  canvas, one reading column, and a picture that only ever shows what
  has been proven. The "First light" moment marks the enrol line.

## What is carried forward from round 4 as ideas

The "Nothing yet?" drawer's two lists (what MikroView checked / only
you can check), the token's good-until time and Reroll, the folded
block with one prominent Copy, Undo this step… and Start setup again…,
the reported-version check with its warning. Rewritten into this
frame, not copied as drawn.

## The data story

Unchanged: MikroView at `192.168.13.15:8080`; the router `rb5009` at
`192.168.13.1`, other address `192.168.254.1`; 14:02 onwards. The
router reports RouterOS **7.24.4** (7.25.1 in the ahead-of-review
scenario). Its push: 47 filter rules of which 3 log (`in-ssh`,
`est-rel`, `icmp`), interfaces `ether1` (WAN), `bridge` (LAN, 31
leases), `wg0` (3 peers). Five proposed rules: `fwd-drop`, `wan-in`,
`nat`, `guest-lan`, `wg-in`. Backup nightly at 03:00.

## Honest gaps, for the build

- The push does not carry a board name today; the prototype shows only
  the operator's name for the router. Adding `board-name` to the push is
  one line in the script if wanted.
- The rule proposals come from the tune-logging analyser
  (`internal/api/tunelogging.go`), which today wants a pasted export; the
  prototype assumes it can run on the pushed table alone.
- Whether the log stream can open before the certificate is trusted, and
  whether the push script can run in the same paste as the enrol line,
  is as today's wizard already does across steps — only the order and
  the number of pastes change.
- "Take me to the fall" and "Add another router" are alerts here.

## Gates

- Driven end to end by `drive.mjs` (all three scenarios), no console
  errors; `shots/` holds one capture per stage from that run, for the
  record only — the prototype is the thing to review.
- Reduced-motion: particles off, transitions instant.
- Palette: the app's own tokens; accept/warn/reject used only for
  evidence, warning and refusal.

## Questions for the owner

Numbered in the session's reply, recorded here when answered.

- Direction V: build it, iterate it, or drop it?
- Drop the version picker, and check the reported version on arrival
  instead (evidence above)?
- One paste covering certificate, log stream, push script and backup,
  with rule tagging as the only second paste?

## Verdicts

**Owner, 2026-09-26 (verbatim):** "Yeah... it's more creative, more
inventive...but it's not what I'm after. It's a bit clumsy, inelegant.
The prose driven stuff.. entry as a sentence it's a bit... childish
almost."

Dropped as drawn. What survives into round 6: the structure (version
learned from the push, one paste, tuning proposed after the push, the
picture drawn from evidence, the evidence ledger with Undo). What goes:
the sentence-form entry, the chatty headlines and leads, the oversized
controls, the "First light" flourish.

Written by Fable 5.1, 2026-09-26.
