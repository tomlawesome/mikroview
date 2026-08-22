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

### H — Orbit (`direction-h-orbit.html`) — wild №1, radial

The router is the core; the lanes orbit it; the internet is the outer
ring that truthfully surrounds everything you own. The unplanned flow
is a red chord cutting across the mandala beneath the core. Watches
are bracket arcs — the broken one visibly snaps where cam-porch sits.
Full live view keeps the orbit as a still-breathing corner medallion.

### I — Strata (`direction-i-strata.html`) — wild №2, time-first

The whole app is a chart recorder: boundaries are strata, time flows
toward an amber NOW cursor, and the newest ticks land at the leading
edge — which *is* the live view; any stratum (including ALL) expands
into the full table. Flags print at the minute they fired, on the
boundary where they fired; watch bands run along their strata and the
break at 13:52 is visible mid-paper. Zoomed out, ticks become texture
and the day reads at a glance — lookback and live are one gesture.

## Shared discipline

Palettes validated CVD-safe per surface (dataviz validator); no meaning
by colour alone; ratified specs (#438/#439/#413/#445) honoured;
`prefers-reduced-motion` respected; screenshots in `shots/`, regenerated
with `cd frontend && node ../docs/design/concepts/round-2/capture.mjs`.

## What happens next

Owner returns one batch across F/G/H/I (blend welcome — F's dock model,
I's time axis and H's glanceability compose). The chosen direction
drives the #482 ADR, proven against these scenes.
