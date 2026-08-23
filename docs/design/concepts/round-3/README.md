# Interface visioning — round 3 (final)

Continues rounds 1–2 (see `../round-1/README.md`, `../round-2/README.md`),
under #482/#483/#385 phase 2. Same format: self-contained HTML, no build
step, real motion, the same RouterOS-shaped data story across directions.

**This is the last mockup round**, at the owner's word. What it ratifies
(a direction or a blend) drives the #482 ADR.

## The owner's round-2 close (2026-08-23), which this round answers

- **Dispatch (K)**: a no as the driving UI; the text-only narrative
  concept is kept on the record as a surviving feature idea.
- **Lattice (L)**: a no. With it, the round-2 composite fell.
- **Strata survives.** The brief for this round, in the owner's words:
  1. *"Strata as a vertical, not a horizontal. It's not just a port
     though — the switch deserves its own fully thought-through
     concept."*
  2. *"Two wild takes on strata, same macro concept, wildly different
     executions."*

What all three keep — Strata's macro-concept, unchanged since round 2:
every boundary drawn against time, an amber NOW at the leading edge,
flags printed at their moment, watches visible where they hold and
where they break, DARK boundaries honestly distinct from quiet ones,
and lookback + live as one gesture. What varies is the execution:
sediment (discrete marks, vertical), notation (symbols, horizontal),
energy (a continuous field, vertical).

## The directions

### M — Core (`direction-m-core.html`) — the considered vertical

The switch to vertical is a change of substance, not axis, and the file
is built around the four things that become true only when time runs
downward:

1. **Scroll is the time machine.** The wheel is the web's native
   gesture and it now means "deeper into the past". The range tabs are
   *gone* — depth is the range, and the ground compresses below a
   visible seam (sediment compaction applied to time), so one surface
   holds the last quarter-hour in fine grain and the whole day below
   the fold. Scene 3 is the same surface scrolled 14 days deep.
2. **Words share a depth with marks.** Text runs horizontally, so it no
   longer competes with the time axis. Strata II could only afford
   words in a NOW gutter; here every knock in the alarm core carries
   its log line beside its mark.
3. **A moment is a horizon.** One instant cuts across all eight cores
   at the same depth, like an ash layer: at the 13:52 horizon the
   knocking starts AND the egress cadence breaks — correlation by
   geometry, not by comparing timestamps.
4. **The table is the core at full magnification.** The live table is
   already vertical time, newest at the top (#363). Scene 2 opens a
   core into the full live view with a depth ribbon joining each mark
   to its row: the chart and the table are one object.

### N — Score (`direction-n-score.html`) — wild №1, the log engraved

A healthy network is rhythm; you hear the wrong note before you could
ever read it. The mapping is load-bearing, not decorative: the DNS
metronome is an *ostinato*; the wan ramp is a printed *crescendo*
hairpin; the cadence watch breaking at 13:52 is a snapped tie marked
*sfz*; and the incident is a voice marked **tacet, 41 days** entering
**ff** at rehearsal mark E — the flag list and the rehearsal-marks
index are the same thing. Two honesty devices only a score has:
**rests are knowledge** (wg is watched-and-silent) while **guest has no
stave at all** (unscored — nothing writes that voice); and right of the
playhead **the page is unwritten**. X-notehead = drop (shape first, red
second), diamond = NAT, beams group bursts. Scene 2 pins two enlarged
bars above the live table, every note joined to its row.

### O — Waterfall (`direction-o-waterfall.html`) — wild №2, the receiver

Mikroview as an SDR — the design invariant made visible: a receiver
hears everything and transmits nothing ("observes, never connects" is
what a receiver *is*). The dial across the top is the topography: eight
bands, one per boundary, position within a band = port. The panadapter
is the live instant; below it time pours downward and steady talkers
draw carriers — the network's voiceprint, learned without trying. The
incident is what waterfalls exist to show: **a new carrier on a band
that has been black for 41 days**, dashes every 64 s. Tuning is the
drill-in: scene 2 locks the carrier (VFO A · IOT→LAN · TCP/445) and
demodulation *is* the live table, with #438's filters filled in by the
act of tuning. "No antenna" (guest) is drawn as categorically different
from a live-but-silent band (wg). SPAN replaces range tabs — a
receiver's own idiom.

## Shared discipline

- Same data story as rounds 1–2, kept consistent to the timestamp:
  cam-porch knocking tcp/445 every ~64 s from 13:52:07, 14 events by
  NOW 14:02:11; wan drops ▲3.1× since 13:30, all named by wan-in-drop,
  latest 185.220.101.34 → :8291; the day's flags 09:03 · 11:20 ×2 ·
  12:58 · 13:41 · 13:52; guest→wan dark; wg quiet by choice.
- Ratified specs honoured: #438's paired source/destination boxes with
  swap in every opened view; #363's newest-at-top (M and O are built on
  it); #445's honesty ethos (dark ≠ quiet, stated everywhere); red is
  only ever a drop; no meaning by colour alone.
- **Palette revalidated this round** (dataviz six checks, per surface):
  the lane set is lan `#3987e5` · srv `#199e70` · guest `#d76a9e` ·
  iot `#c98500` — **guest moved from round 1–2's orange `#d95926` to
  rose**, because orange↔amber failed CVD separation (ΔE 4.8 deutan)
  and the normal-vision floor (10.6 < 15). Passes on all three
  surfaces (`#0d0f12`, `#14151a`, `#08090c`) in physical adjacency
  order; green↔rose sits in the 6–8 floor band, legal with the name
  cards and column gaps every surface carries. Waterfall's two heat
  identities (accept `#5aa7f0` / drop `#e05252`) pass at ΔE 22.1
  deutan. Status colours (drop/ok/now/nat) are not a series palette
  and always ship with shape + label. Rounds 1–2 files keep their
  historical guest orange; they are a record, not shipping code.
- `prefers-reduced-motion` disables all animation in every file.
- Screenshots in `shots/`, regenerated with
  `cd frontend && node ../docs/design/concepts/round-3/capture.mjs`.

## The owner's batch (2026-08-23) — round 3 closes, and converges

- **M Core: dropped.** *"I like this graphically, but the waterfall just
  does this better."* The vertical's devices survive on the record
  (depth compression, horizons, words-beside-marks) but the fall is
  the better execution of newest-at-top time.
- **N Score: dropped.** *"Cute, but it's just confusing to read and
  you've got to be a musician to appreciate it."*
- **O Waterfall: the fall is ratified as the hero.** *"You might have
  smashed it here with 'the fall'."* The other O scenes (tuned, day
  fall) were not kept.
- **Convergence brief** → `../round-4/`: Atlas II — now called plain
  **Atlas** — keeps its topography and live view but the fall becomes
  the landing page. Two style identities on the same bones: a water
  theme (networks flow) and a space theme (the starfall). The live
  table's columns get properly aligned, and Reach folds into the
  topography with a recentring interaction.

## What happens next (superseded — see above)

Owner returns one batch across M/N/O — kill / keep / blend. Because all
three share the macro-concept, blends are cheap: M's depth-compression
and horizon devices, N's rests-vs-unscored honesty and rehearsal-mark
flag list, and O's tune-to-filter gesture are each portable into
whichever execution wins. The ratified direction closes #483's shaping
question and the #482 ADR is written from it, proven against these
scenes, with the bundle-budget reset (92,000 → 200,000 bytes gzipped)
in the same PR.
