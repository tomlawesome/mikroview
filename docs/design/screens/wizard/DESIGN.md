# Wizard as modal — the ratified design (#487, under #518)

Ratified by the owner across two rounds, 2026-08-23 (`round-1/`,
`round-2/` beside this file carry the mockups, screenshots and the
verbatim verdict trail; the same trail is on #518). This document is
the consolidated record the build implements from. The mockups are
reference for execution quality; where this text and a mockup detail
disagree, this text wins.

## The model

The wizard is a **modal over the ratified shell** (navigation record,
`../navigation/DESIGN.md`), and the modal is a **claim ledger**: every
check is an observation — mikroview never connects to the router (the
AGENTS.md invariant) — so each step is a claim about what has arrived,
and ends in exactly one of: **done** (green, with its receipt: what
arrived, when, from where), **skipped** (quiet), or **forced past**
(amber, recorded). The wizard is **stateless beyond the evidence**:
receipts are server-side state, so closing never loses progress,
"finished" is not stored anywhere, and reopening always shows the
ledger as it stands.

Five steps, honest to the implementation (`Setup.svelte` /
`setupsteps.ts`):

| # | Step | Check character |
|---|---|---|
| 1 | Trust the certificate | waiting → arrived (ca.crt fetched) |
| 2 | Send logs | waiting → arrived (TLS handshake; a failed handshake never counts) |
| 3 | Tag firewall rules | counting — can only count upward; Next always free |
| 4 | Push router state | acts (token created on entry, its own audit line) then waiting → arrived (first push) |
| 5 | Name your router | conditional, informational — config-file work, nothing to wait for; primary is **Finish** |

Step 5's row always exists (stable count of five) marked "nothing to
name" until the push surfaces an unnamed device; only then does it
gain a body.

## Anatomy

One anatomy for every step body: **lead sentence · the router-side
command (with Copy) · the observation line.** Four observation-line
flavours, the complete set: waiting (dashed, patient — never the word
"error"; nothing is wrong, nothing has arrived) · arrived (green,
dated, sourced) · counting (green, growing) · quiet (plain, "nothing
to wait for"). The step list at the left carries each step's receipt
sub-line for the wizard's life.

Modal geometry (round-1 verdict "use more of the screen"): 940px wide
(94% cap), 224px step list, roomy type. Header: step x of 5 · title ·
✕. Footer: Back · Skip this step · Next (primary), with the hint
"Next checks what has arrived" on steps that have a waiting check.

## Launch, close, relaunch

- **Auto-launch, once**: first admin sign-in with no router sending —
  the shell paints first (rail, ghost rows, honest empty states),
  then the modal opens on step 1. Viewers never see it (#490: Run
  setup… is absent for them; there is no read-only wizard).
- **Explicit close only** (owner-recorded): no click-outside
  dismissal. ✕ and Esc both close; the ✕'s label says where setup
  lives afterwards. Closing early leaves each affected surface's
  empty state naming its missing prerequisite and pointing at
  **Admin ▸ Run setup…**.
- **Relaunch is the same door**: Run setup… (an action, not a page —
  nav record) reopens the ledger at the first step still waiting;
  evidence that arrived while closed is already green.

## Next, skip, and forcing past

- **Next runs the check** where one exists: arrived → proceeds;
  waiting → the **heavy warning** takes the step body in place:
  mikroview cannot check the router's side, only report nothing has
  arrived; "Keep waiting" is the primary; the amber button ("Go on
  anyway — recorded") quotes the exact record it will write:
  `setup · step N forced past · <what was not observed> · <who> ·
  <when>`. No third option, no "are you sure".
- **The record is the feature**: the forced-past line surfaces in the
  step list, the audit log, and every empty state whose silence it
  explains (e.g. the Stream's). Forced ≠ failed: if evidence later
  arrives the step flips to done and empty states clear; the line
  stays in the audit log as history, not a scar the interface keeps
  pointing at.
- **Skip is quiet, force is loud**: Skip records "skipped by <who> ·
  <when>" and moves on — no ceremony. In the ledger a skipped step is
  a dashed row stating its consequence ("the stream stays
  address-only"), never a reproach. Dashes are quiet, amber is loud —
  the two stay visually distinct.

## The finish

Finish reads the ledger back: headline ("Logs are flowing. Four steps
stand on evidence; one was skipped."), one row per step — receipt or
honest gap — and a quiet line noting Run setup… reopens any time. One
primary leads out, to the fall (the ratified landing). ✕ does the
same.

## Small screens

Below the pointer-width breakpoint the modal is the screen: a
full-bleed sheet, no veil, same explicit close. The header compresses
to step-button · title · ✕; the **step list becomes a view of its
own** — the step button (a real button: "Show setup steps" / "Show
this step") flips body ⟷ ledger, full width, same receipts. Footer
keeps all three actions. Commands come pre-broken to phone width so
nothing scrolls sideways; Copy matters more here, not less.
Auto-launch is unchanged; the sheet behaves like the house half-sheet
(focus trap, Esc/back closes — explicitly).

## Keyboard, motion, announcements

Focus is trapped in the modal; Tab order is steps → body → footer
(header → body → footer on phones). Step changes are announced
("Step 4 of 5 — Push router state — waiting for the first push").
Every control is a real button with a spoken label. Waiting-dot
pulses stop under reduced motion; the phone body⟷ledger flip becomes
instant.

## Build notes

- The wizard page route (`setup` view) is removed wholesale with this
  build (#487's done-when); the rail's Run setup… action targets the
  modal from then on (interim call on #548 retires with it).
- Step 4's token creation is mikroview-side with its own audit line;
  watching for the first push is the step's arrival evidence.
- The forced-past record must be reachable by diagnostics surfaces —
  that is the done-when's "visibly recorded" clause.
- Wizard truthfulness fixes #371/#374 are check-logic this design
  inherits; they land before or with it.

## Superseded (considered and closed)

- **The 640px modal** (round 1 as first posted): the owner's size
  correction ("use more of the screen, no need to squash things in")
  grew it to 940px; applied within round 1.
- The current wizard page's ephemeral, restart-from-scratch behaviour
  and its absence of close/skip affordances: replaced wholesale by
  the ledger model above.
