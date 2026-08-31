# Entities — the ratified design (#489, under #518)

Ratified by the owner 2026-08-23 across four rounds (`round-1/` to
`round-4/` beside this file carry the mockups, screenshots and the
verbatim verdict trail; the same trail is on #518). The ruling on the
two surviving concepts — the survey and the field guide — was "both",
so this document is the consolidated record of how they combine. The
mockups are reference for execution quality; where this text and a
mockup detail disagree, this text wins.

## The model

Entities is **one page with two modes**:

| Mode | What it is | How it is entered |
|---|---|---|
| **The survey** | The reading layout: the map as a column of the ledger | The page itself — always |
| **The sitting** | The naming flow: unnamed things dealt as evidence dossiers, one at a time | "Begin the sitting" on the unnamed fold; Edit on any named row reopens its card |

The old page's four stacked lists are gone. Type is the stable
identity: **Hosts · Rules · Ports** as the house tablist, one ledger
per tab, unnamed rows folded first ("Waiting for a name"), named
after. One filter field (key, name, tags) and an "Unnamed only" lens
compose above the ledger with a live match count; `/` focuses the
filter; filtered-empty names both knobs and offers a door out of
each.

## The survey (the page)

- **The map is a column of the ledger.** Every row draws its own mark
  in its own plot cell, vertically centred on the row — alignment is
  a fact of construction, never sync code. The plot column carries a
  horizontal mini-axis per tab: hosts plot the **octet** across the
  LAN's ruler with the **outside** as a hatched, declared sliver at
  the edge; ports plot the **banded ruler** (well-known · registered
  · ephemeral, each band to its own declared stretch); rules plot the
  **tally bar** (width = the hour's firings, ink = the fall's
  verdict colours, count printed, scale declared on the ruler).
- **The seam** between plot and record drags — wide (octet sublabels
  surface) down to a sliver of pins — and is a **per-user
  preference, persisted and applied before first paint, never moved
  by the app**: the same grammar as the rail's density and metrics'
  view choice. `[` / `]` nudge it from the keyboard. Below sliver
  width the plot column drops out, leaving the plain roll.
- **Honest geography only.** Real coordinates (octet, port number)
  are drawn to scale and declared; the outside has no map and says
  so; rules have no chain order in the logs, so their axis is the
  tally. The unclaimed range is the long empty middle of every host
  row — absence as information.
- Marks: solid pin = named, dashed hollow pin = unnamed; the amber
  seen-this-hour tick lives in the record where the facts are.
  Inline naming in the row (Enter saves, Esc cancels, the row keeps
  its place; one row edits at a time) remains for the quick single
  christening.

## The sitting (the naming flow)

- **You can't name what you can't see.** "Begin the sitting" deals
  each unnamed thing as a **dossier of field marks**, assembled
  entirely from arrived traffic: cadence sparkline (the fall's inks,
  scale declared, floored at 12/min), arrival, the rules it tripped,
  the doors it tried / who tripped it. An honesty line on every
  card: nothing was probed, resolved or looked up.
- **The deck is cross-type and loudest-first** — the census's to-do
  list is one list, and what shouted is worth naming first.
- **The deck remembers**: a card cites entities named earlier in the
  sitting by their new names — each christening makes the next
  card's evidence more legible.
- Keyboard is the whole game: `Enter` names and deals the next;
  `Space` skips to the back of the deck (never to oblivion — the
  count never lies); `Esc` closes the sitting with nothing lost.
  Each deal is announced with the key and its loudest fact.
- **An earned empty**: "The census is complete" when the deck runs
  dry; a new unknown thing deals itself a card. **Edit** on a named
  row reopens that thing's dossier card prefilled — the dossier is
  always one click away.

## Mobile — the pocket survey

The record is the page, full-width; the plot column folds into a
left-edge **drawer behind a pull tab** that names what it holds. The
drawer holds no verb: choosing a stake closes it onto that thing's
row; scrim, drag handle, Esc and a left-drag close it; 200 ms slide,
instant under reduced motion. The sitting works unchanged on a phone
(one card is one screen). The mobile shell itself is #486's
question.

## Shared grammar

- **Viewer (#490, wording since #653):** READ-ONLY declared once in the
  header chip; Name it, Edit, Remove, + Add entity, Begin the
  sitting — absent, never disabled; the ledger and its facts
  unchanged. A viewer's page has no sitting to enter; "N not yet
  named" is stated as a fact, not a door.
- **Colour is meaning.** Chart ink appears only on the rules tally
  bars and the dossier sparklines — the fall's two inks (traffic
  blue, refused red), validated per surface in `round-2/README.md`
  (all checks pass, dark and light). Amber is time's tick and never
  a series; alarm appears nowhere on this page; identity is text;
  everything survives greyscale.
- **Nothing probes.** Every fact on the page arrived on the router's
  own push. Rule keys stay verbatim from RouterOS; the census
  decorates keys with names, never rewrites them.
- Accessibility: the plot column is spoken, not shown twice —
  position is announced per row in words ("at .77, on LAN ground");
  arrows walk rows; nothing focusable pretends to be pressable.

## Superseded (considered and closed)

- **Round 1, W — the census** (accepted as baseline, then
  superseded): its tabs, fold, filter/lens grammar and inline naming
  are absorbed above; its plain roll survives as the below-sliver
  and mobile-record form.
- **Round 2, X — the ledger view** (the alphabetical dictionary
  behind the deck): superseded by the survey as the browse layout;
  the deck, the dossier card, the earned empty and deck-remembers
  are absorbed above.
- **Round 2, Y and round 3, AA — the survey's earlier bodies**
  (horizontal terrain; vertical terrain with the roll beneath/beside
  as separate panels): superseded by round 4's in-step construction
  on the owner's notes (width adjustable; items aligned to rows).
