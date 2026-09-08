# The city — the ratified design (#854)

Ratified by the owner 2026-09-03 on round 40's second cut
(`../../concepts/round-40/isometric.html`, README and BRIEF.md beside it
carry the verdict trail verbatim; the same trail is on #854). This is the
consolidated record the build implements from. The mockup is reference
for execution quality; where this text and the mockup disagree, this
text wins.

**Amended by round 49 (#1016), ratified by the owner 2026-09-07**
(`../../concepts/round-49/`, README beside it carries the rule and the
decision trail). Round 49 changes what is always on and what brightness
means; everything else here stands. The two rules it adds are in "The
always-on picture" below, and they apply to the 2D map identically —
the two surfaces are drawn to one rule so they read as one product.

## The model

The city is a **second view of the same estate the 2D topography
draws**, not its replacement. One altitude slider carries both:

```
clients · services · zones · ◆ city · borough · district · street
```

- **◆ city** is the centre and the default: the whole estate from above,
  the skyline.
- **Left of centre** is the existing 2D map at its existing stops
  (`Topography.svelte`); its old `survey` stop is gone — the city is the
  survey. **Both views share one ground plan** (owner, 2026-09-03, #852):
  the same positions for every zone, router, tunnel, river and road;
  only the vocabulary changes. `zones` is the plan drawn flat and
  carries less than the city at the same place — cards with a host
  count, no dots, no per-host labels; the owner read the first flat
  example (`../../concepts/round-40/one-layout.html`) as cluttered.
- **Right of centre** zooms the city in: **borough** frames one router's
  territory, **district** one gated community and its gates, **street**
  the buildings with their labels and cards. Pan is free at every city
  stop; the stop sets the camera height. A minimap with the viewport
  rectangle is shown at every city stop.
- Two overlay pills (`⚑ flags`, `◉ watch`) apply to both views; the
  header, badges and callout wording are the 2D map's. There are no
  lens tabs: traffic is the picture, coverage is always on, policy is
  gone (round 49).

**The join (#869).** One `<input type=range>` with seven stops carries
the whole axis above; which stop was last open persists per user.
Crossing the centre swaps
the drawing, and carries what still makes sense rather than resetting
everything: the selected lens always carries (it is one piece of state
threaded to both sides); a reach -- a host held open on the 2D map, or
a building stood on in the city -- carries only if the same host exists
as a building on the other side, and otherwise surfaces rather than
half-applying; the city's own pan carries across its own mount and
unmount (it only exists in the DOM while a city stop is active), so
returning to the city lands back where it was left.

## The metaphor, and what each part means

| City | Network | Drawn as |
|---|---|---|
| Building | a host, a router, a gateway | an isometric device shape by type, VLAN-tinted three-face shading, flat on its district plate |
| District | a VLAN / zone | an isometric plate with a low wall around its edge, its name and subnet on a plaque; the wall's material says the coverage, so no badge |
| Borough | a router's territory | that router's districts, grouped; a second router is a second borough down a road |
| Road | traffic between two buildings or a building and a gate | a curved ground ribbon: width = volume, colour = verdict (`--accept`, `--drop`, `--alarm` for an escalated unplanned road) |
| Gate | an accept rule crossing a district boundary | a break in the wall; a road crosses a wall only through a gate. Its posts are hoverable and its card names the rule |
| Lamp on a gate | the rule logs | a lit post; a wall with no lamp toward a neighbour is that boundary's dark made visible |
| Bollards and a red mark on a wall | a drop — the road ends at the wall | the plain mark only, reading `dropped`; **the refusing rule's name is not written on the drawing** — it lives in the card (#991, kept by round 49) |
| River | the Internet | along one edge of the map; there is no Internet box. It must read as water — banks, an uneven edge, a ripple texture, no lane marks — the owner read the mockup's river as a road (2026-09-03) |
| Road bridge | the WAN interface (ether1) | wide, lamped when logged |
| Footbridge | a tunnel (wg0, l2tp) | narrow; lit when up, piers only when down, lit but empty when quiet |
| Far bank hamlet | the tunnel's peers | the same device shapes across the water |
| Dark boundary | nothing logged on that boundary | grey translucent wall faces, dashed top edge, grey posts, no lamp, no road across it |
| Quiet on purpose | the operator declared the gap | white translucent faces and deck, no lamp; the reason is in the card |
| Lit building | an open flag | glow from the device's status light or lens |

**Roads are on the ground.** Everything is painted back to front in one
depth order (road pieces and buildings interleaved), so a building nearer
the camera occludes a road behind it. Roads leave and arrive at a
footprint edge at a tangent; no straight runs, no elbows; a road that
would cross a plate goes round it and through a gate.

## Height (dropped)

#867 built a plinth under each building whose height read one of two
importance readings, depended-on or watched, toggled per user. The
owner ratified dropping the height concept altogether (#981); #986
removed the toggle, the plinth and both readings. Devices now sit flat
on their district plate.

## The device library

One SVG symbol per type, stamped per building, all with the same
VLAN-tinted three-face shading so the district still reads:

router (flat wide chassis, port lights), router with antennas (the
downstream/secondary router), switch (longer, thinner, more lights),
server box (tall, drive-bay slits, one status light), workstation (tower
beside a monitor), laptop (open lid, lit screen), phone (thin upright
slab), TV (wide thin slab on a stand), PoE camera (bullet on a post, lens
toward the road), IoT puck (low flat disc), gateway post (bollard with a
light, for interfaces and tunnel ends).

Type comes from what mikroview knows — the entities register's device
kind where it has one, otherwise a guess from name and traffic shape,
shown as the generic puck until better is known. A wrong shape is a
labelling defect, never a data claim.

## The always-on picture (round 49, #1016)

Two rules, drawn the same way on both surfaces. Nothing is written on
the map that the drawing already says, and nothing is ever removed from
the map — only dimmed.

**1. Material is coverage.** How an element is drawn is what its own
boundary's logging is: solid ink where a rule logs, white translucent
where the operator declared the gap quiet on purpose, grey dashed where
nothing logs. There is no coverage lens and no coverage badge; the
per-element table is in `../../concepts/round-49/README.md`.

**2. Colour is the verdict; brightness is the baseline.** A **line** is
`source → destination · port · proto`. A line seen on **3 distinct days
of the last 14** is *established* and recedes — thin, dim, no flow. A
line off that pattern is *off-baseline*: full width, bright, flow
dashes, and a small ring throbbing in place at the end it arrived at.
Refused stays red with the ✕ where the rule stopped it. Both thresholds
are configurable; the baseline is learned from recurrence and never
maintained by hand.

The purpose is the sieve (owner, 2026-09-07): make traffic that should
not be happening easy to see, and whittle the noise down to genuine
candidates, without arbitrarily hiding anything that could be bad. An
established network sits and talks on the same routes and ports, so what
matters is a line off that pattern — traffic more than devices.

**Roll-up.** A road, rib or host dot is as bright as the brightest line
it carries: one off-baseline line among a thousand lights the one place
it happened, and the line count never grows with traffic (one road per
district pair, one lane per host to its gate). Only the reach draws per
line, for one host by choice. The header counts them,
`⟡ off-baseline today · N`.

**Saying it is expected.** The card on a bright element lists the lines
with first-seen time and count, and offers `expected ▸`: a reason,
recorded with who said it, and the line is established from then on.
That is the only way a line leaves the bright state early.

**The two overlay pills**, both on by default, greyed when off:
- `⚑ flags`: flagged buildings and host dots take a red halo that hugs
  the shape and throbs in place; the escalated unplanned road stays in
  alarm ink with its callout.
- `◉ watch`: watched buildings and dots ringed in the 2D map's watcher
  ink.

## The reach

**Clicking anything on the map, at any stop, opens its reach** — a
building, a host dot, a district, a road (round 49). A reach answers
"where does this thing connect to", and the subject is whatever was
clicked: a **host**, a **zone** (the district, or the 2D lane plate), or
a **rib** — the line between two zones, drawn as a road between
districts in the city (ratified 2026-09-08, #1016). A zone's own side is
its boundary interface, so traffic that came and went through it counts
and traffic that never left it does not; a rib is the pair of
interfaces, both directions of it are one line, and its card lists the
ports that line carried, which is what a rib can say that the drawing
cannot. All three read the same crumb and the same three counts.
Standing on a building drops the camera to the street stop on it and
fades every road that is not its own. Its roads light with direction
shown by the flow
(dashes moving away = it spoke, toward = it was spoken to); accepted
roads pass the district's gates and light the peer buildings; a refused
road ends at the wall with bollards and the red mark, reading `dropped`.
**Nothing is written on a road or strand** — no pill labels and no rule
names, on either surface; the ports and the refusing rule both live in
the card. That is one rule, not two: if it names something, it is in a
card. Established
strands recede and off-baseline ones are bright, by the same rule as
everywhere else. The crumb card
(`name · ip · reaches N · reached by N · refused N · Esc surfaces ▸`)
sits at the top, the same wording on both surfaces. The **composer** is
a card pinned to the wall where the
refused road stopped: "it's been asking · tcp/445 · 14×" and the printed
RouterOS line for a new gate — drafted, never run, the same invariant as
the 2D composer. Esc or the crumb surfaces to the stop and position you
came from.

## Living hosts (round 49, #1016)

Hosts come and go by what the syslog feed shows, and they are drawn at
every stop, including the top-level map — as buildings in the city and
as a row of dots inside each lane card in 2D (ten dots then `+N`, every
dot clickable to its reach).

A host is `live`, `quiet`, `intended` or `dismissed`. **Quiet is 24
hours of nothing** (owner, 2026-09-07; configurable, 24 h default) —
presence is housekeeping, not the sieve, so a short silence means
nothing. A quiet host is not removed: it goes grey with a dashed
footprint and says how long, e.g. `quiet · 26 h`. The operator can mark
it `quiet on purpose`, which draws it white translucent and keeps the
reason, or `dismiss` it, which removes it. Either way, if it reappears
in the feed it comes back by itself. Neither state claims more than
"not heard".

## Cards

Cards are the one interaction, the same on both surfaces: hover to
open, and every card has a **pin** so it stays when the pointer leaves.
Boundary card (dark): the gate's own `rule N · name` (owner,
2026-09-08 on #1016 — numbered as RouterOS numbers it, and on the card
only, never on the drawing; a rule with no comment is shown by its
number alone), what the rule does, both directions, and
`declare quiet on purpose ▸ · rules ▸ · stream ▸`; pinned it opens the
declare form — reason (required), `both directions ☑`, `Declare`, and
who. Quiet card: the reason quoted, who and when, `undeclare ▸`. Host
card: presence, last and first seen, events, `mark quiet on purpose ▸ ·
dismiss ▸`. Line card in the reach: the port / proto / accepted /
dropped table, the totals, `:22 refused by #17 default drop`, and on a
refused strand the composer's `draft the rule ▸`. Off-baseline card:
the lines, first seen, count, and `expected ▸`. Every card links on
into the flags, the watchers and the stream.

## Labels

Plaques and road labels are placed by camera: at city stop only district
names, router names and subnets; port labels only at street. `no rule
table pushed` stays as a dim second line on the plaque, because it is a
different fact from dark. A label that cannot be placed without
collision moves before it hides; hiding is the last resort and never
applies to an escalated callout.

## Honesty and motion

Everything drawn arrived; nothing was provoked. A boundary with no log
rule is grey and dashed, never guessed at, and no road is drawn across
it — a road there would claim a log line that was never written.
Motion: road flow dashes on off-baseline lines, the flagged halo and the
off-baseline ring throbbing in place (never pulsing outward, owner
2026-09-07), camera moves between stops; all instant under reduced
motion. Every building and
district has an accessible name; the
keyboard walks buildings within a district and districts within the
map, Enter stands on a building, Esc surfaces.

## What the 2D map keeps

Its own drawing and its own open work: #726 (edge overlap), #715
(fidelity against round 30), #701 (facts no data answers). #852 (zone
identity per device) closed into the one-ground-plan ruling above: the
2D map draws the city's boroughs flat (#869).

## Build

Ratified on #854 (closed); built in milestone M6 — The city: #863 ground
model and cameras, #864 device library, #865 walls and gates from the
rule set, #866 river and bridges from interface and tunnel state, #867
importance readings, #868 the reach, #869 the slider's join with
`Topography.svelte`, #870 demo feeder data, #874 the pushed tunnel
state #866 reads, #877 the same state as the 2D map's tunnel node.

Three wordings settled during the build, because each is a place the
drawing could have claimed more than the app knows:

- A footbridge with no pushed tunnel state reads **state not pushed**,
  and a tunnel known only from its own events reads identically — from
  the operator's chair those are the same fact. The road bridge never
  reads up or down at all; its lamp says a rule logs that boundary and
  nothing more (#866, #874).
- A district on a router with no pushed rule table draws **no gates** and
  says no rule table has been pushed yet. That is a different statement
  from a dark boundary, where a table exists and nothing on it logs
  (#865).
- Both views escalate the same unplanned pair through one shared
  function, `worstUnplannedOf` in `frontend/src/lib/reality.ts` — busiest
  first, ties on drops, then the pair's own key. Two implementations of
  one rule agree the day they are written and diverge on the first
  one-line change to either (#865, #715 item 4).

Round 49 is built in M11 under #1016: the always-on treatment and
declare on both surfaces, the overlay pills, hosts at the top level,
click-anything reach with pinned cards, living hosts, the baseline
register and `expected`, and live-gate scenarios for all of it.

## Superseded

- The **coverage lens** on both surfaces, and the coverage badges
  `LOGGED` / `DARK` / `QUIET` on plaques and 2D card captions
  (`COVERED — logged or declared quiet`): the material carries it, always
  on. Nobody should have to click a tab to learn a boundary is unlogged
  (owner, 2026-09-06).
- The **policy lens** — a line per firewall rule, "never exercised" when
  unused: nobody asked for it (owner, 2026-09-06). The walls and gates
  already are the rules.
- **traffic as a lens**: it is the picture.
- **Pill labels on reach strands** and ports written along a road: the
  card carries them.
- **Height / plinths** (#981, above), and the `survey` stop (the city is
  the survey).
