# Round 41 — what a surface says after a restart

Issue: #795 (warm restart). Round 39's `the-whole.html` is the ratified
whole; `build.py` copies it and adds one thing to two scenes -- the
statement a surface makes while its counters were restored from a
snapshot -- and clones the metrics scene twice for comparison.

Data story: mikroview restarted at 13:18. The newest snapshot had been
taken at 13:14, so the hour reads restored to 13:14, four blank minutes
while the process was down, live from 13:18.

## Scenes (`warm-restart.html`)

| Scene | Anchor | What it shows |
|---|---|---|
| Metrics, restored | `#s4` | `restored to 13:14 · live since 13:18` as the last fact on the hourline's right-hand group; the four down minutes blank on the seismograph. **Recommended.** |
| Metrics, cold start | `#s4-cold` | `counting since 13:18 — nothing before`; the axis empty left of 13:18. |
| Metrics, relative wording | `#s4-rel` | `counters from a snapshot 4 m old · live from 13:18`, the issue's own phrasing, for comparison. |
| Docket, restored | `#s7` | The same statement as a dim chip in the clear-all row -- the fall's "statement, not a control" idiom (round 36 item 6.1). |

Clearing rule proposed: the statement stays until the surface is fully
live, 60 minutes after boot, and both surfaces use the same clock.

Screenshots: `shots/metrics-restored.png`, `metrics-cold.png`,
`metrics-restored-relative.png`, `docket-restored.png`.

## Verdicts (owner, 2026-09-03)

1. Metrics, restored -- "yeah great". **Ratified.**
2. Metrics, cold start -- "great". **Ratified.**
3. Relative wording -- "as you say, go with 1". **Dropped**; absolute times.
4. Docket chip -- "great". **Ratified.**

Build: the hourline's last fact and the docket's clear-row chip say
`restored to HH:MM · live since HH:MM` after a warm restart and
`counting since HH:MM — nothing before` after a cold one, and clear 60
minutes after boot on both surfaces.
