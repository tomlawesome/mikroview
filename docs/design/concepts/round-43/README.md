# Round 43 — what outlives a restart

Issue: #921. Round 42's `disk.html` is the ratified whole; `build.py`
copies it, changes two rows and adds one state. Nothing else moves.

## The drawing

The memory group's `persistence` row carried two facts about two
different things: which store keeps flags, definitions, watchlist
entries, entities and tokens, and what happens to the event buffer on a
restart. With the disk group one storey down, the second fact has a
counterpart (`27 days · since 7 Aug`) that the old sentence, "memory-only
and clears on restart", reads against. The build (#677) worded that row
plainly because no event store existed; now one does.

The row is split by subject:

- **memory · `on restart`** — about the buffer alone. The buffer always
  clears: nothing refills the ring from disk, and the live scenes start
  empty after a restart whatever the disk holds. What *outlives* it
  depends on the disk group's state, so the row reads per state:
  - history on: `the buffer clears — the 27 days on disk stay; trying a
    watcher reads them`. The reader is named because it is the only one:
    a watcher's try (its receipt) reads disk then memory; the Fall and
    the stream read the buffer only.
  - off, with a key: `the buffer clears — nothing outlives it; days can be
    kept on disk below`. No second `turn on` link: the disk group's own
    is one storey down.
  - no key: `the buffer clears — nothing outlives it`. The disk group
    already says why.
  - unanswered: `the buffer clears`, dim, and nothing more claimed.
- **disk · `state`** — the state store moves here, beside `key`: `file
  store · /var/lib/mikroview — flags, definitions, watchlist, entities,
  tokens` (`Postgres — …` when that is the backend). It is the other
  thing mikroview keeps on disk, and like `key` it is an admin-only
  fact, so both rows are absent for a viewer together. #853 (state under
  the retention key) may add a word here when it lands; the row does not
  claim either way now.
- **disk · unanswered** (`dfail`, round 42's gap 9): when the settings GET
  fails — an older server, or an error — the group stays rather than
  vanishing, with no controls: `on disk · unknown — the server did not
  answer · ask again`. The `dnokey` idiom, one row instead of two.

Data story: round 42's own — today is 2 Sep, 27 days on disk since
7 Aug, a file store under `/var/lib/mikroview`.

## Round 42's other gaps, for a verdict without a drawing

PR #920's gaps 6 and 8 are sentences in slots round 42 already drew (the
proposal line under the track, the row on the right), so they are listed
here as built rather than redrawn:

- grow within the cap: `60 days would need ~1.8 GiB at today's rate,
  within the 2 GiB cap — apply · keep 30 days`
- shrink deleting nothing (fewer days allowed than held, or a cap above
  what is held): `28 days holds ~840 MiB at today's rate — nothing on
  disk lets go — apply · keep 30 days`
- raising the cap: `2 GiB holds ~30 days at today's rate — nothing on
  disk lets go — apply · keep 1 GiB`
- no rate yet, more days: `90 days, under the 1 GiB cap; no rate yet to
  say how much of it that needs`
- no rate yet, a cap below what is held: `512 MiB is less than the
  812 MiB on disk — the oldest days let go until it fits`
- after `turn on` (gap 8): the row reads `nothing` until the next 60 s
  refresh. Not a wording question: the build should ask again as soon as
  the PUT answers.

## Scenes (`restart.html`)

States are section classes on `#set`, round 42's own plus `dfail`;
`restart.html?d=<state>#set` applies one.

| Scene | URL | What it shows |
|---|---|---|
| History on | `restart.html#set` | the two groups agreeing: buffer clears, 27 days stay |
| Off, keyed | `?d=dstopped#set` | nothing outlives it; days can be kept below |
| No key | `?d=dnokey#set` | nothing outlives it; the disk group says why |
| Unanswered | `?d=dfail#set` | the disk group present but unknown, `ask again` |

Screenshots: `shots/settings.png`, `pair-{on,stopped,nokey,fail}.png`
(memory and disk together). Looked at each; no collisions.

## Verdicts (owner, 2026-09-04)

Scenes 3–6 and the listed sentences: *"All look fine"*. **Ratified as
drawn**, the gap-6 sentences as built, gap 8 as a build fix.

Build: #921, on the same branch. Port the two rows and the `dfail` state
from `restart.html`.

## Built (2026-09-04)

`restartRow` and `stateRow` in `frontend/src/lib/history.ts`; the rows in
`EngineRoom.svelte`, the `state` row through `DiskControl`'s `stateStore`
prop. Against a live instance (`scripts/live-env.sh up`): `shots/built-on.png`
and `built-stopped.png` match `pair-on` and `pair-stopped`, with the
instance's own figures (1 day, a `/tmp` data dir). Gap 8: after `turn on`
the row read a held window again in under a second — the PUT's own answer
carries it — and the build asks once more 6 s later for the writer's flush.

Built differently, or not drawn:

- History on with nothing filed yet (`held.days` 0) reads `the buffer
  clears — what is on disk stays; trying a watcher reads it`. Undrawn;
  the on-state sentence with no figure to name.
- `dfail` also covers the GET failing on a server without a history
  control (503). A viewer's 403 is not a failure: no group, and the
  memory row reads the bare `the buffer clears`, dim, as drawn for
  unanswered.
