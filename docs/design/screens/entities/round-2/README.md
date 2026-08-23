# Entities (#489, under #518) — round 2

The wild passes, on the owner's ask (verbatim, recorded in
`../round-1/README.md`): "take a couple of wild passes at this, really
go out of the box and explore what we could do here if don't just
stick to convention". Round 1's **W (the census)** stands accepted as
the baseline. Letters run on: this round is **X** and **Y**.

Both directions keep the round-1 data story (the hour 13:02–14:02;
203.0.113.44's 13:52 burst at 88/min, 1 206 events this hour; r7's 88
hits all inside 13:52; the named hosts/rules/ports and their facts
verbatim), carry the ratified shell (#486) forward verbatim, prove the
same three scenes dark and light, and keep W's invariants: facts from
arrived traffic only, naming never moves anything under the operator,
and #490's read-only grammar (absent, never disabled) for viewers.

## Direction X — the field guide

Naming as **identification**, not list admin. The premise: you can't
name what you can't see, and every conventional page (W included)
hands the operator a bare key and a text field as if the name were
already known. The field guide deals each unnamed thing as a dossier
of field marks — cadence sparkline, arrival, the rules it tripped,
the doors it tried — assembled entirely from arrived traffic, and the
operator names the thing from its behaviour, one card at a time,
until the deck runs dry.

- **The deck is cross-type** (hosts, rules, ports from one deck),
  ordered loudest-first — the census's to-do list is one list.
- **The deck remembers**: r7's dossier cites scanner-jkt by the name
  coined two cards ago; each christening makes the next card's
  evidence more legible.
- **The ledger** behind the sitting is the finished dictionary,
  typographic, ordered by *name* — a dictionary is read
  alphabetically. Edit reopens the card: a named thing's dossier is
  one click away. Unnamed things wait in the deck, not the ledger.
- **An earned empty**: "The census is complete" is a real finish
  line; new arrivals deal themselves a card.
- Keyboard is the whole game: Enter names and deals, Space skips to
  the back of the deck (never to oblivion), Esc closes the sitting
  with nothing lost.
- The cost, stated on the page: one-at-a-time trades bulk-entry
  throughput for evidence.

## Direction Y — the survey

The dictionary drawn as **territory**. A list throws away the one
thing every key already carries — where it lives. Hosts are planted
on their real subnet ground (the LAN drawn to its address, foot by
foot, including the unclaimed range); visitors stand beyond the fence;
ports stand on a banded ruler (well-known · registered · ephemeral,
each band to its own stretch); rules are tally stones. Naming is
planting a label where the thing stands.

- **Honest geography only**: the LAN octet and the port number are
  real coordinates and are drawn to them. The outside has no map —
  visitors stand by last visit, declared on the terrain. RouterOS
  logs carry no chain order, so rules refuse invented geography and
  stand by the hour's tally, loudest first, declared.
- **The stake grammar**: solid plaque = named (name leads, key
  follows), dashed hollow stake = unnamed, amber tick = seen this
  hour. Same two states on every terrain.
- **The map is the index, never the record**: W's roll sits beneath
  every terrain, map and roll share one selection and one focus, and
  every act is possible from the roll alone. Screen readers get each
  terrain as one described image and the roll as the operable list.
- **Spatial findings a list cannot make**: a stranger standing
  *inside* the fence (.77) reads differently from a visitor beyond
  it; 5000 stands in registered ground near winbox — bad company; the
  unclaimed range .78–.255 is itself a finding.
- Filtered-empty dims the terrain to its contours (an empty map and a
  missing map are different facts) and the empty state below names
  both knobs, as in W.
- The cost, stated on the page: terrains spend vertical space; a
  200-host subnet needs label-lane engineering; the roll carries the
  long tail.

## Palette (dataviz six-checks validator)

The fall's two chart inks appear where the wild passes draw data: X's
dossier sparklines (single series per card — refused red for refused
traffic, traffic blue for passed; scale declared, floored at 12/min)
and Y's tally stones (height = the hour's firings, ink = the rule's
verdict). Validated categorical, `--pairs all`, against **round 2's
actual chart surfaces** (the dossier card and the terrain both sit on
the tile surface):

- **Dark** (`#3987e5`, `#e66767` on `#0d1220`): ALL CHECKS PASS —
  worst-pair CVD ΔE 19.2 (protan), normal-vision ΔE 29.0, both ≥ 3:1
  contrast.
- **Light** (`#2a78d6`, `#e34948` on `#ffffff`): ALL CHECKS PASS —
  worst-pair CVD ΔE 21.6 (protan), normal-vision ΔE 32.3, both ≥ 3:1
  contrast.

Everywhere else colour stays meaning-only per the house grammar:
accent for interactive, amber solely as time's colour (the
seen-this-hour tick), alarm appearing nowhere on the page. Identity
is text; text wears text tokens; both directions survive greyscale
(X's sparkline is one labelled series; Y's stones print their counts).

## Verdicts

Owner, 2026-08-23, on direction Y (verbatim):

> Oooo the survey is cool! But could be do it better by making it
> vertical, with the table to the right of it? That way, it can be as
> long as it likes and we use the otherwise empty space in the table.
> It would also work well for mobile view as we could have a drawer
> and a pull tab for that medium.

Read as: **Y liked; evolve, don't keep as drawn** — the survey goes
upright in round 3 (`../round-3/`): terrain vertical, the roll to its
right (the terrain as long as it likes, the roll's dead space put to
work), and on mobile the terrain becomes a drawer with a pull tab.
Vertical is also the house's own orientation — the fall drops and the
register reads downward.

**X (the field guide)** — owner, 2026-08-23, ruling on the field
guide and the survey together (verbatim):

> I guess both really, then?

Read as: **both ship, combined** — the survey (in its final round-4
form, `../round-4/`) is the page's reading layout; the field guide's
sitting is the page's naming mode. The consolidated buildable record
is [`../DESIGN.md`](../DESIGN.md). X's ledger view is superseded by
the survey page; its surviving organs (the dossier card, the deck,
the earned empty, the deck-remembers cross-references) are absorbed
into the record.
