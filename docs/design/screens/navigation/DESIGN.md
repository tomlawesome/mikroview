# Navigation — the ratified design (#486, under #518)

Ratified by the owner across five rounds, 2026-08-23 (`round-1/` …
`round-5/` beside this file carry the mockups, screenshots and the
verbatim verdict trail; the same trail is on #518). This document is
the consolidated record the build implements from. The mockups are
reference for execution quality; where this text and a mockup detail
disagree, this text wins.

## The model

One **left rail** — the ADR's nav-rail primitive — holds the whole
geography. No masthead. Five groups in fixed order, group headings as
labels (never controls, no landing pages, no accordion):

| Group | Pages (in order) |
|---|---|
| Live | The fall · [Map, v0.5.0] · Stream |
| Investigate | Metrics · Audit log · [Lookback, when built] |
| Detect | Flags · Detectors |
| Expect | Watchlist |
| Admin | Users · Tokens · Fleet · Entities · ⟳ Run setup… |

- The fall is the landing (#483/#363, already ratified).
- Exclusions is a tab of Flags; Suggestions and Matches are tabs of
  Watchlist; Users/Tokens/Fleet/Entities are pages (the overlays
  retire); "Run setup…" is an action (opens #487's modal), not a page.
- **Reserved-slot rule**: the rail never renders a link to a surface
  that does not exist yet (Map before v0.5.0, Lookback until built).
  Slots are reserved in this spec, not in the DOM. No stubs.

## Three persistent states

**Full (216px, icons+text) · icons (54px) · docked (0px + handle).**
One per-user preference, applied before first paint, never changed by
the app on its own. Defaults: full at ≥1280px, icons below; docked is
never a default. States change **in the rail footer only**: ⇔ toggles
density (aria-label names the destination), ⇤ docks (its label
teaches the way back). The handle **restores** the persistent
undocked state — same density, same scroll, focus on the current
page; it never writes the preference. (Round-2's drawer/pin was
considered and dropped by the owner — Superseded below.)

## The handle

A 30×84px glass tab on the left edge, **vertically centred on the
viewport, always** — independent of scroll and page. Mark: a hub on
the edge with three links fanning inward, hub carrying the receiving
dot's pulse (off under reduced motion). Wears the open-flag count
badge. First in tab order after the skip-link; Enter restores.
Connection state is never the handle's job. *Owner note: the mark is
accepted "for now" and will be revisited (“we'll change it later”).*

## Badges and broken state

- **One count on the rail**: open unexcluded flags, alarm-red, on
  Flags (and on the handle when docked). Quiet counts inside pages
  (e.g. Exclusions' tab) are outlined, never alarm-filled.
- **Broken is a ring, not a number**: anything in a currently-broken
  state (e.g. Watchlist while a watch is broken) wears a 2px
  alarm-red outline, 3px offset — around **icon + word** in
  icons+text, around the **icon alone** in icons only (one element;
  hiding the label tightens the ring). Clears when the break clears;
  the aria-label carries the reason.

## States of the chrome

- **First run**: the shell and rail render behind the auto-launched
  wizard modal (#487); no badge yet; closing early leaves empty
  states that each point to Admin ▸ Run setup….
- **Connection lost**: rail-head dot turns alarm; the banner tops the
  content column and pushes content (never overlays); nav stays
  operable; the fall holds its last brink time rather than faking
  liveness. Docked: the banner alone carries it.
- **Loading**: shell plus ghost rows — never a spinner page. Errors
  name the failing thing in words and keep the chrome alive.
- **Viewer (#490 grammar)**: the Admin group renders for viewers, and so
  do the pages in it a viewer may actually read — today Fleet alone.
  Read-only is declared once, in words, in the page header chip
  ("READ-ONLY — ADMINS EDIT"); edit affordances and admin-only rows
  (Run setup…) are **absent, never disabled**. Users, Tokens and
  Entities stay admin-only and the grammar does not reach them: their
  data is gated server-side for reasons recorded in
  `internal/api/authz_matrix_test.go` — `GET /api/auth/users` is "the map
  of whose account is worth attacking", and `GET /api/tokens` "lists
  issued bearer credentials".

## Keyboard and accessibility

Skip-link first. Rail items are plain links, `aria-current="page"`;
group headings are list labels. Icons density keeps full labels:
tooltip on hover **and** focus, label+count in aria-labels, visible
focus ring. Tabs inside pages are the house tablist (arrow keys).
Every control is a real button with a spoken label; state changes are
announced ("navigation docked", "navigation restored — icons only").
Docking returns focus to the handle; restoring lands it on the
current page. Nothing in the chrome is hover-only. Reduced motion:
all pulses and slides become instant.

## Small screens

Bottom bar of the five groups (badge intact); tapping a group with
more than one page raises a half-sheet (house modal: focus trap,
Esc/back closes); single-page groups go straight to the page. Dock
and density are pointer-width affordances — they do not exist on the
bottom bar.

**The broken ring, on a bar of groups** (#583, ratified 2026-08-24 —
this section was silent on it until then, because the ring's trigger
was settled after the small-screen rounds were written). The ring is
a safety signal, so it is never conditional on screen width:

- **It rings the group, and the sheet says which page.** The group's
  claim is the rail's claim one level up — *an answer behind this
  group cannot be trusted*. The precision is deferred, not lost, and
  deferral is honest only because the next tap resolves it. Hence the
  general rule, binding on every future broken state, not just this
  one: **a group may wear the ring only where opening that group
  shows which page carries it.** A group ring the sheet does not
  resolve is a dead end, and is not shown.
- **Geometry tightens to the space.** Around the **icon alone** on
  the bar — five items across a phone-width bar leave a 2px/3px
  outline nowhere to go around a stacked icon-and-word — which is the
  same form the rail uses in icons-only, for the same reason. Inside
  the half-sheet the rows are full-width list items, so the ring
  follows the icons+text form, around **icon + word**.
- **One sentence, narrowing subject.** Group: *Expect — 1 watch can't
  be checked…*; sheet row: *Watchlist — 1 watch can't be checked…*;
  page: the entry itself. The operator reads the same sentence three
  times, each time about a smaller thing. Plural agreement follows
  the count.
- **Two alarm-red marks on one bar is allowed.** Flags' filled count
  and Expect's outline ring are different marks on different groups,
  and the rail already permits both at once. Not a clash to be fixed
  later.
- `unknown` and `out-of-scope` coverage never ring, on any surface —
  an honesty rule, not a rail detail. The trigger itself is #546's.

## Themes, for the build's purposes

Light and dark are token swaps on the ADR floor; the light surface
requires its own validated lane steps (lan `#2f77d3` · srv `#12855d`
· guest `#c2508a` · iot `#a06a00` on `#dfe5ef`+; record in
`round-3/README.md`). Theme *identity* stays #492's. The navy/grey
canvases in rounds 4–5 are mockup presentation, not app tokens.

## Superseded (considered and closed)

- **P — Masthead** and **R — Places** (round 1): killed by verdict;
  R's Detectors-into-Flags fold died with it.
- **Auto-collapse on Live places** (rounds 1–2): superseded by the
  explicit three-state preference.
- **The drawer + pin from docked** (round 2): the owner chose
  restore-from-footer-state instead.
- **A dot for broken watch state** (round 3 question): replaced by
  the broken ring, owner's design.
- **Every Admin page readable by viewers** (how the viewer bullet read
  until 2026-08-24): closed by the owner on #548. Building it meant
  loosening three admin-gated endpoints so a viewer could enumerate
  accounts and issued API credentials — a real and permanent security
  cost, bought for a read-only label on pages a viewer has no reason to
  open. The rounds wrote the bullet about hiding edit affordances and
  did not weigh that.
- Icon glyphs in the mockups are placeholders; the icon set is an
  implementation asset, not part of this record.
