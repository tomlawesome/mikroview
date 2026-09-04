# Round 42 — the disk group: the on-disk history's switch

Issue: #910. Round 39's `the-whole.html` is the ratified whole; `build.py`
copies it and adds one group to settings, `disk`, directly under
`memory`. Nothing else changes.

## The drawing

The memory group already reads as two things: a bar for what is **held**
(the hours) and a track for what is **allowed** (the slider). The disk
group is the same two things one storey down:

- **A bar of the days held on disk**, one cell a day, darker held more,
  the oldest day labelled at the left, `today` at the right. This is the
  window actually held, never the setting: `27 days · since 7 Aug ·
  812 MiB — filling` in the row on the right, and `— full` when the cap
  is what decides.
- **A track for the days allowed**, 1 d to 365 d on a doubling scale,
  same handle, same ghost, same `apply · keep`. A dashed mark on it says
  where the byte cap runs out at today's rate, so the two numbers explain
  each other: a handle right of the mark means the cap decides.
- **The byte cap is a figure** in the `allowed` row; clicking it opens a
  field in place. Two numbers do not need two sliders.
- **Off is a link**, `turn off`, in the same row.
- **Every change that would delete something is a proposal** until a
  link that names the deletion is taken — `delete 13 days · keep all
  27`, `delete 27 days · keep them` — the slider's own shrink idiom
  (round 39). The days that would go dim on the bar with the new oldest
  day marked, as the hours do. There is no modal; the confirmation is
  the sentence, and the link says what it does.
- **No key mounted**: no control at all. The group is two statements,
  `on disk · nothing` and `key · none mounted — nothing is kept on disk
  without one · how to mount one` — the ingest group's `plain syslog ·
  off — loopback only` idiom. An operator still finds the feature; there
  is no dead switch.
- **Off, with a key**: the bar is absent (nothing is held), the track
  stays live so days can be set first, and the row reads `30 days · at
  most 1 GiB · turn on`. Turning on deletes nothing, so it is not a
  proposal. It takes what memory already holds and every day after
  (owner, 2026-09-03), so the first day on disk is not a short one.

Data story (the whole's own): today is 2 Sep. History was turned on
7 Aug: 27 days on disk, 812 MiB, about 30 MiB a day compressed. 30 days
allowed under 1 GiB, so the cap would bite at ~34 days. The capped scene
lowers the cap to 768 MiB: 25 days held, the cap deciding.

Not drawn: a viewer's view (settings is an admin surface in this
drawing) and the setup wizard, which does not carry this group.

## Scenes (`disk.html`)

States are section classes on `#set`, as the memory group's are;
`disk.html?d=<state>#set` applies one.

| Scene | URL | What it shows |
|---|---|---|
| Settings | `disk.html#set` | the whole screen, memory and disk together |
| At rest | `disk.html#set` | 27 days held, 30 allowed, the cap mark at ~34 d |
| Fewer days | `?d=dshrink` | 14 d proposed: 13 days dim, `delete 13 days · keep all 27` |
| More days | `?d=dgrow` | 90 d proposed: the sentence says the cap would hold ~34 of them |
| Lower cap | `?d=dcap` | 512 MiB in the field: `delete 10 days · keep 1 GiB`; the cap mark moves to ~17 d |
| Turning off | `?d=doff` | all 27 days dim: `delete 27 days · keep them` |
| The cap deciding | `?d=dcapped` | 768 MiB cap, 25 of 30 days held, `— full` |
| Off, keyed | `?d=dstopped` | nothing held, track live, `turn on` |
| No key | `?d=dnokey` | two statements, no control |

Screenshots: `shots/settings.png`, `settings-disk.png`,
`settings-disk-{shrink,grow,cap,off,capped,stopped,nokey}.png`. Looked at
each; no collisions.

## Verdicts (owner, 2026-09-03)

All eight scenes: *"1-8 all look great"*. **Ratified as drawn.**

On what turning on takes: *"what memory already holds + new"* — the
ring's contents are written to disk at turn-on, then every event after.

Build: `#910`. Port the group's markup and CSS from `disk.html`; the
states are the section classes listed above.
