# Settings surfaces (#490) — round 5: the conditions editor in mikroview's own clothes (#829)

## Why

Round 4 drew the right grammar in the wrong clothes: *"lets bring it
stylistically in line with the rest of the new UI, and less
aesthetically like a generic program. This is mikroview - it should
LOOK like mikroview."* So round 5 keeps round 4's sentence grammar,
counting sentence, *reads as* line and Clone's honesty, and rebuilds
the drawing out of the app's own parts — `app.css` tokens and both
font stacks, `EngineRoomWatchers.svelte`'s bench, row, panel, chips,
inputs and actions row, `EngineRoom.svelte`'s settings card and group
heading — plus the owner's second instruction: a GitLab-style
click-to-select token bar as the way a condition line is written. Dark
only; round 30 (#708) removed the theme picker.

## The rules the round proposes

- **A condition line is a token in one bar**, labelled *matches when*.
  The bar is the stream's filter bar (`live-filter-dark.png`): mono
  placeholder, thin `--accent` ring when focused. A finished line is a
  chip reading `field · verb · value`, with the built `.chip-x` and a
  quiet mono *and* between chips — the engine has no "or".
- **Writing a line is three steps in a dropdown**: field (15 worded
  fields, typing narrows) → the verbs that field allows (`is · is not ·
  is one of · is not one of · is within · is between · is classed as`)
  → a value control that suits the verb (choice list for action, chain,
  protocol, interface, connection state, day of week, classification;
  typed box, chips for *is one of*, for addresses and ports). A whole
  line typed straight in parses to the same chip.
- **`is between` is offered for time of day as well as ports** — one
  box, one grammar, `1-1024` or `22:00-06:00`, hint follows the field.
- **Try and Save both wait** until every line is complete. The
  unfinished line says so on its own chip in `--now`, and a quiet
  *finish this line* sits beside both buttons; both take the built
  `:disabled` style.
- **Clone says beforehand what it carries**, in one sentence beside the
  button. Copies always start paused.
- Counting stays one sentence, *reads as* stays the honesty line, and
  the Try receipt is the built one (#786). `--alarm` appears nowhere on
  the bench; amber is `--now`, the engine room's time colour, borrowed
  for "not yet".

## Scenes

| Scene | Shows |
|---|---|
| s1 | The shipped row expanded to its actions row; Clone's sentence; the copy's empty bar |
| s2 | Port scan (copy), paused: one chip held, field list open and narrowed by typing `dst` |
| s3 | Garage probes: three lines held, one unfinished in amber; step 3 open on the range box; Try and Save waiting |
| s4 | The same row complete and tried, Save live; the viewer's prose line; the bar at 390 px |

Data story: the garage range `10.0.70.0/24` and its stragglers,
`10.0.70.14 garage-cam` and `10.0.70.31 ev-charger`.

## For the build

- Clone from a shipped *declarative* detector needs the server to hand
  back that detector's conditions (`BuildShippedDeclarativeDefinition`
  has them; the clone response or a GET must expose them). An API
  addition the build issue owns.
- Detail-template placeholders are the engine's closed set per key mode
  (`{Count}` plus the key's fields); the "can use" chips list exactly
  that set and insert on click.
- **The token bar's keyboard model.** The bar is one focus stop. `↑ ↓`
  move within the open step, `enter` takes the highlighted item and
  advances, `esc` closes the dropdown leaving the line as it is,
  `shift ⇥` steps back to the previous part, `backspace` on an empty
  input reopens the last chip. Typing narrows the current step's list;
  a typed full line (`dst port is one of 22, 3389`) parses on `enter`
  and becomes a chip. Clicking a chip reopens it at the part clicked.
  Chips are `.chip`/`.chip-x`, so removal already has a keyboard path.
- The dropdown is drawn in flow here so one screenshot can hold the
  bar, the menu and the sentence beneath it; in the build it overlays.

## Verdicts

Owner, 2026-09-09: *"it's really not very elegant I'm afraid, and it's
super generic. The engine room is the wrong place to look at for
inspiration on the style. Consider the rest of the app instead."* Then:
*"The layout is cluttered and confusing"* and *"It needs to be simple,
intuitive, clean."*

→ Round 6 starts from the docket's drawer and the app's popovers, and
makes the sentence itself the editor. The token bar, the group
headings and the boxed fields do not carry forward.

