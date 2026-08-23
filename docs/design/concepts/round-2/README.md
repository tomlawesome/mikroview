# Interface visioning — round 2

Continues round 1 (see `../round-1/README.md`), under #482/#483/#385
phase 2. Same format: self-contained HTML, no build step, real motion,
same RouterOS-shaped data across directions.

## The owner's round-1 batch (2026-08-22), which this round answers

- **Atlas (D) strongest conceptually and visually** — gut leans Atlas.
- Worry 1: **a full live view must still exist somewhere.**
- Worry 2: **flags and watchlist must work really well** for a
  map-led model to be viable.
- Instrument (A): familiar, but its topography is weak next to Atlas.
- Brief for this round: *"Give me Atlas itself, but refined —
  simplicity, elegance, still visually striking. The horizontal style
  and two more, go wild."*

Every round-2 direction therefore proves the same three scenes:
**(1)** its topography/home, **(2)** the live view IN FULL, and
**(3)** flags + watches as first-class surfaces.

## The directions

### F — Atlas II (`direction-f-atlas2.html`) — Atlas, refined

Calmer and sparer than D: hairline casings, one accent, colour spent
only where meaning lives — the alarm is the only saturated thing on
screen. The stream is **never gone**: it runs as a ticker at the foot
of the map, lifts to a boundary dock, and lifts again to the **full
live view** while the map folds into a thin still-blinking ribbon.
Flags bloom in place with an evidence tray; watches draw as bands on
the edges they guard, and a violation visibly breaks the band.

### G — Riverline (`direction-g-riverline.html`) — the horizontal option

Outside left, inside right, the router as a vertical membrane every
flow crosses. Local traffic hairpins at the waist; the unplanned flow
is drawn **reaching the membrane and being cut there** — because that
is where the default drop kills it — with its intent continuing as a
dashed ghost that never passes. Watches are gauges clamped onto flows;
flags are buoys. The current (stream) runs underfoot and lifts to full.

### H — Halo (`direction-h-halo.html`) — wild №1, radial

> Renamed from “Orbit” (the owner already has an Orbit project),
> settled as “Halo” at the owner’s pick, 2026-08-23 — the internet
> ring *is* the halo. Radar/Sonar were rejected: they imply the
> active scanning mikroview never does.

The router is the core; the lanes orbit it; the internet is the outer
ring that truthfully surrounds everything you own. The unplanned flow
is a red chord cutting across the mandala beneath the core. Watches
are bracket arcs — the broken one visibly snaps where cam-porch sits.
Full live view keeps the orbit as a still-breathing corner medallion.

**Scene 4 — the Reach view** (added 2026-08-23, from the owner's
"bubbles within bubbles" riff): click any host and its world arranges
around it. The membrane is the free-talk set — inside it, lane-mates
need no rule; every crossing needs one, judged per direction. Green
strands pass through the membrane (may talk; motes = talking now);
blocked directions die *at* the membrane with a stop bar, pulsing where
the host is actually knocking. One-way rules are visible, not implied:
tom-desktop's strand passes in to watch the camera while the camera's
strand back at him stops at its own membrane. Built for the two-second
check: "what can this thing talk to?" The view is direction-agnostic —
it drops into F (or any winner) as the host drill-in.

### I — Strata (`direction-i-strata.html`) — wild №2, time-first

> Developed 2026-08-23 after owner feedback (*"very interesting to look
> at but a bit hard to read — I wasn't sure what I was looking at"*).
> The legibility pass: traffic volume is a soft silhouette per row
> instead of tick-noise; red marks mean drops and nothing else; each
> row gets an index card (name, live rate, status); a NOW gutter right
> of the cursor prints each row's newest event in words; an attention
> strip and a how-to-read line let the paper explain itself.

The whole app is a chart recorder: boundaries are strata, time flows
toward an amber NOW cursor, and the newest ticks land at the leading
edge — which *is* the live view; any stratum (including ALL) expands
into the full table. Flags print at the minute they fired, on the
boundary where they fired; watch bands run along their strata and the
break at 13:52 is visible mid-paper. Zoomed out, ticks become texture
and the day reads at a glance — lookback and live are one gesture.

### J — Gauntlet (`direction-j-gauntlet.html`) — the true horizontal

Added 2026-08-23 after the owner's mid-round batch: Riverline was
"super weak" — water is a metaphor, not a truth. The strong horizontal
is the one RouterOS already is: **the x-axis is rule order**. Each rule
is a gate; strands thread the gauntlet and are absorbed by the gate
that judges them. The established gate visibly swallows the bulk; a
hot gate glows with its counter; a dormant rule is a gate untouched
for 30 days; and the unplanned flow runs all 41 gates untouched into
the default-drop wall — which is what "unplanned" means. Scene 3 is
the gates report: hot gates, dormant gates (with a print-the-command
retirement draft), watched gates, flags on the gates that fired them.

### K — Dispatch (`direction-k-dispatch.html`) — wild №3, the log as narrative

No chart on the front page at all: the interface writes what is
happening in sentences, in a controlled verb vocabulary (knocked,
refused, passed, went dark), and every sentence cites the rows that
make it true — nothing said that cannot be shown. Nouns are clickable
tokens; watches are standing sentences ("Nothing unsolicited has
passed in 41 days ✓") whose breaking IS the news; the dark boundary is
reported as the story's missing chapter. Scene 2 pins a sentence over
the full live view it compiles to; scene 3 tells the day in chapters.

### L — Lattice (`direction-l-lattice.html`) — wild №4, the reachability matrix

Sources down, destinations across: every cell is one directed
boundary. Policy is the cell's face — open with its ports, shut,
open-but-waiting (rule #6, 30 days unused), dark (hatched, unlogged) —
and reality pulses on top. The unplanned flow is a shut cell that is
knocking, burning red in a grid of quiet ones. One-way policy is
simply the matrix being asymmetric. This is also the answer to
"Strata has no topography": the lattice is the complete topography —
all N² conversations visible, including the ones nobody thought
about — and composes with Strata (lattice = every pair now; strata =
chosen pairs over time). Scene 3 shows Policy and Reality side by
side with the three-cell disagreement list: the audit, done at a
glance. Click a shut cell → compose the rule.

## Running batch (2026-08-23, owner, mid-round)

- Down to **Atlas II** and **Strata II** — though Atlas II reads
  "underwhelming"; what appeals are the strong concepts (Strata's time
  basis, Halo's circularity). **Halo dropped** as a top-level view; its
  Reach view survives in F scene 4, developed into **Reach & Compose**
  (point at host → destination → port → mikroview prints the RouterOS
  command; observed denials become drafted rules). **Riverline
  dropped** ("super weak"); **Gauntlet** replaced it — and was a no
  ("Gauntlet's a no"). Atlas II stays but reads "safe, boring, fine".
  Strata II "better, still very interesting" — its missing topography
  is answered by Lattice. K (Dispatch) and L (Lattice) added as the
  two requested wild concepts, orthogonal to everything prior and to
  each other: language and relation.

## Shared discipline

Palettes validated CVD-safe per surface (dataviz validator); no meaning
by colour alone; ratified specs (#438/#439/#413/#445) honoured;
`prefers-reduced-motion` respected; screenshots in `shots/`, regenerated
with `cd frontend && node ../docs/design/concepts/round-2/capture.mjs`.

## What happens next

Owner returns one batch across F/G/H/I (blend welcome — F's dock model,
I's time axis and H's glanceability compose). The chosen direction
drives the #482 ADR, proven against these scenes.
