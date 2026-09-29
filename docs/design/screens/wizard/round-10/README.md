# Setup wizard — round 10 (#1374): the fall rises

Owner, on round 9 (verbatim in `../round-9/README.md`): AF dropped; AE
kept, with: no "fake fall thing as a selector", the red-struck bits gone;
the track "should stay just green"; "1-4 could easily be condensed into
a single step"; the step list gated ("users shouldn't be able to click
the next steps ... until they've done the one before it. They should be
able to click preceeding steps after they've completed one"); and "AE as
a full screen, with a beautiful transition between first login and
successfully completing the wizard. I don't just mean a fade in/out. I
mean something like Orbit's login/out animation journeys". Then, during
the build: "To be clear do NOT COPY Orbit", "It must be completely new,
unique", "I am NOT asking you to port Orbit's journey to Mirkoview, it
is _sinpiration only_".

So: nothing of Orbit's is here — no mark ride, no name on the void, no
flight. The ambition is borrowed (a journey with a story, not a fade);
the material is MikroView's own: the door's rain, the amber box round
the wordmark, the boundary strip, the NOW line, the fall's columns.

Served from the design host, port 8309:

| | Concept | Open |
|---|---|---|
| **AG** | **The fall rises** — AE, full screen, with the way in and the way out | `http://192.168.11.30:8309/screens/wizard/round-10/ag-rises.html` |

Start at the door: Enter (or ⏎). Fill the one router step, Next, your
password, Copy, watch, tag, Finish. The prototype strip is bottom right:
scenario, speed, Restart. `?skipdoor` opens straight on the wizard.

## The way in (≈1.6s)

The door is the real one: the void, the fall raining across the whole
screen with the centre carved out, the wordmark in its amber box,
underlined fields, the round Enter button. On Enter:

1. The fields and the welcome drop away (0–400ms).
2. The rain lifts — every drop rises and thins — and gathers into one
   line across the top of the page (200–1500ms). That line is the
   boundary strip the wizard then wears: grey, one tick, until the
   router speaks; green when logs flow; six ticks once the push names
   the boundaries.
3. The amber box carries the wordmark up to the top-left and settles as
   the bar's wordmark; the amber fades as it lands (260–1340ms).
4. The step rail slides in from the left (600ms); the first step rises
   (1250ms).

## The wizard, full screen

No modal. Top bar (wordmark, chips as proofs arrive, live count),
the strip under it, the step rail on the left, the body, the footer.

- **Five steps**: The router · Mint the token · Paste once · Tag
  firewall rules · Where setup stands. The router step is one form:
  name, address (checked as you type), push yes/not now, backup yes/not
  now. Next enables when all four are answered.
- **Gated**: a step ahead of the furthest reached is locked (dashed
  number, dim, not clickable, "After the step before it"). A completed
  earlier step is clickable while the run is still yours to change
  (before the paste lands); once the router is answering, the list is
  the record and nothing goes back.
- **The track**, all green: a wire from Copy to Done, a station per
  proof, grey until it lands. The router's turn shows it live with the
  NOW cursor; the ledger shows it compact.
- **Tagging is a rule list** again (round 7's): chain, action, rule,
  why it matters, prefix; tick, copy, paste.
- **The ledger**: green rows with undo; set-aside rows dashed and
  struck. **Finish**.

## The way out (≈1.9s)

1. The ledger and footer fold away; the rail withdraws left (0–600ms).
2. The strip's ticks spread apart, and from under each one its column
   header drops into place, staggered left to right (700–1500ms) — the
   line the rain gathered into on the way in is now the top of the
   fall.
3. The NOW line draws across in amber and the rain begins under it, in
   the boundaries' own columns, from the lines the router has already
   sent (700ms on).
4. The right rail of section names slides in (1700ms). It is the fall,
   with the router flowing and the top bar's chips as the record.

Reduced motion: both journeys collapse to short crossfades; nothing
moves.

## Gates

- `drive.mjs` runs the door, every stage, the way out, the not-now
  path, the wrong-address recovery and ahead-of-review, and asserts
  that a locked step is not clickable; no console errors. `shots/` is
  the record. Frames 02–03 and 15–17 are the journeys mid-flight.
- No new colour; every ink is an `app.css` token. The track is
  `--accept` only.

## Honest gaps

As round 9: the boundary set is the story's; the fall is a rendering of
the fall's grammar, not `Fall.svelte`. The door's rain is a static field
of marks (the real `Fullfall` animates). "Add another router" and
Reroll are alerts. The transitions are CSS on transform/opacity, timed
by `setTimeout` beats; a build would drive them from the same state.

## Verdicts

Owner, 2026-09-27 (verbatim):

> needs a lot more work, its very boring andthe rain looks wird/not
> liketheproper thing. try a verion where the user slides down the fall,
> and a version where the rain increases and gets more colorful and
> vibrant with increasing intensity until it fills thescreen, before
> reducing to nothing to reveal thescreen. then try one wild, brave awe
> inspiring incredible option too, free reign on the third one - go nuts

Read as: AG's wizard (the one router step, the gated rail, the green
track, the rule list, the ledger) stands; the *journeys* are the work.
Round 11 draws three ways in: the slide down the fall, the rain that
swells to fill the screen, and one unconstrained. The door's rain must
be the real `Fullfall`'s, not a static field of marks.

Written by Fable 5.1, 2026-09-27.
