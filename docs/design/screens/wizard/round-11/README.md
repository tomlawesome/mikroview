# Setup wizard — round 11 (#1374): three ways in

Owner, on round 10 (verbatim in `../round-10/README.md`): "needs a lot
more work, its very boring and the rain looks weird/not like the proper
thing. try a version where the user slides down the fall, and a version
where the rain increases and gets more colorful and vibrant with
increasing intensity until it fills the screen, before reducing to
nothing to reveal the screen. then try one wild, brave awe inspiring
incredible option too, free reign on the third one - go nuts".

Two things carried forward unchanged: **the wizard itself** — round 10's
AG (the one router step, the gated step list, the all-green track, the
rule list, the ledger) is `wizard.css` and `wizard.js`, verbatim — and
**the door**, which now rains with the real thing: `fullfall.css` is
`Fullfall.svelte`'s stylesheet as it ships (forty strokes, three inks,
transform-only, three duration tiers, the still-rain rest under reduced
motion). Round 10's still field of marks is gone.

What differs is the journey — Enter to the wizard, and Finish to the
fall. Each direction plays the *same* journey both times, so the way
between places is one idea per direction, not two.

Served from the design host, port 8309:

| | Concept | Open |
|---|---|---|
| **AH** | **The slide** — you slide down the fall to the wizard | `http://192.168.11.30:8309/screens/wizard/round-11/ah-slide.html` |
| **AI** | **The swell** — the rain rises to fill the screen, then falls away | `http://192.168.11.30:8309/screens/wizard/round-11/ai-swell.html` |
| **AJ** | **The storm** — wind, the rain drunk into the box, one ring | `http://192.168.11.30:8309/screens/wizard/round-11/aj-storm.html` |

Start at the door: Enter (or ⏎). Fill the one router step, Next, your
password, Copy, watch, tag, Finish. The prototype strip is bottom right;
`?skipdoor` opens straight on the wizard. Restart reloads the page.

## AH · The slide (≈2.9s in, ≈1.7s out)

The door is the top of the fall. Enter lets go, and you slide down it:
past the NOW line (amber, with the clock), down through the rows of a
fall nothing has filled yet — thirty seconds a row, the labels dimming
with depth, one honest line at the deepest point: *nothing has arrived —
no router is sending yet; that is the next screen* — and the wizard
comes up from below to meet you, its strip the seam you land on. A
short settle, and it stands.

The rain is the weather you fall through: three planes of the real
strokes at three depths (far, small and dim; mid; near, large), each
looping seamlessly, each streaking longer with your speed — the near
plane most. The amber box lifts off the door and rides with you to the
top-left, where it settles as the bar's wordmark just as the page lands.

Finish is the same slide, one screen: the wizard's body slides up and
away, the fall slides up into its place under the bar and strip, the
weather streaks past for the length of it, and the fall is live.

## AI · The swell (≈3.2s in, ≈3.2s out)

Enter, and the rain comes on. One curve, up over 1.5s, held, down over
1.3s, and everything the rain is follows it: how many strokes (seventy
to 2,600), how fast, how long, how wide, how bright — and in which inks.
It starts as the fall's three; past a quarter of the way up, every ink
MikroView owns joins in (the four lanes, accept, log, NOW, drop, alarm),
so the swell is not louder rain but *more coloured* rain. The strokes
draw additively, so where they crowd they bloom toward white, and at the
top of the curve the whole frame is light — a warm wash, the screen
filled. Under that cover the place changes. Then it eases: the ground
lifts ahead of the rain so the wizard shows through as the field thins,
the last strokes fall out of the bottom, and it is standing there.

Finish is the same swell; what it leaves behind is the fall.

## AJ · The storm (≈3.0s in, ≈2.4s out)

Four beats on one clock. **Wind** (0–1.1s): the rain tilts and gusts,
there is more of it and it is longer, the frame darkens at its edges,
the amber box gutters like a lamp. **Drain** (0.8–1.9s): every stroke on
the screen bends toward the box and is drunk — the sky empties into it,
and the box charges from amber to white as it drinks. **Break** (1.9s):
a flash; a ring goes out from the box, amber edged white, and everything
inside the ring is the wizard, already lit — the page is revealed by the
ring's radius and nothing else; the rain the box drank comes back as
three hundred embers riding just behind the ring's edge, in every ink,
and dies out over the new place; the box jumps to the bar and cools; the
frame shakes once. **Settle** (2.9s): the ring is past the corners, the
embers are out, the page stands.

Finish charges the box from the strip — the boundaries' ticks stream
into the wordmark — and breaks the same way; the ring's wake is the fall.

## Gates

- `drive.mjs` runs each direction through the door, the way in at true
  times (Playwright's clock, so a frame at 1300ms is at 1300ms), the
  wizard to Finish, the way out at true times, the fall, and asserts a
  locked step is not clickable; no console errors. `shots/` is the
  record, `shots/times.txt` the frame times.
- No new colour: every ink is an `app.css` token, read off `:root`.
  The swell's white is the strokes crowding, not a colour of its own;
  the wash and the flash are the same warm white.
- The wizard and the door markup are generated from one skeleton
  (`pages.py`), so the three pages differ only in their journey.

## Honest gaps

As round 10 for the wizard and the fall. The journeys' rain in AI and AJ
is a canvas, which the app's CSP allows (no inline style attributes,
nothing fetched), but a build would decide whether to grow `Fullfall`
into it or keep the canvas for the journey alone. AH's chute is the
fall's grammar redrawn, not `Fall.svelte`. Reduced motion: every journey
is a short crossfade; the door's rain hangs still as the real one does.

## Verdicts

None yet.

Written by Fable 5.1, 2026-09-27.
