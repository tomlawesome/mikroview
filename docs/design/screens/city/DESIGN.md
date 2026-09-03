# The city — the ratified design (#854)

Ratified by the owner 2026-09-03 on round 40's second cut
(`../../concepts/round-40/isometric.html`, README and BRIEF.md beside it
carry the verdict trail verbatim; the same trail is on #854). This is the
consolidated record the build implements from. The mockup is reference
for execution quality; where this text and the mockup disagree, this
text wins.

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
- The lens tabs (traffic · policy · coverage · flags · watch) apply to
  both views; the header, badges and callout wording are the 2D map's.

## The metaphor, and what each part means

| City | Network | Drawn as |
|---|---|---|
| Building | a host, a router, a gateway | an isometric device shape by type, VLAN-tinted three-face shading |
| Plinth under a building | importance (two readings, below) | a raised base; the device itself never scales |
| District | a VLAN / zone | an isometric plate with a low wall around its edge, its name, subnet and coverage badge on a plaque |
| Borough | a router's territory | that router's districts, grouped; a second router is a second borough down a road |
| Road | traffic between two buildings or a building and a gate | a curved ground ribbon: width = volume, colour = verdict (`--accept`, `--drop`, `--alarm` for an escalated unplanned road) |
| Gate | an accept rule crossing a district boundary | a break in the wall; a road crosses a wall only through a gate |
| Lamp on a gate | the rule logs | a lit post; a wall with no lamp toward a neighbour is that boundary's DARK badge made visible |
| Bollards and a red mark on a wall | a drop — the road ends at the wall | with the refusing rule's name beside the mark (`caught by default drop`) |
| River | the Internet | along one edge of the map; there is no Internet box. It must read as water — banks, an uneven edge, a ripple texture, no lane marks — the owner read the mockup's river as a road (2026-09-03) |
| Road bridge | the WAN interface (ether1) | wide, lamped when logged |
| Footbridge | a tunnel (wg0, l2tp) | narrow; lit when up, piers only when down, lit but empty when quiet |
| Far bank hamlet | the tunnel's peers | the same device shapes across the water |
| Unlit district | nothing logged on that boundary | plate and buildings dim, coverage badge in alarm ink |
| Lit building | an open flag | glow from the device's status light or lens |

**Roads are on the ground.** Everything is painted back to front in one
depth order (road pieces and buildings interleaved), so a building nearer
the camera occludes a road behind it. Roads leave and arrive at a
footprint edge at a tangent; no straight runs, no elbows; a road that
would cross a plate goes round it and through a gate.

## Height = importance

Two readings, a small toggle on the survey (persisted per user):

- **depended-on** (default): how many distinct hosts talk to it in the
  window — routers tallest, then the services everyone asks (DNS, NAS),
  then workstations, then phones and IoT.
- **watched**: the flag and watch weight the operator has put on it — a
  twice-flagged camera becomes the spike; everything unwatched sits low.

The reading changes the plinth height with a transition; under reduced
motion it snaps.

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

## The lenses in the city

- **traffic**: roads strong, walls quiet.
- **policy**: roads fade; every gate lights with its rule number and the
  wall segments read as the rules — "41 rules" becomes 41 gates and walls
  you can walk.
- **coverage**: lamps and unlit districts dominate; coverage badges on
  every plaque.
- **flags**: flagged buildings lit, everything else dim; the escalated
  unplanned road in alarm ink with its callout.
- **watch**: watched buildings ringed (the 2D map's watcher ink).

## The reach

The 2D reach (#626) becomes **standing on a building**. Clicking a
building at any city stop drops the camera to the street stop on it and
fades every road that is not its own. Its roads light with direction
shown by the flow (dashes moving away = it spoke, toward = it was spoken
to); accepted roads pass the district's gates and light the peer
buildings, with the ports on the road; a refused road ends at the wall
with bollards, the red mark and the refusing rule's name. The crumb card
(`name · address · reaches N · reached by N · Esc surfaces`) sits at the
top as in 2D. The **composer** is a card pinned to the wall where the
refused road stopped: "it's been asking · tcp/445 · 14×" and the printed
RouterOS line for a new gate — drafted, never run, the same invariant as
the 2D composer. Esc or the crumb surfaces to the stop and position you
came from.

## Labels

Plaques and road labels are placed by camera: at city stop only district
names, router names and the badges that matter (DARK, QUIET); port
labels only at street. A label that cannot be placed without collision
moves before it hides; hiding is the last resort and never applies to a
coverage badge in alarm ink or an escalated callout.

## Honesty and motion

Everything drawn arrived; nothing was provoked. A district with no log
rule is unlit, never guessed at. Motion: road flow dashes, the plinth
transition, camera moves between stops; all instant under reduced
motion. Every building and district has an accessible name; the
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
