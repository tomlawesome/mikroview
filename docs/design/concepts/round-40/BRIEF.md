# Round 40 brief — the topography as a city (#854)

Owner's vision, 2026-09-02: the topography stops being a top-down flow
chart and becomes **a city**: the whole estate on one big map.

- **Buildings** are the things on the network, shaped by type and sized by
  traffic: WAN sources as gates at the edge of town, routers as the big
  civic buildings, hosts as the buildings inside their district.
- **Districts** are VLANs; a router's territory is a **borough** made of
  its districts. A second router is another borough on the same map.
- **Roads** are traffic: width for volume, colour for verdict (accept
  `--accept`, drop `--drop`, flagged `--alarm`); the Internet is the
  highway out of town.
- **Height is importance**, not traffic. The `survey` altitude stop
  becomes a skyline: important buildings stand tall, flagged ones light
  up, dark (unlogged) districts sit unlit.

Every fact round 30 draws on the topography keeps a place: coverage
badges per boundary (LOGGED BOTH WAYS / DARK / DARK TOWARD WAN / QUIET),
the escalated UNPLANNED callout, watch counts (◉), flag counts (✱), the
WireGuard node, the lens tabs (traffic · policy · coverage · flags N ·
watch), the altitude axis (clients · services · zones · survey).

## Data story (one story, every direction — do not vary it)

Primary borough — **rb5009**, RouterOS 7.20.1, 41 rules, LIVE 34/s.
WAN: `ether1` → Internet 203.0.113.7, coverage LOGGED. WireGuard `wg0`
10.99.0.0/24, QUIET, 1 watcher.

| District | Subnet | Coverage toward WAN | Hosts |
|---|---|---|---|
| LAN | 10.0.10.0/24 | LOGGED BOTH WAYS | tom-desktop, phone-tom, laptop-anna, tv-lounge |
| Servers | 10.0.20.0/24 | LOGGED BOTH WAYS | nas, pihole, unifi |
| IoT | 10.0.30.0/24 | DARK TOWARD WAN | cam-porch ✱✱, hue-bridge, thermostat, doorbell, esp-weather, plug-kettle, +4 |
| Guest | 10.0.40.0/24 | DARK — no log rule on this boundary | guest-e8b2, +2 |

Second borough — **hap-ax3**, RouterOS 7.20.1, 12 rules, in the workshop.
Its `ether1` carries 10.0.10.9 — inside rb5009's LAN — so the borough is
reached by a road from LAN. Districts: **Workshop** 10.0.50.0/24 (cnc,
printer-3d, pc-bench), coverage LOGGED BOTH WAYS; **Cams** 10.0.60.0/24
(cam-yard, cam-gate), coverage DARK.

Traffic: `any → wan · 9/s`; LAN ↔ Servers heavy (`:53 :123`, `:445 :5001`);
IoT → Servers modest; **UNPLANNED · iot → lan · tcp/445 · caught by
default drop · 14×** (cam-porch → tom-desktop) — the one alarm road; Guest →
wan only; Workshop → Servers (`:445`, backups) light; Cams → nas steady.
Flags 6 open: cam-porch ✱✱, doorbell ✱, hap-ax3's cam-gate ✱, two on lan.

Importance readings (each direction offers both, a small toggle
`importance: depended-on · watched`):
- **depended-on** — how many distinct hosts talk to it in the window:
  rb5009 tallest; pihole (every host asks DNS) and nas tall; unifi mid;
  hap-ax3 mid; tom-desktop mid; phones/IoT low; cams low.
- **watched** — watch/flag weight the operator has put on it: cam-porch
  tall (2 flags + a watch), wg0 mid (watch), nas mid (watch), rest low.

## Scenes every direction proves (same ids, so one capture script fits)

1. `#survey` — the skyline: the whole estate from above, height on.
2. `#street` — street level in the LAN district: buildings, their roads,
   the labels that only appear close up.
3. `#estate` — the two boroughs and the road between them, the hap-ax3
   borough clearly a second router.
4. `#alarm` — the UNPLANNED road lit and cam-porch's building lit; the
   Guest district unlit (dark coverage).

Each scene is a `<section>` 1400×860 at most, self-contained (no build
step, inline CSS/SVG, real CSS motion with `prefers-reduced-motion`
respected, aria labels on every scene and building). Use round 39's
tokens verbatim (below). No new inks beyond them.

```
--void:#06080e; --raised:#0f1422; --glass:rgba(15,20,34,.66);
--hair:rgba(160,185,230,.13); --hair-2:rgba(160,185,230,.26);
--ink:#e9eefb; --ink-2:#97a4c4; --ink-3:#55628a; --accent:#9db8e8;
--lan:#3987e5; --srv:#199e70; --iot:#c98500; --guest:#d76a9e;
--ok:#37b364; --alarm:#ff5470; --now:#e8b05a;
--accept:#3ecf7e; --drop:#f5a623; --nat:#2dd4bf;
--sans: system-ui,-apple-system,"Segoe UI",sans-serif;
--mono: ui-monospace,"SF Mono",Menlo,Consolas,monospace;
```

Workshop and Cams need lane inks: reuse `--lan`/`--srv` hues at reduced
saturation within the second borough (a borough's districts are ranked
within the borough), never purple (ratified for watchers).

## Directions

- **Direction R — isometric.** True 2.5D: districts are isometric
  plates, buildings extruded blocks whose height is importance, roads
  ribbons on the ground plane. The survey is the native view; street
  level is the camera dropped into one district.
- **Direction S — relief.** A plan view (top-down blocks, like a city
  plan) where height shows as shade and contour at the lower stops, and
  the survey stop tilts the same plan with a CSS 3D perspective so the
  blocks rise — one drawing, one camera.

Gates before the owner sees anything: screenshot every scene, look at
every shot, fix collisions and density, recapture. Density is a defect.

## Second cut (owner verdict, 2026-09-03: isometric, with caveats)

Relief is dropped. `isometric.html` is rebuilt with three changes; nothing
else in this brief changes.

1. **Roads curve.** No straight segments and no elbows: every road is a
   smooth ground-plane curve (cubic Béziers in ground coordinates, then
   projected), leaving and arriving at a building's footprint edge at a
   tangent. Roads bend around plates rather than cutting corners across
   them. The highway out is a long sweeping curve off the map.
2. **The map is bigger than the screen.** The view already pans and
   zooms, so stop squeezing the estate into 1400×860. Lay the town out on
   a ground plane roughly 2.5× the viewport in each direction, with real
   distance between districts and between the two boroughs (the workshop
   borough is *down the road*, not next door). Each scene is then a
   camera: `#survey` is zoomed out enough to see the whole estate small
   (the skyline reads as silhouettes and lit points; plaques collapse to
   district names only); `#estate` frames the two boroughs and the road
   between them at mid zoom; `#street` and `#alarm` are close. Include a
   fifth scene `#pan` showing the same map mid-pan with the minimap /
   scrollbars that tell the operator there is more beyond the edge.
   Density on any one screen is still a defect — now solved by zoom, not
   by cramming.
3. **Devices, not cubes.** A small library of generic isometric device
   shapes, one per type, drawn once as SVG symbols and stamped: **router**
   (flat wide chassis, a row of port lights on the front face, two short
   antennas on the hap-ax3 variant), **switch** (the same chassis, longer
   and thinner, many port lights), **server box** (tall upright box, drive
   bays as horizontal slits, one status light), **workstation** (a tower
   beside a monitor), **laptop** (open lid, screen face lit), **phone**
   (thin upright slab, screen lit), **TV** (wide thin slab on a stand),
   **PoE camera** (a bullet body on a short post, lens facing the road),
   **IoT puck** (a low flat disc for hue-bridge, thermostat, plug-kettle,
   esp-weather, doorbell), **gateway post** for ether1/wg0 (a bollard with
   a light), and the **Internet** as the highway itself vanishing at the
   horizon, not a box. Every shape keeps the same VLAN-tinted three-face
   shading so the district still reads; height = importance scales the
   shape's plinth (a raised base under the device), not the device itself,
   so a phone never becomes a skyscraper — the plinth does the talking.
   Flagged buildings glow from their status light and lens.

Type per building in the data story: rb5009 router, hap-ax3 router (with
antennas), nas/pihole/unifi server boxes, tom-desktop workstation,
laptop-anna laptop, phone-tom phone, tv-lounge TV, cam-porch/cam-yard/
cam-gate PoE cameras, hue-bridge/thermostat/doorbell/esp-weather/
plug-kettle IoT pucks, guest-e8b2 phone, cnc/printer-3d IoT pucks on a
taller plinth, pc-bench workstation.

## Walls and gates (owner, 2026-09-03: "visualise the firewall", "VLANs as gated communities")

4. **Every district is a gated community; the firewall is its walls and
   gates.** The district plate gets a low isometric wall around its edge.
   A road crosses a wall only through a **gate**, and a gate *is* an
   accept rule for that boundary (chain + in/out interface), labelled at
   street level with the rule in plain terms (`lan → servers · :53 :123 ·
   accept`). A **lamp** on the gate means the rule logs; a gate without a
   lamp accepts silently; a wall with no lamp anywhere toward a neighbour
   is that boundary's DARK badge made visible. A **drop** is a road that
   ends at the wall: bollards and a red mark where it hits, no gate — the
   UNPLANNED road stops at LAN's wall with "caught by default drop"
   beside the mark. The router is the civic building whose plaque carries
   the rule count; in the **policy** lens the roads fade and every gate
   lights with its rule number, so "41 rules" becomes 41 gates and wall
   segments you can walk. The **traffic** lens is the reverse: roads
   strong, walls quiet.
5. Sixth scene `#walls`, policy lens, close camera on the LAN ↔ IoT ↔
   Servers corner: the lan → servers gate open and lamped; the iot → lan
   road stopped at LAN's wall with bollards, red mark and the UNPLANNED
   callout; Guest's wall with one gate to the highway and no lamp.

## The river and its bridges (owner, 2026-09-03: "L2TP and WG tunnels could be visualised as bridges over a river")

6. **The Internet is a river along one edge of the town; everything that
   crosses it is a bridge.** The WAN highway crosses on the main road
   bridge at ether1 (wide, busy, lamped = LOGGED). Each tunnel is its own
   bridge to the far bank: **wg0** a narrow footbridge, **l2tp** a second
   one. A bridge is *up* when it is lit along its length and *down* when
   it is drawn as unlit piers with the deck missing. A quiet tunnel is a
   lit bridge with no traffic on it (wg0's QUIET badge hangs from it). On
   the far bank, a small hamlet of the tunnel's peers — the remote
   buildings, drawn with the same device shapes (phone-tom-away as a
   phone, anna-remote as a laptop) — so a road warrior is literally
   across the water. The river replaces the "Internet" box entirely.
   Data story additions: `l2tp · 1 peer · anna-remote · UP · 0.3/s`; wg0
   peers `phone-tom-away` (QUIET). The `#survey` scene shows the river
   and all three bridges; `#estate` keeps its framing but the river is
   visible at the map edge.

## Two views, not a replacement (owner, 2026-09-03)

The city does not replace the 2D topography: it is a second view of the
same estate. The chrome gets a `map · city` switch beside the lens tabs
(city selected in every scene here); the altitude axis is replaced in the
city by a zoom control (− · + and a "fit" button) and the minimap; the lens
tabs apply to both views. The 2D map keeps its own drawing and its own
issues (#726, #715, #701, #852).

## One slider (owner, 2026-09-03, replacing the `map · city` switch)

> The slider would have all the same levels, with the major city view
> being the default, with the slide in the middle. Move left, and you move
> into 2D at different levels. Move right, and you change through the 3D
> city levels.

So there is no switch: the altitude axis stays, with the city at its
centre and as the default. Stops, left to right:
`clients · services · zones · ◆ city · borough · district · street`.
Left of centre is the 2D map at its existing stops (the 2D `survey` stop is
gone — the city is the survey); right of centre is the city zooming in:
**borough** frames one router's territory, **district** one gated
community with its gates, **street** the buildings with labels and cards.
Pan is free at every city stop; the zoom stop sets the camera height. The
scenes map onto stops: `#survey` = city, `#estate` = city (panned to show
both boroughs), `#pan` = borough mid-pan, `#walls` = district, `#street`
and `#alarm` = street. Show the slider in every scene with the right stop
lit; the minimap stays.

## The reach in the city (owner, 2026-09-03: "We will need a reach type view for the city style, too")

7. The 2D reach (#626: recentre on one host; its strands per direction,
   accepted ones pass the membrane, dropped ones die at it; the composer
   drafts the refusing rule) becomes **standing on a building**. Clicking
   a building at any city stop drops the camera to the street stop on
   that building and fades every road that is not its own; its roads
   light with direction shown by the flow (dashes moving away = it
   spoke, toward = it was spoken to). Accepted roads pass through the
   district's gates and light the peer buildings at the far end, with the
   ports on the road; a refused road ends at the wall with bollards, the
   red mark and the refusing rule's name on the wall (`caught by default
   drop`). The breadcrumb card (`tom-desktop · 10.0.10.21 · reaches 4 ·
   reached by 6`) sits at the top like the 2D crumb; the **composer** is a
   card pinned to the wall where a refused road stopped — "it's been
   asking · tcp/445 · 14×" and the printed RouterOS line for a new gate,
   drafted never run. Esc or the crumb surfaces to the stop you came from.
   Scene `#reach`: standing on tom-desktop at street stop, its three
   accepted roads lit through the LAN gates to nas/pihole and the
   highway, cam-porch's refused road dead at LAN's wall with the composer
   card on the wall.
