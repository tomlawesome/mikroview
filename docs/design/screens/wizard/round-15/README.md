# Setup wizard — round 15 (#1374): the neon, sparks done properly

Owner, on round 14 (verbatim in `../round-14/README.md`): "The sparks
look a bit like cheap confetti, but I really like the rest."

AN is round 14's AM with only the sparks redrawn. Everything else — the
slide, the swell, the sign that strikes and fails like a broken tube,
the hum, the current, the line, the catch, the wipe, the landing, the
groups that strike on — is AM, unchanged.

| | Concept | Open |
|---|---|---|
| **AN** | **The neon, sparks done properly** | `http://192.168.11.30:8309/screens/wizard/round-15/an-neon.html` |
| | the same with no sparks at all, to compare | `http://192.168.11.30:8309/screens/wizard/round-15/an-neon.html?nosparks` |

## What changed

What made round 14's sparks confetti: every drop threw them, each in
the drop's own colour, and each was a coloured dot with a short tail.
Now:

- **One colour, the tube's.** A spark is white-hot when it leaves the
  line and cools to the line's ink by a third of its life, then dies.
  Never a drop's colour.
- **A streak, not a dot.** Each spark is drawn as a line the length of
  its speed — fast means long, slowing means short — thinning as it
  cools. There is no dot at the head.
- **Fewer, faster, shorter-lived.** Roughly one catch in three throws a
  spark (one or two, not one to three), thrown harder (up to 420px/s
  up, 160 sideways), under heavier gravity, gone in 200–460ms. At most
  three hundred alive.
- **The strike throws a burst.** When the current reaches both edges
  and the line strikes, nine sparks fly from each end — the one moment
  the tube throws a handful.

## The wizard, one change

Owner, while round 15 was up (verbatim): "Remove the two lines with a
green Yes and change the button to just a Yes/No. You don't need to
time/interval as it's already in the description". Done in `wizard.js`
(the first change to the wizard since round 10): the push and backup
choices read **Yes** / **No**, and the two lines under them are gone.

## Gates

As round 14.

## Honest gaps

As round 14.

## Verdicts

Owner, 2026-09-27 (verbatim): "6. Round 15 no sparks approved."

**Ratified**: AN with no sparks (`an-neon.html?nosparks` is the
reference; in a build the sparks do not exist). The wizard is round
10's AG with this round's Yes / No change. The consolidated record is
`../DESIGN.md`; the build issues are linked from #1374.

Written by Fable 5.1, 2026-09-27.
