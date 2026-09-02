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
