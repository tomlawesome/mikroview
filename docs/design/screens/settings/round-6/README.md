# Settings surfaces (#490) — round 6: the sentence is the editor (#829)

## Why

Rounds 4 and 5 were both rejected, and for the same reason twice over:
*"not very elegant, super generic"*, *"cluttered and confusing"*,
*"needs to be simple, intuitive, clean"*, and — the instruction that
decided this round — *"the engine room is the wrong place to look for
style; consider the rest of the app"*. Round 5 had gone shopping in the
engine room and come back with group headings, labelled form fields, a
token bar and a dropdown. So round 6 keeps only round 5's sentence
grammar and its data story, and takes its drawing from the surfaces the
owner already likes: the docket's expanded flag, the popover family,
and the settings bench's own dashed-underline click-to-edit value.

## The rules this round proposes

- **The row opens into a drawer, like a docket flag.** Accent left rail,
  prose down the left, a small labelled block on the right where THE
  EPISODE sits, plain pill buttons across the foot, no boxes inside
  boxes.
- **One sentence, and it *is* the editor.** Body prose in `--fg-muted`;
  every changeable part in `--fg` under the bench's dashed accent
  underline, values in mono. Hovering a condition shows its `×`;
  `+ and…` adds one.
- **A second short line says what the flag will read**, `says` in the
  docket's 9px mono small-caps, the wording in mono, `{Count}` /
  `{SourceAddress}` in accent and editable like everything else.
- **One popover, off the word you clicked** — the app's own
  `.popover`. A condition is three quiet steps shown as a breadcrumb
  (`field › verb › value`): the choices are plain words in a list, never
  a select; typing narrows them; the only boxed input in the whole
  drawing is a typed value. Counting parts get a one-step popover; the
  name goes through the existing rename popover on the `✎`.
- **Unfinished is amber, not red.** The missing value shows as `…` in
  `--now` inside the clause itself, try and save both wait, and one
  amber line says which line to finish. `--alarm` appears nowhere.
- **Clone says nothing beside the button.** The copy is the answer: a
  new row directly under the original, paused, drawer open, with one dim
  mono line saying what came across.
- No group headings, no *reads as* line, no chips bar, no borders except
  that one input. Most of the drawer is quiet space.

## Scenes

| Scene | Shows |
|---|---|
| s1 | Port scan (copy) just cloned — the copied-from line, a complete sentence, "not tried yet" |
| s2 | Garage probes — the popover at `value`, `1-` typed, the sentence behind it carrying an amber `…`, try and save waiting |
| s3 | The same clause reopened at `field` with `dst` typed, the list down to two words |
| s4 | Complete and tried — the receipt, save live; below it the same row read-only, and at 390 px |

Data story unchanged from round 5: the garage range `10.0.70.0/24` and
its two stragglers, `10.0.70.14 garage-cam` and `10.0.70.31 ev-charger`.

No data palette is used on this surface — the only inks are the app's
own `--accent`, `--now` and `--accept` tokens — so the dataviz validator
does not apply here.

## For the build

- **The popover is anchored to the clicked span**, off its own
  `getBoundingClientRect()`, exactly as `NameEditorPopover` and
  `IpLookupPopover` already do. It is drawn beside the clause in these
  scenes, with a spacer holding the room it would cover, so one
  screenshot can show the popover, the sentence it came off and the
  actions still waiting below it.
- **Keyboard model.** The popover is one focus stop: typing narrows the
  current step's list, `↑ ↓` move, `enter` takes the highlighted word
  and advances a step, `esc` closes leaving the clause as it was,
  `shift ⇥` steps back. The clause's `×` is a real button, so removal
  already has a keyboard path.
- **Clone from a shipped declarative detector needs the API to hand back
  that detector's conditions** (`BuildShippedDeclarativeDefinition` has
  them; the clone response or a GET must expose them). Cloning a code
  detector carries scope and numbers only, and its copied-from line says
  so.
- **Placeholders are the engine's closed set per key mode** — `{Count}`
  plus the key's own fields. The `says` popover offers exactly that set
  and inserts on click.
- Verbs are offered per field: `is · is not · is one of · is not one of ·
  is within · is between · is classed as`. `is between` covers time of
  day as well as ports — one grammar, `1-1024` or `22:00-06:00`, with
  the hint following the field.

## Verdicts

Owner, 2026-09-09: *"The styling is exactly the same … No one asked for
the full sentence style either. I explicitly asked you for a GitLab
style builder/bar."* Clarified: the sameness is *"generic boxes/cards
on the flat blueish background, zero use of colour anywhere in
connection to the rest of the UI"*; flags (the docket) is the closest
screen; the bar's behaviour should be GitLab's, its style uniquely
mikroview's.

→ Round 7 restores the bar and draws it in the docket's inks. The
sentence-as-editor does not carry forward.

