# #1386 design review: the built journey against round 15

Fable, 2026-10-01. The real app at 09363519 (!1124's head, now on `dev`)
in Chromium at 1600×1000 and 1.5×, on a stepped clock, driven by
`drive.mjs` the way `../round-15/drive.mjs` drove the prototype: each
frame is shot at its true time on the journey's own clock, so
`pairs/<beat>.png` lays the ratified frame (`../round-15/shots/an-neon-*`)
beside the live one at the same time. Where a fix changed a frame, the
pair is a triple: ratified · as reviewed · after the fix.

Reproduce: `scripts/live-env.sh up` (eval its output), then from
`frontend/` `MV_PHASE=in node ../docs/design/screens/wizard/review-1386/drive.mjs`
on an instance restarted with the `devices:` line dropped from
`$MV_DIR/cfg.yaml` (the way in needs a brand-new install), `MV_PHASE=out`
on the default instance, `MV_REDUCED=1` for either; `python3 pairs.py`
for the sheets. The way in spends the account's one auto-launch, so each
way-in run is a fresh `up`.

## Verdict per "Done when" line

| Done when | Verdict |
|---|---|
| Enter at the door plays the way in and lands on the wizard with the name field focused; Finish plays the way out and lands on the live fall — in the real app, driven by real state | **Yes.** The way in starts 28 ms after Enter, ends at 7150 ms, and lands with the name field focused (`shots/live-notes.md`). The way out starts 16 ms after Finish, ends at 5350 ms, and lands on the fall; the offer rises at 6450 ms. |
| Frame-by-frame screenshots at true times against round-15's shots show the same beats at the same times, with no sparks | **Yes, after four fixes** (below). All six beats land on the prototype's clock in both directions; no sparks anywhere. Four things in the groups beat were built differently from the drawing and are fixed on this branch. |
| `prefers-reduced-motion` gives the crossfade; the CSP is unchanged; no console errors | **Crossfade: no, now fixed.** Both journeys were a cut, not a fade (`pairs/reduced-in-before.png`, `pairs/reduced-out-before.png`: the door gone and the wizard standing in the first frame; the fall standing in the first frame). Now the held door fades over the wizard and the wizard fades over the fall, 300 ms each, the prototype's `beat()` cap — `shots/reduced-notes.md` has the DOM timelines: the door held with `journey-fade` from 50 ms and down at 300 ms; the wizard page under `journey-fade` at 0 ms and gone at 250 ms. The fade itself is a CSS transition, which runs on real time under the stepped clock, so the frames show its ends, not its middle (the prototype's own caveat for the strike). CSP unchanged (the fix is classes on `<body>` and CSS). No console errors in any run. |
| After Finish the tour is offered once; the account menu opens it any time; #646's attach/connecting/glass code is removed | **Yes.** Offered once after a real Finish (`pairs/24-offer.png`), not after a second; "Take the tour" in the account menu starts it (`pairs/25-menu.png`, `pairs/26-tour.png`). Of #646's pre-wizard walk only `Fullfall`'s unused `attach` mask and three comments were left; removed here. |

## Beat by beat

The way in (`pairs/in-*.png`, 16 frames) and the way out
(`pairs/out-*.png`, 14 frames):

- **Let go** (in-0400, in-0900; out-0300, out-0700): the fields and the
  welcome drop, the box moves to the centre and grows by half, the rain
  streaks. Matches. The way out's box leaves the bar and the wizard's
  main slides up. Matches, except the bar kept its own wordmark while
  the ride carried a second one (fix 2).
- **The swell** (in-1400 – in-2400; out-1100 – out-1900): the rain
  thickens into every ink, no white wash; the sign strikes letter by
  letter (in-1900, out-1500) and letters fail in the rain's inks after.
  Matches.
- **The line** (in-2700, in-3000; out-2200): the current runs out to
  both edges, the line strikes. Matches.
- **The catch** (in-3300, in-3600; out-2500, out-2800): porous, then
  total; beads in their own ink; the line thickens with the wet. No
  sparks, where the prototype's frames (shot with sparks on) show them.
  Matches the ratified no-sparks reference.
- **The wipe** (in-3900 – in-4500; out-3100, out-3400): the line rises
  with everything it holds, the box rides up, shrinks and slides left,
  its border gone by the landing. Matches. On the way out the fall's
  axis, NOW line and port labels, and the deck's side rail, stood under
  the swell from the swap on, where the prototype's frame is bare until
  the body's beat (fix 3).
- **The groups** (in-4800 – in-6400; out-3700 – out-5300): bar, strip,
  rail rows one by one, the heading then the form rows, the footer. The
  footer arrived with the rail at 4.55 s and struck again at 5.35 s (fix
  1). On the way out the columns strike, then the axis and the body;
  the fall is live with the router's line flowing. Matches after fix 3.

Timings read off the DOM (`shots/live-notes.md`, both runs of the way
out): the box is placed 16–28 ms after Enter or Finish; the way in ends
at 7150 ms and the way out at 5350 ms, where `an.js` ends at 6500 ms
and the rain's tail, and 5300 ms (the way out's last group) — the same
clock. The sheets' third panels are from the capture made before the
fixes; the raw frames of that capture are not kept.

## Fixes on this branch

Each with a test that failed on 09363519:

1. **The footer's beat.** `Wizard.svelte` dropped `away` from the rail,
   the body and the footer together when the rows struck (4.55 s);
   `an.js` removes `#foot`'s `away` at 5.35 s. Now `wizardJourney.foot`
   is set at that beat and the footer waits for it
   (`Wizard.svelte.test.ts`, "the footer waits for its own beat").
2. **The bar's wordmark on the way out.** `an.js` sets the bar's
   wordmark to opacity 0 as the ride leaves and restores it when the
   ride lands; the build left it lit, so two wordmarks stood for 3.7 s.
   `.page.wiz.live` now drops while the ride is out
   (`Wizard.svelte.test.ts`, "the bar gives up its wordmark").
3. **What stands bare under the swell on the way out.** `journey.css`
   hid only the fall's bands, bar, attention line and foot; the rig's
   time axis, NOW line, port and peak labels and horizons, and the
   deck's roll rail, showed from the swap. They are the prototype's
   `.axis`, `.fallbody` and `.siderail`: hidden until `ak-fb` (4.8 s)
   and the last beat respectively (the NOW line's own pulse animated
   opacity over the rule, so it waits too). Checked in the live
   scenario (`frontend/scripts/live-journey.mjs`), which puts the
   journey's classes on `<body>` once the fall stands and reads the
   computed opacity of the axis, the NOW line and the rail off the real
   stylesheet — jsdom does not cascade stylesheets. Without the rules
   it read 1, 1, 1.
4. **Reduced motion.** `decide()` let the door down outright and
   `wayOut()` returned false so Finish cut to the fall. Now both put
   `journey-fade` on `<body>` for `CROSSFADE_MS` (300 ms) — the held
   door, or the wizard page, fades over what stands behind it — then
   hand over (`wizardJourney.test.ts`, the two "crossfades" tests).
5. **The chips and the strip on the way out.** `journey.css` fades the
   wizard's chips and strip to nothing whenever `journey` is on
   `<body>`; in the prototype they stayed through the way out because
   `ak-bar` and `ak-strip` were still on `<body>` from the way in
   (`an.js`'s `done()` never removed them; the build's does). The way
   out now adds them from the start
   (`WizardJourney.svelte.test.ts`).
6. **#646's remnant.** `Fullfall`'s `attach` mask variant and the
   comments that still described the attach beat (`AuthScreen`,
   `UpgradeWarnings`, `Fullfall`) are gone (`Fullfall.svelte.test.ts`).

## Findings that are not the build's

- **The wordmark's weight on this host.** Every live frame shows the
  wordmark lighter than the prototype's (`pairs/01-door.png`). Both set
  `font-weight: 800` on the same `--font-sans` stack; the review host's
  `system-ui` (WenQuanYi Zen Hei) has no bold face and `app.css` sets
  `font-synthesis: none`, so the app gets the regular face where the
  prototype, without that rule, gets a synthesised bold. On a host with
  a real bold face both are bold. Not a journey defect; worth knowing
  if a screenshot ever looks "thin".
- **The fall's bar and strip.** The design has one bar and one strip
  shared by the wizard and the fall, so the way out leaves the chips
  standing as the record and the strip becomes the top of the fall
  (DESIGN.md, "The bar and the strip"; `out-3700` – `out-5300`). The
  built fall's bar is the deck's own scene bar (range buttons, LIVE,
  the account) with no chips and no strip. That is the fall's shell,
  not the journey's motion, and nothing on the issue decides it. Owner
  question A below.
- **Where setup stands** (`pairs/22-done.png`): the live step reads as
  drawn; its drift was filed and closed as #1406.

## Not verified here

- The crossfade's middle frames (above): the DOM timeline and
  `wizardJourney.test.ts` stand for it.
- The way in on a brand-new install from AuthSetup's Continue: this
  capture signs in to an existing admin with a second factor and
  restarts the instance with no router declared, which takes the same
  `signedIn()` → `decide()` path. The Continue door is pinned by
  `AuthSetup.svelte.test.ts`.
- Firefox and WebKit: Chromium only, as round 15 was.

## Owner questions

- **A.** The fall's bar after the way out does not carry the wizard's
  chips or the strip (above). Options: (a) leave the fall's bar as it
  is and strike the sentence from DESIGN.md; (b) a new issue to give
  the fall's bar the chips and the strip as the record; (c) chips only.

Written by Fable 5.1, 2026-10-01.
