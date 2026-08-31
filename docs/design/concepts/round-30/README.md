# Interface visioning — round 30 (the source of truth)

Under #634, closing #697. Rounds 1–29 are a stack of corrective and
additive layers: each one carries a verdict batch, and none of them is
the whole picture. **This round is the whole picture** — every ratified
surface in one file, `the-whole.html`, with the two things round 29 was
missing and the chrome the owner struck on 2026-08-31 struck here too.

Round 29's surfaces carry forward **verbatim** unless listed below.
Nothing accepted has been redrawn.

The stale, uncommitted `round-30/` written in a previous session
(a truncated copy of round 13, ending mid-stylesheet) is superseded by
this one and was never reviewed.

## What is new in round 30

### 1. The stream's filter finally has a home (#697)

Two halves, and only one was a defect:

- Round 29's `#s5` drew a box that **displayed** filters and offered no
  way to make one. The build therefore kept `FilterBar.svelte`'s
  separate "Filters ▸" trigger mounted beside it, because retiring it
  would have removed the only way in. That mismatch is what the owner
  kept reporting as "nothing like round 29".
- The ratified answer already existed and had never been folded in:
  **round 8's thin bar** — "no, you ignored my instruction, sliding out
  to the left, as a thin bar that's reminiscent of the old live view",
  accepted as *"Round 8, yeah much better!"*

So, in round 30:

- **The box is always on screen.** Clear every term and it says so
  (`no filter — every line, as it arrived. type a term, or click a
  value in a row`) instead of vanishing. There is always a way in, so
  no second filter control needs to exist.
- **`◂ bar` is welded to the box's left edge.** Click it and round 8's
  strip unfurls out of that edge — device · action · chain · proto ·
  source ⇄ destination · port · interface · rule, dim micro-labels over
  hairline-underlined values, `× clear` and `fold ▸` at its end. The
  handle reads `bar ▸` while open; folding pushes it back into the box.
- **One filter, two hands.** The box keeps the full typed grammar and
  stays the workhorse; the bar is the same filter as named fields.
  Editing either writes the other. Clicking a value in a row — the
  ratified `EventRow` gesture — writes both.
- Round 7's glass panel is **not** restored. It was rejected, and
  round 23 recorded that "a filter-box move was asked and reversed —
  the filter row stays". `FilterBar.svelte` **is** round 8's bar and is
  not to be retired; what changes is that its trigger lives on the
  box's edge instead of floating separately.

> **#697's body asks for round 7's panel.** That is stale: it was
> written before round 8's verdict was found in the trail. Round 8
> supersedes it, and round 30 builds round 8.

### 2. The stream's top, ordered

The owner's last report: *"the overall layout at the top of the stream
is just wrong."* It was, and measurably. Round 29 stacked four
absolutely-positioned strips at hand-picked offsets: the whisper sat at
`top: 88px` and stood ~46px tall (a 30px line whose two time labels hang
32px beneath it), while `table.stream` began at `margin-top: 100px`.
They overlapped by most of the whisper's height. With the page heading
struck as well, the left of the second row was empty while every control
crowded the right.

Coordinates cannot hold an order, so the top is a flow column now:

    the chrome     wordmark left · status cluster right
    the filter     [◂ bar]  the box  …  the spans
    the bar        round 8's strip, when it is out
    the whisper    the last fifteen minutes, full width
    the lines      the table

Each band follows the one above it. Nothing can collide, and the bar
pushes the whisper and the table down rather than covering them.

### 3. The record changes of 2026-08-31, drawn

These were made against the running demo and live only in commit
messages on `fix/mockup-labels-removed` (76282f8, a04b880, 61ed6fa).
Drawing them here is the point of the round — a later build to round 29
would silently undo every one.

- **No page heading and no strap, anywhere.** Struck on metrics, the
  docket, the fall and the topography in turn, so it is gone outright.
  The roll rail already says which card you are on. `.scname` and
  `.scname .epi` are **deleted from the stylesheet**, not just unused,
  so nobody restores them from it.
- **The switchers ride the bar** where the heading used to be, beside
  the wordmark: metrics' seismograph · register · table, and the
  docket's flags · watchlist · audit log.
- **No counts on the docket's tabs.** Tried inline (round 17) and
  beneath (round 19); both were called clumsy. The counts live in the
  chrome's ⚑ and eye marks, on every scene. `.dtabs .under` and its
  inks are deleted for the same reason as `.scname`.
- **The fall's span control sits in the status cluster**, ahead of LIVE,
  now using the shared `.spans` class rather than inline styles.
- **The stream has its country flags and its ⚑ mark back**, and the mark
  rides **after** the time — ahead of it, it pushed the first digit
  right and broke the left edge the tabular figures line up on. The row
  wash stays; the mark annotates it. A row with no country code gets no
  glyph and no guess, and the two-letter code always stays beside the
  flag, so the meaning never rests on the glyph alone.
- **Settings is two columns**: "your deck" on the left, everything else
  stacked down the right at full width (owner, 2026-08-31: *"make it two
  columns, one for the deck layout and stack the others vertically, and
  make them a lot wider"*). A group with a drawing splits inside itself —
  the drawing and its caption left, its rows right — so the width buys a
  shorter page rather than wide whitespace, and the four groups fit the
  card without scrolling.
- **The wordmark is the way home** — to whichever card the operator
  keeps first, so reordering the deck moves it too.

### 4. Two ratified rules round 29 was still breaking

Found while going through the trail, not asked for this session:

- **The door said "passphrase".** Round 15's verdict — *"the login
  should say password, not passphrase because I'm not american"* — was
  ratified as a wording rule for every surface and named the door as the
  one place breaking it. Rounds 16–29 never fixed the mockup. Fixed.
- **The register's flag labels crossed the axis.** At `rotate(-60)` each
  name began 2px above the brink rule and swept down across it, and a
  longer name dipped further — the same fault the build diagnosed and
  fixed in `a04b880`. Rotation was carrying nothing: the two columns are
  52px apart and a horizontal name fits. They are written flat above the
  line now, each centred on the column it names.

## What carries forward, and where it was settled

| Surface | Settled in | Verdict |
| --- | --- | --- |
| Atlas as the direction; **the fall** as hero and landing | 3, 4 | "You might have smashed it here with 'the fall'" · "we have our direction" |
| The topography = round 2's Atlas II, never round 4's map | 4 | round 4's map/reach rejected: "stick to the previous Atlas II refined version" |
| Columns squared on the stream | 4 | owner's alignment ask, done structurally |
| **The deck** — full-viewport cards, scroll-snapped, the operator's own order | 5 | promoted from the owner's own read of the format |
| The door, v3 (full-screen fall, boxed wordmark) | 5 | "Congratulations on the new login, great work" |
| Free-floating chrome, no rigid top bar | 5 | "can we have the information presented in more of a free floating way?" |
| **One altitude axis** (concept T); lenses as quiet words | 6 | "Ok, I like T too. Lets go with that." |
| Three main views: the fall · the topography map · the stream | 6 | governing principle, ratified |
| **The thin filter bar** | 8 | "Round 8, yeah much better!" |
| Deck order: fall → topography → metrics → stream; **the roll rail** | 11, 13 | "FWIW the side bar nav is great" |
| Metrics = the page we already built (#488), the round-13 drum | 13 | "lets move on with the seismograph as it is in 13" |
| Watchers **purple** | 14 | "Go with purple" |
| The account menu; **password, never passphrase** | 15, 16 | "Approved otherwise"; the atlas dropped entirely |
| **The docket** — flags · watchlist · audit log on one card | 17–19 | "Loving it!" |
| Drawers, one unbroken type stripe, sort/filter on every column | 18, 19 | built to the corrections |
| The health **dials** on the topography | 19, 20 | "Yeah I love the dials" |
| The reach **dives in place**; waves are lines only | 21, 22 | corrections built |
| The **whisper** commanding the stream; node cards everywhere | 22, 23 | "amazing!" · "really good" |
| **One aggregate bar**, purple/red split, everything clicks through | 23 | "LOVE this, this is what we needed." |
| Entities (fleet is an internal name only) | 23, 24 | "now great" |
| Settings in the app's own identity; the reach backdrop | 24, 25 | "Settings approved" · "This is now excellent." |
| Reach dead zones | 25 | "Dead zone is perfect." |
| **The journey** — attach · it flows · the tour offers · the tour · the wizard | 26, 27, 29 | "55,56,57 yes" · "58, yes but…" · "59, yes" · "62. approve" |
| The **outlined bubble** clear-all | 28, 29 | "61. perfect" |

Dropped and **not to be re-proposed**: Editorial (B), Instrument (A),
Luminous (C), Casefile (E), Halo, Riverline, Gauntlet, Dispatch,
Lattice, Core (M), Score (N), round 4's map and reach scenes, round 7's
glass filter panel, the atlas overlay, the two-bar aggregate, the
outside-the-shoulder aggregate, both earlier clear-alls, docket tab
counts in every form.

## Validation

- **No new colours.** Every ink is a ratified token: lanes lan
  `#3987e5` · srv `#199e70` · guest `#d76a9e` · iot `#c98500`, validated
  on the Atlas void `#06080e` in round 5 (six dataviz checks; lanes are
  always direct-labelled and never colour-alone); the flag family inks
  from round 18; watchers `#a78bfa` from round 14; the fall's heat pair
  from round 3. Nothing to re-validate.
- **Every scene screenshot and looked at** — 15 shots in `shots/`,
  regenerated with
  `cd frontend && node ../docs/design/concepts/round-30/capture.mjs`.
  The register's crossing labels and the door's wording were both caught
  in this pass, not in the markup.
- `prefers-reduced-motion` disables all animation, including the bar's
  unfurl.
- **No apparatus at all** — see below.

## 5. No apparatus, anywhere

Owner, 2026-08-31, verbatim: **"Ok, well remove ALL deescriptions
intended just for mock ups please. A final mock up shou;d be the visual
source of truth free of any mock up descriptions of aids to understand."**

Every round from 1 to 29 carried explanatory furniture. All of it is
gone from round 30, markup and stylesheet both, so nothing invites it
back:

- the round ribbon at the top of the file
- all seven amber scene notes
- the door's dashed sign-out storyboard strip and its "mockup note" label
- the topography's dimmed "at rest — all green, nothing to report" pair
- the docket's "round apparatus — bring the six back" restore link
- **the fall's how-to-read key**, which round 5 had already ruled against:
  *"deep explanation never sits in the UI … 'How to read this' blocks hide
  behind a tiny (i) well out of the way, with the depth in the docs."*
  The fall has that (i); it never needed the key beneath it.
- the fall's now-line, which explained itself — it reads `NOW · 14:02:11`
- two journey captions that described the design rather than spoke in it

**What was kept, and why it is not apparatus:** the honesty statements
(`nothing logged — no trace, and no claim of one`; `blank because nothing
is logged — not because nothing is sent`; `quiet is a fact, not a fault`)
are #445's never-guess ethos, ratified as interface in round 3 and stated
on every surface that can be silent; the stream's foot line reports the
day's three facts, not a key; and the journey is a five-beat storyboard of
a flow, so its beat labels are the sequence, not commentary on it.

## Owner verdicts

- The filter, as round 30 builds it (verbatim, 2026-08-31): **"ok, i love
  it as it is in round 30 now."** and, on whether round 8 supersedes
  round 7: **"Not sure what this pertains to, if it's the filter bar, I
  like it as it now is in round 30."** — round 8's bar stands; #697's
  request for round 7's panel is closed as stale.
- The two ratified rules round 29 was still breaking: **"Great catches."**
- Settings and the apparatus removal: asked for and built this round
  (verbatim above); no verdict yet.
- What the apparatus sweep kept — the #445 honesty statements, the
  stream's foot line, the journey's beat labels (verbatim, 2026-08-31):
  **"correct, these are part of the UI."**
- Everything else: pending — he is reading the round now.

## Known, and not fixed here

- **The topography's layout in the *built app*** — "still a mess": zone
  cards overflow the right edge, aggregate bars collide with the survey
  text, the "+27 pairs" chip overlaps a card. The mockup's `#s3` does
  not have those faults; it is a build problem against this drawing, not
  a design one, and it needs its own pass.
