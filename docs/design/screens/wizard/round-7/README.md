# Setup wizard — round 7 (#1374): three ways to ask, one block to paste

Owner, after round 6 (verbatim in `../round-6/README.md`): none of the
three "tried to tie in with Mikroview's visual identity and existing
design language", none "told a story, or pulled the user through an
exciting journey", and all three used "the same style of forms and
entry". Then the direction: **"Why don't we just ask the user which
features they want, one by one, then build the copy & paste block as one
thing?"**

So this round starts from the app, not from a theme. `shell.css` is
`frontend/src/app.css`'s Atlas II token block and `SetupWizard.svelte`'s
own modal, header, step list, body, `pre`, buttons and footer, ported
rule for rule; the observation line's four flavours and the #1132
warning box are drawn as the ratified wizard draws them. Every concept
sits in that 940px modal over the ratified shell. What differs is **how
the operator answers** — a form, the script itself, or a dealt card —
because that is where the owner said round 6 did not differ.

Same journey in all three, from the owner's direction: five questions
one at a time (name · address · push router state? · back up nightly? ·
your password mints the token) → one block, whole, shown only then →
one paste → the router answers, one proof at a time → rule tagging
proposed from the push as the only second paste → a ledger that stands
on evidence, with Undo per row and a way to start again. The RouterOS
version is never asked: the push reports it (owner, 2026-09-26, "drop it
and read instead").

Served from the design host, port 8309:

| | Concept | Open |
|---|---|---|
| **Z** | **The ledger asks** — the ratified wizard, one question per step | `http://192.168.11.30:8309/screens/wizard/round-7/z-ledger.html` |
| **AA** | **The block writes itself** — answer inside the script | `http://192.168.11.30:8309/screens/wizard/round-7/aa-block.html` |
| **AB** | **The sitting** — Entities' dealt cards, Enter / Space | `http://192.168.11.30:8309/screens/wizard/round-7/ab-sitting.html` |

Drive each: answer the five questions, Copy, watch. The dashed strip at
the bottom left is prototype furniture only: scenario (happy path ·
router sends from another address · RouterOS ahead of review), speed,
Restart. `?demo` on any URL prefills every answer.

## Z — The ledger asks

The shipped modal exactly: step list at 224px, body, footer. The step
list holds seven rows — the five questions, Paste once, Tag firewall
rules — and Where setup stands. The body asks one question at a time,
with what Yes gives and what Not now costs under the two buttons, and a
"So far" line reminding that nothing has touched the router. Each
answer writes a receipt into its row in the decision ink (`--log`;
dashed for not now, as a skipped step is drawn today). The block appears
only at row 6, with a comment line per part. After the paste the rows
flip to `--accept` one by one as each proof arrives — the piece of the
round-6 Analyzer the owner called "kind of fun", carried into the
ledger. The refused-sender case is the #1132 warning box with the exact
`src-address` fix line and "enrol at … instead". Done is the ledger
read-back with Undo per row.

## AA — The block writes itself

No step list: the paste block is the ledger for the whole journey. It
opens as a skeleton — a heading line per part, upcoming parts folded to
"7 lines — filled in when asked" — and the questions are asked in it: a
caret and a question card sit at the current part; the name is typed
into the block's first comment line, the address into `src-address=` on
the logging line; Yes inks a part's lines, Not now folds it to one dim
comment with "add it" beside; the password mints the token straight into
the last line. Copy wakes when the block is whole. After the paste the
left margin becomes the proof margin — a mark beside each part as the
router answers, the refused case pinned under part 2 — and at the end
Undo lives beside each part. One artefact, start to finish.

## AB — The sitting

Entities' ratified naming flow, borrowed whole: each question is dealt
as a dossier card, one per screen, showing in the app's own fragments
what saying yes gets you — the fall's bands named vs "other traffic",
the routers row with its refused twin, the backups list, the stream row
carrying the name. `Enter` says yes and deals the next; `Space` says not
now; `Esc` closes with nothing lost. Answered cards become slips in a
row above the deck (click one to re-deal it). The block is the last card
dealt; then "the router's turn" — it deals its own card as each proof
arrives, and the slips flip green — and the deck runs dry on the ledger:
"the deck is empty · setup stands on evidence".

## The data story

MikroView at `192.168.13.15:8080`; `rb5009` at `192.168.13.1`, other
address `192.168.254.1`; Copy at 14:02:50, certificate 14:02:58, enrol
14:03:04, first push 14:03:21, backup 14:03:22, token good until 14:17.
RouterOS **7.24.4** read from the push (7.25.1 in the ahead-of-review
scenario, reviewed set 7.24.2). 47 filter rules, 3 logging (`in-ssh`,
`est-rel`, `icmp`), five proposed (`fwd-drop`, `wan-in`, `nat`,
`guest-lan`, `wg-in`). Same story as round 6 (`engine.js` is round 6's
engine re-cut around the questions).

## Gates

- `drive.mjs` runs all three through the happy path, the not-now path
  (no push, no backup), the wrong-address recovery and ahead-of-review;
  no console errors. `shots/` is the record of that run, not the thing
  to review.
- Reduced motion honoured (waiting pulse, caret, deal off).
- No new colour: the app's tokens only. The fall fragment in AB uses
  `--fall-accept` / `--fall-drop` / `--fall-other`, already validated for
  the fall.

## Honest gaps, for whichever is chosen

The push carries no board name; the rule proposals assume the
tune-logging analyser can run on the pushed table alone; "Open the
fall" and "Add another router" are alerts here; the certificate part is
shown as given rather than asked (it is not optional). Token expiry and
Reroll are drawn but not simulated.

## Verdicts

None yet.

Written by Fable 5.1, 2026-09-26.
