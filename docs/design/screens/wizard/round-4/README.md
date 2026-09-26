# Setup wizard refinement — round 4 (#1374, round 2 of the dialogue)

2026-09-26. Round 3 (`../round-3/`) is the wizard as built, screenshotted
in every state. This round draws the fix. Direction **T**: the ratified
ledger model (`../DESIGN.md`) kept whole — modal, step list with
receipts, one anatomy per step, the four observation flavours, Skip
quiet and force loud, the finish readback — and re-sequenced so that a
step shows one thing at a time and every wait knows how to help.

Owner rulings this round builds on (verbatim on #1374, 2026-09-26):

- "Version picker — we should enforce the user picking the version
  right at the start. Proceeding is gated on it."
- "The very first wizard screen should be asking the required
  information for the wizard to complete the minimum setup, with
  mandatory and optional fields marked clearly."

Owner findings it answers (#1374 body): shown too early; no way back
from a mistake; no help anywhere else. And the owner's brief for the
round (verbatim on #1374, 2026-09-26): "Logical progress through the
process, show the right information at the right time … Don't keep
showing them things we categorically know they've done. Give the user as
few steps as possible … ask for as little information as we can and
don't ask for the same information again later … as slick and
professional as possible. It's an on boarding process."

Five rules every scene is checked against: **one thing at a time**;
**never before it is needed**; **never again once it is known**; **ask
once, reuse everywhere**; **check everything we can, and say so**.

## The data story

Unchanged from round 3: the instance answers on `192.168.13.15:8080`;
the router is `rb5009` at `192.168.13.1`, RouterOS **7.16.2**; its
other address is `192.168.254.1`. Clock: the same afternoon, 14:02
onwards. Every scene is this one router on this one instance.

## T — what changes

### 1. "Before we start" is the first screen

One form, before step 1, and nothing proceeds until the mandatory
fields are filled. Each field carries **Required** or **Optional** as a
small label after its name — the word, not an asterisk. In order:

| Field | Label | Prefilled | Help line under the field |
|---|---|---|---|
| RouterOS version | Required | none — a select whose first row is the prompt "Pick your RouterOS version"; there is no "not sure" row | "Every command below is written for this release, and MikroView checks it against the version the router reports later." |
| Address the router reaches MikroView on | Required | the page's own host and port | "The router has to reach this address — a proxy, a second interface or a mapped port all change it." (today's line, moved here) |
| Router name | Required | empty | "How the router appears everywhere in MikroView. Letters, digits, dashes." |
| The router's own address | Required | empty | "MikroView opens the log port for this address only, until the router's first line arrives." |
| Your password | Required | empty | "Opening that port is what asks who you are. Used now, never kept." |
| Back up the router nightly | Optional, a switch, off | — | "Needs a key file on the MikroView machine. Off leaves the backup step in the list as skipped." |

Primary button **Begin setup**, disabled with a plain reason beneath it
while anything Required is empty ("Pick a RouterOS version to begin").
Pressing it does what steps 2 and 3 do today: creates the router record
and mints the enrolment token. Footer has ✕ only; there is no Back.

### 2. The told strip replaces the global address field

The address field that today sits above every step is gone from the
steps. In its place, under the header on every step and on the ledger,
one line in the quiet secondary colour:

> Told: RouterOS 7.16.2 · MikroView at 192.168.13.15:8080 · rb5009 at
> 192.168.13.1 · **Change…**

**Change…** reopens the first screen with the values filled in. Saving
a change re-prints every affected block and marks each one **Paste
again** (amber word on the block's caption) until its evidence
re-arrives. This is #1370's "the block changed under you" made visible
everywhere, not only on Send logs.

### 3. One step, three moves

Every router-side step body becomes an ordered sequence of moves. Only
the current move is open; earlier moves collapse to one receipt line
with a tick; later moves are dimmed to their heading and a phrase
saying what they need.

1. **Paste** — a lead sentence of at most two lines; the block; under it
   one caption line: *Paste into the router's terminal (WinBox: New
   Terminal; WebFig: Terminal) · creates the mikroview logging action
   and its rule, then logs the enrol line* (the #1368 hint and the
   one-line "what it does", joined); one **Copy** button. Copy is what
   advances to move 2 — the step does not wait for a click on Next.
2. **Wait** — the observation line exactly as today (dashed, patient),
   and directly under it a closed drawer: **Nothing yet? ▸**. Nothing
   else in this move.
3. **Receipt** — the arrived line as today (green, dated, sourced) plus
   one link, **Undo this step…** (§5).

Tokens are never shown on their own. The Push step's token box and
"Copy token" go: the token is inside the block, and the caption says
"carries this router's push token, so keep the paste to this router".
A step that is already done opens on its receipt alone — the Paste
move stays collapsed and is re-opened only by Undo. The wizard opens on
the first step that is not done (ratified), so a returning operator
never sees a block they have already pasted.

### 4. The "Nothing yet?" drawer

Open, it has two short lists and nothing else:

**What MikroView checked** — one line per check, a tick or a cross,
each cross followed by the fix in the same breath:

- ✓ The certificate covers 192.168.13.15.
- ✓ The log port is open for 192.168.13.1 until 14:17.
- ✗ Lines are arriving from **192.168.254.1** and being refused. If
  that is rb5009, its logs leave from that address, not the one you
  told me → **Use 192.168.254.1 instead** (one button; it changes the
  router's own address and re-mints).
- ✗ The block above was printed for 192.168.13.15:8080, but the
  address has changed since → **Paste again** (re-prints).

**Only you can check** — questions, each with one plain sentence:

- Did the paste go into the terminal, not System ▸ Scripts ▸ Source?
  (Scripts stores the text without running it.)
- Did the paste end with the `/log info "mikroview-enrol …"` line?
  (RouterOS accepts the earlier lines silently without it.)
- Has the token lapsed? Token good until 14:17 · **Reroll**.

The drawer's checks are the ones the server already knows how to make
today (the #442 source split, #1370's stale block, the enrolment
window, the certificate's covered hosts). Nothing here asks MikroView
to reach the router — it prints, it never probes.

### 5. Undo this step, and Start again

**Undo this step…** opens in place, under the receipt, two short
blocks:

- **On MikroView** — what clears, stated exactly: "Forgets rb5009's
  enrolment. Its logs are refused until it enrols again. Its name,
  tokens and tables stay." Button: **Undo** (secondary colour, one
  click, no second confirm — the sentence *is* the confirmation).
- **On the router** — the command that removes what this step's block
  added, in a copy box with the terminal caption. Optional: MikroView
  cannot tell whether it ran.

At the foot of the ledger (finish pane), one link: **Start setup
again…** — the same two blocks, listing every step's undo in order,
one Undo button for the lot, and the router-side commands as one
block. What "start again" clears is exactly the union of the per-step
undos; nothing else is ever touched.

### 6. The reported-version check on the ledger

When the router's first push reports a version other than the one
told, the ledger (step list and finish pane) gains one amber line
under the Told strip:

> You told me 7.16.2; rb5009 reports 7.24.4. Upgrade warnings were
> re-checked — 1 applies to Send logs · **Show**

A warning, never a stop. **Show** opens that step at its Paste move
with the #1344 warning above the block.

### 7. Fewer steps

"As few steps as possible" (owner, 2026-09-26). Two steps go:

- **Trust the certificate + Send logs → one step, "Connect the
  router".** One paste: the certificate fetch and import, then the
  logging action and rule, then the enrol line — the second half cannot
  succeed before the first, and both go in the same terminal. The
  receipt is two lines: *certificate fetched by 192.168.13.1 · 14:02*
  and *enrol line arrived from 192.168.13.1 · 14:03*; the Wait shows
  whichever has not arrived, so diagnosis loses nothing. The "Nothing
  yet?" checks cover both halves.
- **Register the router → the finish pane's primary.** It records
  intent and grants nothing (DESIGN.md), so it is a confirmation, not a
  step: the finish pane's primary reads **Confirm rb5009 is set up**,
  with the ratified "Take me to the fall" as the secondary and the
  ledger unchanged. Naming already moved to the first screen (§1).

First run is then: *Before we start → 1 Connect the router → 2 Tag
firewall rules → 3 Push router state → 4 Back up the router (optional,
present only if switched on at the start) → Where setup stands.* Four
pastes, one form, one confirmation. The step list shows four or three
rows accordingly, never a dimmed placeholder for a step the operator
switched off. This departs from DESIGN.md's seven and is drawn here for
ratification; the router ledger (#1284) inherits the same shape minus
the certificate half.

### 8. Visual weight

Owner, 2026-09-26: "Visually, the wizard looks rather cluttered, too. It
would be nice if it was a much more impressive/visually appealing
experience." The app's own dark voice stays; the weight comes out:

- **Blocks fold.** A command block shows its first line and *… 6 more
  lines* in the secondary colour, with **Copy** as the one prominent
  control beside it (primary style, the only primary in the body). The
  operator's job is to copy, not to read; a click on the folded lines
  expands them in place. The caption (§3) sits under the folded block.
- **One reading column.** The step body is a single column of about
  640px measure, left-aligned, with generous vertical space between
  moves. The step list slims to a quiet rail: number-in-ring, title,
  and a receipt sub-line of at most one short line ("14:02 ·
  192.168.13.1"); full receipts live on the finish pane.
- **One box at a time.** No move has more than one bordered box open:
  the block, or the drawer, or the undo panel. Everything else is type
  on the background — no boxed notes, no boxed hints.
- **Two lines, then stop.** Each move has a lead of at most two lines
  and, if needed, one secondary line. Paragraphs of explanation move to
  the drawer or to the docs link at the foot of the step.
- **The one motion.** When evidence arrives the waiting dot resolves
  into the green tick (about 300ms), the receipt line slides in under
  it, and the next move opens by itself. This is the wizard's only
  motion and its reward; under `prefers-reduced-motion` it is instant.
- **A lead on the first screen.** Above the form, one line: "Four
  pastes and your router is talking to MikroView." (three when backup
  is off — the count is live). Nothing else decorative.
- **No new colours.** Green for arrived, amber for warnings and *Paste
  again*, the reject red for blocked, as the app already uses them.

## U — the full-screen guided flow

Owner, 2026-09-26: "you're free to ENTIRELY redesign the wizard in terms
of visual appearance/layout etc, if you think it would be useful/better."
U keeps every rule above (§§1, 3–8: the first screen, the moves, the
drawer, undo, the version check, fewer steps, the visual weight) and
changes the frame:

- **The whole screen.** No modal, no veil, no step list at the side.
  Setup is the page: the app's background, and one centred column of
  about 600px starting in the top third. First run has nothing else to
  show; a relaunch from Run setup… is an action and may take the screen
  too. **Leave setup** (✕ with its label) top right — explicit close,
  as ratified; Esc does the same.
- **A progress rail across the top.** Centred, one small-caps word per
  step joined by a hairline: *Before we start · Connect · Tag rules ·
  Push · Back up · Done* (Back up absent when switched off). Done steps
  carry a small green tick; the current one is bright; later ones dim.
  Under the rail, the Told strip (§2) in one quiet line.
- **One screen, one job.** A heading in the display size the app uses
  for a card title (about 28px), a lead of one line, then the current
  move (§3) — and only the current move. Earlier moves are not shown on
  this screen at all; their receipts live on the rail's tick and on
  Where setup stands. The block folds (§8) with a large **Copy** as the
  screen's only primary.
- **The wait is the screen.** In move 2 the observation line is set
  large, with the pulsing dot, and under it **Nothing yet?** as a text
  button that opens the §4 drawer in place. When evidence arrives the
  dot resolves, the receipt slides in, and after a beat the next
  screen slides in from the right (reduced-motion: cut). Continue is
  automatic; a manual **Continue** sits in the footer for the operator
  who wants to read the receipt first.
- **Footer.** Three quiet text controls: **Back** · **Skip this step** ·
  **Continue** (enabled by evidence; without it, Continue opens the
  ratified heavy warning — same words, same amber, same record). No
  hint text: the wait screen already says what it is waiting for.
- **Where setup stands** is the last screen and the ledger: one row per
  step with its full receipt or honest gap, the §6 amber line if the
  reported version differs, the primary **Confirm rb5009 is set up**,
  secondary **Take me to the fall**, and **Start setup again…** at the
  foot. Run setup… opens here when everything is done, and on the first
  step that is not done otherwise.
- **Small screens** need no separate treatment: the column is the
  screen, the rail wraps to two lines, blocks stay folded.

## Scenes (`direction-t-wizard.html`, `direction-u-wizard.html`)

Both files prove the same nine scenes on the same story; U's are
`#u1`…`#u9`, shots `u-<scene>-dark.png`.

| # | Scene | Shows |
|---|---|---|
| t1 | Before we start, empty | the form; Begin disabled with its reason |
| t2 | Before we start, filled | all Required filled, backup switch off; Begin enabled |
| t3 | Connect the router · Paste | told strip; slim rail of §8 (four rows); move 1 open with the block folded and Copy primary; moves 2–3 dimmed |
| t4 | Connect the router · Wait | move 1 collapsed to "Copied 14:02"; observation line; drawer closed |
| t5 | Connect the router · Wait, drawer open | the two lists of §4, with the 192.168.254.1 cross |
| t6 | Connect the router · Receipt, undo open | the two-line receipt; §5's two blocks |
| t7 | Push · Paste, block expanded | the folded block opened by a click, token inside, one Copy, caption |
| t8 | Where setup stands | four-row ledger with the §6 amber line; primary **Confirm rb5009 is set up**, secondary Take me to the fall; Start setup again… at the foot |
| t9 | Before we start, backup off → step list | the step list as it first appears after Begin: three rows plus Where setup stands, nothing dimmed for the switched-off backup |

## What is carried forward verbatim

Modal geometry (940px, 224px step list), header (step x of N · title ·
✕), footer (Back · Skip this step · Next with its hint), the step list
with receipt sub-lines, the four observation flavours and their
colours, the partial-step warning box, the heavy warning on forced
Next, the finish headline and readback rows, the "Run setup… reopens
this any time" line. Not redrawn, not reworded.

## Gates before the owner sees it

Screenshots of all nine scenes looked at and clean; no new data
colours (amber for warnings and the Paste-again word only, as the app
already uses it); `prefers-reduced-motion` respected on the drawer;
every control a real button with a label.

## Shots

| Scene | File | URL |
|---|---|---|
| t1 | `shots/t-t1-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t1-dark.png |
| t2 | `shots/t-t2-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t2-dark.png |
| t3 | `shots/t-t3-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t3-dark.png |
| t4 | `shots/t-t4-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t4-dark.png |
| t5 | `shots/t-t5-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t5-dark.png |
| t6 | `shots/t-t6-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t6-dark.png |
| t7 | `shots/t-t7-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t7-dark.png |
| t8 | `shots/t-t8-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t8-dark.png |
| t9 | `shots/t-t9-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/t-t9-dark.png |
| u1 | `shots/u-u1-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u1-dark.png |
| u2 | `shots/u-u2-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u2-dark.png |
| u3 | `shots/u-u3-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u3-dark.png |
| u4 | `shots/u-u4-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u4-dark.png |
| u5 | `shots/u-u5-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u5-dark.png |
| u6 | `shots/u-u6-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u6-dark.png |
| u7 | `shots/u-u7-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u7-dark.png |
| u8 | `shots/u-u8-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u8-dark.png |
| u9 | `shots/u-u9-dark.png` | http://192.168.11.30:8309/screens/wizard/round-4/shots/u-u9-dark.png |

Index: http://192.168.11.30:8309/screens/wizard/round-4/index.html

## Verdicts

None yet.

Written by Fable 5.1, 2026-09-26.
