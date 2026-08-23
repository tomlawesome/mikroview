# Navigation (#486) — design round 1

First round of the #518 design pass, on the surface every other
phase-3 design sits inside. Three directions, letters continuing from
the concept rounds (…M N O →) **P · Q · R**. Each proves the same five
scenes: **s1** the landing (the fall) · **s2** a working section
(Flags) · **s3** Admin as a viewer · **s4** small screens · **s5** the
chrome's own states (first run, connection lost, pre-map v0.4.0).

## What is fixed before any direction starts (the ratified bones)

- **The fall is the landing** (#483 round 4); drawn here verbatim from
  the round-3/4 record, dimmed, as context — its own design is not
  re-opened in this round.
- **Owner-decided merges** (#385): Exclusions is a tab of Flags;
  Suggestions and Matches are tabs of Watchlist; Users, Tokens, Fleet,
  Entities are Admin pages (the overlays retire); wizard relaunch
  lives in Admin.
- **Tokens** are the ADR floor (Atlas base; round-4 corrected lane set
  with guest rose). The nav unit is one house primitive whichever
  direction ratifies — P's masthead is the nav-rail primitive
  horizontal, Q's is it vertical.
- **#490 grammar**: Admin renders for viewers; read-only is declared
  once, in words, in the page header; edit affordances and admin-only
  actions are *absent* for viewers, never disabled — no control that
  cannot act.
- **The reserved-slot rule** (decided this round, all directions): nav
  never links to a surface that does not exist yet. Map's slot
  (second place in Live / the places, between The fall and Stream) is
  reserved in the spec and absent from the DOM until the topography
  ships (v0.5.0); Investigate holds to Metrics · Audit log until
  Lookback is built. No "coming soon" stubs.
- **One count, told once per level**: the alarm-red badge is open
  unexcluded flags, the same number at every level that shows it;
  quiet counts (Exclusions) are outlined, never alarm-filled.
- **States**: loading is the shell plus ghost rows, never a spinner
  page; the connection banner slots under the chrome and pushes
  content; first run renders the shell behind the auto-launched wizard
  modal (#487) with every empty state pointing back to setup; the fall
  holds its last brink time when the connection drops rather than
  faking liveness.
- **Keyboard**: skip-link first; nav rows are plain links with
  `aria-current="page"`; tabs are the house tablist (arrow keys);
  mobile nav surfaces are the house modal (focus trap, Esc/back).
  Nothing in the chrome is hover-only.
- Icon glyphs in Q are placeholders for anatomy, not an icon proposal.

## The directions — what actually varies

### P — Masthead (`direction-p-masthead.html`)
The #385 five-section starting point (Live · Investigate · Detect ·
Expect · Admin), executed in Atlas's chrome idiom: a glass masthead of
sections, a sub-line of the open section's pages, both translucent
over the surface. 88px of chrome; two levels always visible; the
five verbs stay the product's frame.

### Q — Rail (`direction-q-rail.html`)
The same five sections as a literal left rail: groups as headings,
all eleven pages permanently visible, zero navigation depth. On
Live's places the rail collapses to icons — deterministic per page,
pinnable, no hover tricks. Costs ~216px width on working pages.
Mobile: bottom bar of sections + half-sheet of pages.

### R — Places (`direction-r-places.html`)
Round 4's implicit line grown honest: The fall · Stream · Flags ·
Watch top-level ｜ Investigate · Admin as drawers. The verb sections
dissolve into the things themselves; Detectors folds into Flags as a
⚙-marked tab (the owned cost: views of the data and configuration of
it share a tab row). Flattest chrome — 50px on places.

## Validation record

- Lane set (lan `#3987e5` · srv `#199e70` · guest `#d76a9e` · iot
  `#c98500`) validated on this round's surface (`#06080e`): lightness
  band, chroma floor, normal-vision floor (worst 18.9), contrast all
  PASS; CVD separation WARN — worst adjacent pair srv↔guest ΔE 6.4
  deutan, inside the 6–8 floor band that is legal only with secondary
  encoding. Satisfied here: lanes appear exclusively as text-labelled
  chips and labelled channel heads, never colour-alone.
- Heat identities (accept `#5aa7f0` / drop `#e05252`) are sequential
  endpoints, not categorical slots (round-4 scope note stands). Chrome
  alarm `#ff5470` is chrome-only (badges, attention, banners); data
  red stays `#e05252` — red water never means anything else.
- `prefers-reduced-motion` disables all animation in all three files.
- Screenshots in `shots/`, regenerated with
  `cd frontend && node ../docs/design/screens/navigation/round-1/capture.mjs`.
  Every shot reviewed; annotation cards live below the frames so no
  callout ever covers the surface it describes.

## Open with the owner (round-1 batch)

Direction verdict (kill / keep / blend) plus:

1. Does Expect earn a quiet indicator when a watch is currently
   broken (a dot, never a count), or does Detect's badge carry the
   whole alarm story?
2. R folds Detectors into Flags; P/Q keep it a page. If R wins, does
   that fold survive, or does Detectors stay a page inside a places
   line (six top-level items becoming seven)?
3. Wordmark in the chrome: MIKROVIEW letterspaced (as drawn) — keep,
   or reserve the wordmark for the login screen and spend the space
   on the section line?

## Owner verdicts

_Pending — recorded verbatim here when the batch returns; the running
log lives on #518._
