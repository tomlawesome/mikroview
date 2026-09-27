# Setup wizard — round 12 (#1374): the catch

Owner, on round 11 (verbatim in `../round-11/README.md`): combine the
slide and the swell; no white flash; something after the swell that is
not a fade; the wizard's elements arrive in groups; the logo does not
disappear — it moves to the centre, grows by half, its yellow box
wiggles organically and changes colour, the letters flicker at random
to match the rain; a line grows from each side of the box across the
screen, starts to catch the rain, then wipes up the screen and away.
While this was drawn: "Don't overdo the letter flicker. Just enough, is
enough" and "when it starts to catch, at first only a few drops, most
slip by, more and more get caught until nothing passes the line".

The wizard is round 10's AG, unchanged (`wizard.css`, `wizard.js`); the
door's rain is the real `Fullfall`'s (`fullfall.css`). One direction:

| | Concept | Open |
|---|---|---|
| **AK** | **The catch** — slide, swell, the box, the line that catches the rain and wipes it away, the wizard in groups | `http://192.168.11.30:8309/screens/wizard/round-12/ak-catch.html` |

Start at the door: Enter (or ⏎). Fill the one router step, Next, your
password, Copy, watch, tag, Finish. The prototype strip is bottom right;
`?skipdoor` opens straight on the wizard. Restart reloads the page.

## AK · The catch (≈6.5s in, ≈5s out)

One journey, six beats on one clock. The way in:

- **Let go** (0–0.8s). The welcome and the fields drop away. The box
  does not: it leaves the stack, moves to the centre of the screen and
  grows by half. The door is the top of the fall, and you slide down
  it — past the NOW line, down through the empty rows nothing has
  filled yet (round 11's chute), the rain streaking with your speed.
- **The swell** (1.1–2.6s). While you are still sliding the rain comes
  on: more of it, faster, longer, and in every ink MikroView owns — the
  fall's three first, then the lanes, accept, log, NOW, drop, alarm.
  The box's border wobbles like something alive (three slow sines per
  edge, the amplitude growing with the swell) and glides from amber
  through the rain's colours, one ink to the next, never snapping. A
  letter flickers to a rain colour now and then — one at a time, a few
  times a second, for a tenth of a second — and no more. No white
  wash: at the top of the swell the frame is full of colour, not light.
  Under that cover the door goes and the page stands bare.
- **The line** (2.5–3.1s). From each side of the box a line grows,
  the box's own colour, until it spans the screen.
- **The catch** (3.1–3.7s). Rain meeting the line is caught or slips
  through: at first almost all of it slips by, and the sky below the
  line clears as it falls out; then more and more is caught, until
  nothing passes. Each drop caught is a bead on the line in its own
  ink, bright as it lands, then part of the wet; the line thickens and
  glows with what it holds.
- **The wipe** (3.7–4.6s). The line rises, taking everything it has
  caught and everything it meets, and below it the place is clean. The
  box rides up with it, then leaves it near the top, shrinking and
  sliding left to become the bar's wordmark; its border and the
  flicker are gone by the time it lands. The line goes off the top.
- **The groups** (4.35–5.8s). The bar's chips; the strip; the step
  rows, one at a time; the heading, then the form, row by row; the
  footer. Then the router's name field has focus.

Finish plays the same journey at three-quarters speed: the box leaves
the bar for the centre and grows; the wizard's body slides up; the
swell, the line, the catch, the wipe; what stands bare under it is the
fall's frame, and its groups are the columns one by one, then the
axis and the body, then the side rail — and the fall is live.

## Gates

- `drive.mjs` runs the door, the way in at true times (Playwright's
  clock, so a frame at 3300ms is at 3300ms), the wizard to Finish, the
  way out at true times, the fall, and asserts a locked step is not
  clickable; no console errors. `shots/` is the record, `shots/times.txt`
  the frame times.
- No new colour: every ink is an `app.css` token read off `:root`. The
  line and the box's border wear the rain's inks; the beads wear the
  ink of the drop that made them.
- The wizard and the door markup come from round 11's skeleton
  (`pages.py`).

## Honest gaps

As round 11 for the wizard and the fall. The journey's rain, the box's
border, the line and the beads are a canvas (the app's CSP allows it: no
inline style attributes, nothing fetched); a build would decide whether
`Fullfall` grows into it or a canvas serves the journey alone. The chute
is the fall's grammar redrawn, not `Fall.svelte`. Reduced motion: a
short crossfade; the door's rain hangs still as the real one does.

A trap found this round, worth carrying into a build: a fixed canvas
with only `inset: 0` keeps its bitmap size as its CSS size, so at any
device pixel ratio other than 1 everything drawn lands off by that
ratio. Round 11's AI and AJ have this; it did not show there because
nothing they drew had to line up with the DOM. `ak.css` gives the canvas
`width: 100%; height: 100%`.

## Verdicts

Owner, 2026-09-27 (verbatim):

> YES finally. I love it. Some small tweaks.. the yellow line at the
> start - from the fall, don't show that at all and remove the numbers
> from the fall. Basically... do the slide bit but without the fall
> bits. Only other tweak is to increase the letter flicker a bit

Read as: AK is the journey. Round 13 (`../round-13/`) is AK with the
two tweaks: the slide keeps its motion but the chute — the NOW line,
the time labels, the rows, the "nothing has arrived" line — is gone;
the letters flicker a little more often.

Written by Fable 5.1, 2026-09-27.
