# The setup wizard — the ratified design (#1374)

Ratified by the owner on 2026-09-27: round 15's **AN, without sparks**
(`round-15/an-neon.html?nosparks`), carrying round 10's wizard **AG**
with round 15's Yes / No change. Rounds 3–15 beside this file carry the
prototypes, the screenshots and the verbatim verdict trail; the same
trail is on #1374. This document is the consolidated record the build
implements from. The prototype is reference for execution quality:
**port its markup and CSS, never build from an impression of it**
(AGENTS.md, "Building a ratified design"); where this text and the
prototype disagree, this text wins.

It replaces the modal design below ("Superseded: the wizard as a
modal"), which stays as history: its claim-ledger principles carry
forward, its modal, its seven steps and its finish do not.

## The model

The wizard is a **full screen**, not a modal: the bar, the strip, the
step rail, the body, the footer. It is still a **claim ledger** — every
check is an observation, MikroView never connects to the router (the
AGENTS.md invariant), each step ends on evidence or is set aside — and
still **stateless beyond the evidence**: receipts are server-side, so
closing never loses progress and reopening shows the ledger as it
stands.

Five steps:

| # | Step | Character |
|---|---|---|
| 1 | The router | one form — name, address, push Yes / No, backup Yes / No; Next when all four are answered |
| 2 | Mint the token | asks for the password at the moment of minting; the token's life is 15 minutes; Reroll asks again |
| 3 | Paste once | one block built from the answers, the enrol line last; Copy; then the router's turn — the track — until everything chosen has arrived |
| 4 | Tag firewall rules | a rule list proposed from the push; tick, Copy, paste — the only second paste; Skip leaves them dark |
| ✓ | Where setup stands | the ledger: what stands on evidence, what was set aside, Undo per row, undo everything; Add another router; Finish |

**Gated** (owner, round 9): a step ahead of the furthest reached is
locked — dashed number, dim, not clickable, titled "After the step
before it". An earlier completed step is clickable while the run is
still yours to change (before the paste lands); once the router is
answering, the list is the record and nothing goes back. Each row
carries its receipt in the ink of what it records.

**Shown when needed** (the issue's first theme): nothing router-side is
visible before its step. The block exists only once the token is
minted; the tagging block only once the push has read the rule table;
the undo lines only when asked for.

## The way in, and the way out

One journey, played in on Enter at the door and again on Finish, onto
the fall. The door is the real `AuthScreen`: the void, the real
`Fullfall` rain with the centre carved out, the wordmark in its amber
box, underlined fields, the round Enter button. The wordmark's box is
the protagonist; it never disappears.

Six beats on one clock (≈6.5s in; the way out at three-quarters speed):

1. **Let go** (0–0.8s). The welcome and the fields drop away. The box
   moves to the centre of the screen and grows by half. You slide down:
   the welcome goes up and away, the rain streaks with your speed.
   Nothing of the fall is drawn under the door (owner, round 12: "do
   the slide bit but without the fall bits").
2. **The swell** (1.1–2.6s). The rain thickens — more, faster, longer —
   and takes every ink MikroView owns: the fall's three first, then the
   lanes, accept, log, NOW, drop, alarm. **No white wash** (owner: "the
   white flash is removed, as it's really cheesy"); at the top of the
   swell the frame is full of colour, not light. Under that cover the
   door goes and the page stands bare.
   The box is a neon sign. As the swell comes on it **strikes**: the
   letters light one after another in amber, each with a tube's stutter.
   From then on a letter now and then **fails like a broken tube** — a
   buzz, a stutter, a drop-out and a flash back — in whatever ink the
   rain is wearing, instantly, never faded (owner, round 13: "like
   neon/tube lights flicker when they're broken"; round 12: "just
   enough, is enough"). The border wobbles like something alive and
   glides from amber through the rain's inks; border and line hum
   together — the odd dip for a few frames.
3. **The line** (2.5–3.1s). From each side of the box a line grows to
   the screen's edges — the tube extending, a bright head of current at
   each end; when both reach the edges the whole line strikes on.
4. **The catch** (3.1–3.7s). Rain meeting the line is caught or slips
   through: at first almost all of it slips by and the sky below the
   line clears as it falls out; then more and more is caught, until
   nothing passes (owner, round 12). Each drop caught is a bead on the
   line in its own ink, bright as it lands, then part of the wet; the
   line thickens and glows with what it holds. **No sparks** (owner,
   round 15).
5. **The wipe** (3.7–4.6s). The line rises, taking everything it holds
   and everything it meets; below it the place is clean. The box rides
   up with it, leaves it near the top, shrinks and slides left to become
   the bar's wordmark; its border and the flicker are gone by the time
   it lands. The line goes off the top.
6. **The groups** (4.35–5.8s). The wizard arrives in groups, never all
   at once (owner, round 11: "not just 'slap' and the full screen is
   there"): the bar's chips; the strip; the step rows one by one; the
   heading, then the form row by row; the footer. Each **strikes on**
   like a tube — dark, a flash, a dip, then steady (a 420ms blink).
   Then the router's name field has focus.

**Finish** plays the same journey: the box leaves the bar for the
centre and grows; the wizard's body slides up; the swell, the line, the
catch, the wipe; what stands bare under it is the fall's frame, and its
groups are the columns one by one, then the axis and the body, then the
side rail — and the fall is live, with the router flowing, under the
fall's own scene bar (range, LIVE, the account). The wizard's chips and
strip stay through the way out and go with the wizard; they do not carry
onto the fall (owner, 2026-10-01, #1386).

**Reduced motion**: both journeys are a short crossfade; the door's rain
hangs still as the real one does; the groups appear without the strike.

**Then the tour** (owner, 2026-09-30, on #1386): once the way out has
landed on the fall and its groups have struck on, a beat later a small
panel rises at the foot of the screen — the retired glass's own shape
(round 27: one glass over the live fall, never a modal maze) — "Take
the tour?", the deck's count and length, `begin the tour`, and the
quiet "not now — it stays in the account menu". Once per account,
taken or declined. Only a Finish that lands on the fall offers it;
adding a router lands on the fleet and does not. The tour (#646's
beat 6, unchanged) ends back on the card it started from, and the
account menu's "Take the tour" starts it any time, for every role.

## The bar and the strip

The bar carries the wordmark, then **chips** as proofs arrive — the
router's name (decision blue), refused sender (alarm), cert, logs, push,
"N dark" / "every boundary watched", RouterOS ahead of review (amber),
backup, "N rules" — and on the right the live rate and the line count
("no router yet" until the enrol line). Under it the **strip**: one grey
tick until the router speaks; green when logs flow; one tick per
boundary once the push names them. The bar and the strip are the
wizard's; the fall keeps its own scene bar (owner, 2026-10-01, #1386).

## The steps, in detail

**1 · The router.** Lead: "Your first router. Four things, then one
paste. Nothing touches the router until you paste, and MikroView never
connects to it — the router sends." Name (what MikroView calls it on
the fall, the stream and Entities). Its address, checked as you type
with the problem worded under the field (#1380; owner, round 7: "the
router ip block rejects incorrect formatting for IPs"). Push router
state and Back up nightly are each a plain **Yes / No**; the label's
own line already says what each does (owner, round 15: "You don't need
to time/interval as it's already in the description"). Footer hint "All
four, then Next".

*The drop box, when closed* (#1361, Fable, 2026-10-01 — the one
instance-wide fact on this per-router form). Under Back up nightly's
Yes / No, only while the drop box is known to be closed (the backups
read has landed with no port, on an SFTP install): a caution in the
caution ink, "**The drop box is closed** — a backup would have nowhere
to arrive. Open it now, or later from Settings → router backups." "Open
it now" unfolds Settings' own dialog in the wizard's grammar — the
trust caveat, the password (owner, 3a), open / cancel — and on success
the caution gives way to "The drop box is open on port N — the router
must be able to reach this host there." Yes and No stay per-router
answers: a No never closes the drop box, a Yes never opens it, and a
Yes with it still closed is allowed (the caution stays). Not on Paste
once: the choice is made here, and the block is built from it after the
mint; opening from here after Back re-renders the block so its backup
part appears. Nothing is said before the read has landed.

**2 · Mint the token.** Lead: "Your password, to mint <name>'s token.
Minting opens the log port for <address>, for 15 minutes, so it asks for
your password at that moment. The token goes at the end of the block."
One password field; Back; **Mint the token** primary. (#1291's model:
minting is what opens the port, so it asks every time.)

**3 · Paste once.** Lead: "One paste. N parts, in order — <titles> — and
the enrol line last. Into the router's **terminal** (WinBox ▸ New
Terminal, or ssh), not a script." (#1368: never call a terminal block a
script.) The block, sections numbered and titled, in a folded `pre`
with one prominent **Copy**; "Token good until hh:mm (15 minutes) ·
Reroll". The observation line under it: waiting — "Nothing has arrived
yet. Copy, paste, and this line will say what the router did." Next is
disabled until Copy.

**The router's turn** (same step, after Copy). Lead: "The router's turn.
The block is on the router. Each part answers in its own time — every
station that lights is something that arrived." Then the **track**, all
green (owner, round 9: "it should stay just green"): one wire from Copy
to Done, a station per proof — certificate, enrol line, first push,
backup — grey until its event lands, the amber NOW cursor travelling the
gap to the next; set-aside stations dashed and struck. Under it, the
latest arrival's observation line in its ink, then the stream of lines
arriving. When everything chosen has arrived: "<name> is sending.
Everything you chose has arrived; each station stands on evidence."
Next.

Two recoveries live here (the issue's second theme):

- **Refused sender.** Lines arrive from an address that is not the
  enrolled one: a warning box (`--warn`) — "Lines from <other> arrived
  without the enrol line and were refused. If that is this router, its
  logging action is sending from another address — usually an older
  `remote` action, or a `src-address` left from a previous setup. Fix it
  on the router, then paste the last line of the block again:" — with
  the fix and the enrol line as one block to copy, and the alternative
  "Or, if <other> is the address it should send from: **enrol at <other>
  instead**." (#1370, #1373.) The bar shows a "refused · <other>" chip
  while it stands.
- **Ahead of review.** The push reports a RouterOS newer than the
  commands were reviewed on: a caution box — "RouterOS x.y is newer
  than these commands were reviewed on (a.b). They ran and the push
  arrived. If anything reads wrong, this is the first suspect." — and
  an amber chip in the bar.

**4 · Tag firewall rules.** Lead: "N rules log nothing. The push read M
rules; K already log. These N sit on boundaries nobody watches — a
rule, but no line. Tick the ones to tag; this is the only second
paste." A **rule list** (chain · action · rule · why it matters ·
prefix), each row a checkbox; the tagging block below it (or "# nothing
ticked"); **Copy — N rules**, "Safe to paste again; it sets, never
adds." Observation: quiet until Copy ("Nothing to wait for until you
paste — the rule table is already here"), waiting after, counting once
the first new prefix lands. **Skip this step** leaves them dark; the
step's receipt says "left dark". Without the push the step reads "needs
the push".

**✓ · Where setup stands.** Lead: "<name> is sending. N things stand on
evidence; K were set aside. Finish takes you to the fall, with <name>
already flowing." The compact track, then the **ledger**: a row per
thing — certificate trusted, logs flowing, router state pushed, nightly
backup, rules tagged — green with its receipt, or dashed and struck
where set aside ("not now · the fall stays address-only"). Each green
row has **Undo**: the lines to paste on the router, and "MikroView
notices when the lines stop and this row goes back to waiting." Below:
"Admin ▸ Run setup… reopens this ledger any time. To start again from
nothing, **undo everything on the router first**" — which shows every
undo block in order, then "forget <name> on MikroView — its token, its
enrolment and its record" (the issue's "start again": the router side
is undone by pasting, the MikroView side by one act). Footer: **Add
another router** · **Finish**.

## One ink per thing

Every artefact the wizard makes gets one ink and keeps it everywhere it
appears — step-list receipts, chips, the track, the ledger, the fall's
own columns (owner, round 8: "give the 'success' things a mix of
mikroview colours"; round 9: the track alone stays green). All are
`app.css` tokens already meaning the same kind of thing:

| Thing | Ink |
|---|---|
| trust — the router now trusts MikroView | `--accent` |
| logs flowing | `--accept` (the one green, kept for the moment the fall starts) |
| router state pushed | `--fall-nat` (the fall's "the router rewrote it" purple) |
| backup — a clock thing | `--now` (amber is time) |
| the token — a decision you made | `--log` (decision blue) |
| tagged rules | the lane family; each rule's dot wears its own lane |
| not now, set aside | `--fg-dim`, dashed, struck |

The journey adds no colour: the rain, the box, the line and the beads
wear these same tokens.

## Adding a router, re-enrolling

"Add another router" from the ledger, and the Entities routers row's
"+ add a router" berth (#1284), open this same wizard at **The router**;
**Re-enrol…** on a router card opens it at **Mint the token** for that
router with a fresh token. There is no separate router ledger: the
five steps are the router ledger, with the certificate as one of the
paste's parts rather than a step of its own. Refused senders are
handled where they happen (the router's turn) and, as #1284 records,
as cards beside the routers on Entities.

## Keyboard, motion, announcements

Enter at the door starts the way in. In the wizard every control is a
real button with a spoken label; locked steps are `aria-disabled` with
the reason in their title; the address problem is `aria-live`; step
changes are announced. The canvas and the riding box are
`aria-hidden`; the page is `aria-hidden` until the swap. Reduced
motion is above.

## Small screens

Desktop-first, by ruling (#635); the phone form of this wizard is
designed later, with #635.

## Build notes

- Port `round-15/wizard.css`, `wizard.js` (the bodies, the rail, the
  bar, the strip, the foot), `inks.css`, `track.js` and `door.css` as
  drawn; the journey from `an.js` / `an.css` with the sparks removed
  (everything under `spark(`, `sparks` and the burst on the strike).
  `fullfall.css` is `Fullfall.svelte`'s own stylesheet and needs no
  port.
- The journey's rain, the box's border, the line and the beads are a
  canvas; the letters are the DOM. The app's CSP allows it (no inline
  style attributes, nothing fetched). A build decides whether
  `Fullfall` grows into the journey or a canvas serves the journey
  alone; either way the door's rain at rest is `Fullfall` unchanged.
- A fixed canvas with only `inset: 0` keeps its bitmap size as its CSS
  size, so at any device pixel ratio but 1 everything drawn is off by
  that ratio: give it `width: 100%; height: 100%` (round 12's trap).
- The lit and dark letter states are `text-shadow` and `color` set
  inline in the prototype; a build puts them in classes.
- `wizard.js` is a classic script whose top-level `const`s are global;
  the prototype's `WIZ` seam (`showWizard`, `showFall`, `fallLive`,
  `beat`) is the contract the journey drives — in Svelte, the same
  beats become state the journey component reads.
- The prototype's engine (`engine.js`) simulates the router: the data
  story, the scenarios (happy path, wrong address, not-now, ahead of
  review), the state machine. It is the acceptance script for the
  build's live check.
- Wizard truthfulness fixes #371/#374 are check-logic this design
  inherits; the ledger's server-side receipts are unchanged.

## Superseded (considered and closed)

- **Rounds 4–9 under #1374**, each dropped or folded as its verdict
  records: two screenshot sets (T, U — "underwhelming"); "First light"
  (V — "childish almost"); three themed concepts (W, X, Y — "three
  hashed together versions of the same theme"); AA, AB; AC's done
  strip ("weird"); AD's enrolment line as drawn; AF; AE's mini-fall as a
  selector; the modal itself. What survived from each is in this
  document.
- **Round 11's three journeys** (AH the slide, AI the swell, AJ the
  storm — "all kind of the same"): combined into AK (round 12), tuned
  in AL (round 13), taken further in AM (round 14), whose sparks
  ("cheap confetti") went in AN (round 15).
- **Round 14's group strike on chips and strip**: only the rows, blocks
  and footer strike; chips and strip fade.

## Superseded: the wizard as a modal (#487, under #518)

The design ratified 2026-08-23 and built as `SetupWizard.svelte`; the
owner's verdict on it as built (2026-09-26, #1374): "The way the steps
are shown and other stuff just isn't that user friendly. We show a lot
of information before it should be seen ... There are various points
where if you make a mistake there's nothing in the wizard to help you
fix it." Kept here as history. What carries forward is named above:
the claim ledger, statelessness beyond the evidence, the four
observation flavours (waiting · arrived · counting · quiet), the
partial-step warning box in `--warn` (#1132), explicit close, Run
setup… as the same door, the router ledger's minting model (#1284,
#1291). What does not: the modal and its geometry, the seven-step list,
the heavy warning on Next, the finish readback as a modal page, the
phone body⟷ledger flip.

### The model (as it was)

The wizard was a **modal over the ratified shell** and a **claim
ledger**: every check an observation, each step ending in exactly one
of **done** (green, with its receipt), **skipped** (quiet), or **forced
past** (amber, recorded). Five steps honest to `setupsteps.ts`: Trust
the certificate · Send logs · Tag firewall rules · Push router state ·
Name your router — later seven with the router ledger (#1284): Name ·
Send logs (mint on the operator's say-so, #1291) · Tag · Push · Back up
· Register, with the certificate in front.

### Anatomy (as it was)

One anatomy per step body: lead sentence · the router-side command
(with Copy) · the observation line. Modal 940px wide (94% cap), 224px
step list. Header: step x of 5 · title · ✕. Footer: Back · Skip this
step · Next (primary), with "Next checks what has arrived" on waiting
steps. Auto-launch once on the first admin sign-in with no router
sending; explicit close only (✕, Esc; no click-outside); relaunch from
Admin ▸ Run setup… at the first step still waiting. Next on a waiting
step showed the **heavy warning** in place — forcing past recorded
amber, reachable by diagnostics. Finish read the ledger back with one
primary out to the fall. Below the pointer-width breakpoint the modal
was the screen, with a body⟷ledger flip.

### Superseded before this (as it was)

- **The 640px modal** (round 1 as first posted): grown to 940px within
  round 1 ("use more of the screen").
- The old wizard page's ephemeral, restart-from-scratch behaviour and
  its absence of close/skip affordances: replaced by the ledger model.

Written by Fable 5.1, 2026-09-27 (the modal design: Fable 5.1,
2026-08-23; the router ledger: 2026-09-19).
