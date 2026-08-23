# Wizard as modal (#487, under #518) — round 1

One direction this round (letters run on from the navigation rounds).

## Direction S — the observed setup

The wizard becomes a modal over the ratified navigation shell (Q V,
carried forward verbatim). The organising idea: every check the wizard
runs is an **observation** — mikroview never connects to the router —
so the modal is a claim ledger. Each step ends done (green, with its
receipt: what arrived, when, from where), skipped (quiet), or forced
past (amber, recorded).

Scenes, dark and light (light is the token swap; identity is #492's):

1. **Auto-launch at first run** — shell paints first, modal opens on
   step 1; explicit close only (✕/Esc), click-outside does nothing.
2. **The step anatomy** — lead sentence · router-side command · the
   observation line; shown waiting, arrived, and on step 4 (the one
   step that takes input: device, token, push script).
3. **Forced past, recorded** — Next with a waiting check raises the
   heavy warning; "Keep waiting" is primary; the amber button quotes
   the exact record it writes. Second frame: the Stream's empty state
   quoting that record back — the diagnostics surface for #487's
   done-when.
4. **Close early, come back** — empty states name their missing
   prerequisite and point at Admin ▸ Run setup…; relaunch reopens the
   ledger at the first step still waiting, evidence arrived meanwhile
   already green.

Flow model is the owner-recorded one on #487 (steps back/next/skip,
explicit close only, forced-past validations recorded, relaunch from
Admin ▸ Run setup…). Step list and checks are honest to the current
implementation (Setup.svelte / setupsteps.ts): trust certificate ·
send logs · tag rules (can only count up, never fail) · push state ·
name your router (conditional). Skip is quiet; forcing past a waiting
check is loud — that distinction is this direction's voice.

Data story: the same world as the navigation rounds, on day one —
rb5009 at 192.168.11.1, MikroView at 192.168.11.30; the nav rounds'
"first in 41 days" quiet starts counting when this afternoon's logs
start arriving. No new data palette this round: surfaces reuse the
validated navigation tokens (light lane steps recorded in
`../../navigation/round-3/README.md`); the only new semantic colour is
the existing `--warn` amber for forced-past.

## Verdicts

Owner, 2026-08-23, on direction S: **"Wizard looks great, but use more
of the screen, no need to squash things in."** — accepted, with a size
correction. Applied in this round's files the same session: the modal
grew from 640px to 940px (94% cap), the step list to 224px, type and
spacing up a notch throughout. The direction, anatomy and wording
stand unchanged; direction S carries into the design record / round 2
at the larger size.
