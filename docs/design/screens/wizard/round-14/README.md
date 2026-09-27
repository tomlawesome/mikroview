# Setup wizard — round 14 (#1374): the neon

Owner, on round 13 (verbatim in `../round-13/README.md`): "So, with the
letter flickering, I meant a bit more like neon/tube lights flicker
when they're broken. Have one more round and try to just go a bit
further. Add something more to it."

AM is round 13's AL with one idea taken through the whole journey: the
box is a neon sign in the rain, and everything that happens to it is
what happens to a tube. Nothing about the slide, the swell, the line,
the catch, the wipe, the landing or the groups' order has changed.

| | Concept | Open |
|---|---|---|
| **AM** | **The neon** | `http://192.168.11.30:8309/screens/wizard/round-14/am-neon.html` |

Start at the door: Enter (or ⏎). Fill the one router step, Next, your
password, Copy, watch, tag, Finish. `?skipdoor` opens on the wizard.

## What is new

- **The sign strikes.** As the swell comes on, the letters light one
  after another in amber, each with a tube's stutter (on, out, on, out,
  on), left to right over about half a second.
- **The letters fail like a broken tube.** From then on, every 110–300ms
  one letter (sometimes its neighbour with it) does one of four things
  in whatever ink the rain is wearing: a *buzz* (five to ten flicks
  between lit and dark, 18–42ms each), a *stutter* (lit, out, lit, out,
  lit, then back), a *drop-out* (dark for 140–320ms, a flash back, a
  dip, then lit), or a plain *hold*. Lit means the ink with a glow; dark
  means the tube gone out — the letter almost invisible. Colour changes
  are instant, as a tube's are; there is no fade.
- **The tube hums.** Now and then, for a few frames, the box's border
  and the line dip to half — the same dip on both, since they are one
  tube.
- **Current runs out along the line.** While the line is growing, each
  end carries a bright head, white at the core and the line's ink
  around it; when both heads reach the edges the whole line strikes
  (on, out, on, dip, on) and settles.
- **Sparks.** Every drop the line catches throws one to three sparks
  off the tube, in the drop's ink: up and out, then falling under
  gravity, white-hot for the first part of their life, dying in under
  two-thirds of a second. The catch goes from porous to total, so the
  line fizzes more and more; during the wipe the rising line sheds
  sparks below it like a seam being welded.
- **The groups strike on.** Each step row, each block of the body, the
  footer — and on Finish, each of the fall's columns — comes on like a
  tube: dark, a flash, a dip, then steady, one after another. It is a
  420ms blink, not a light show; if it reads as too much, it is one CSS
  rule (`.strike`) and comes out cleanly.

## Gates

As round 12: `drive.mjs` at true times, `shots/` the record, no console
errors, the locked-step assertion, every ink an `app.css` token (the
sparks' white-hot core and the current's core are the same warm white
the swell used in round 11).

## Honest gaps

As round 12. The letters' glow is `text-shadow`, which the app's CSP
allows as inline style set from script; a build would put the lit and
dark states in classes.

## Verdicts

None yet.

Written by Fable 5.1, 2026-09-27.
