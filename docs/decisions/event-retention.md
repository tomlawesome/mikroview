# Event retention on disk: encrypted daily files, off until a key is supplied

**Status:** Decided (owner and Fable 5, 2026-09-02). Build tracked on #798.

## Context

The replay corpus is the in-memory ring only (`internal/engine/corpus.go`,
`MemoryCorpus`). Its size is the operator's memory slider (#796), bounded by
what the host can spare — so past that ceiling, memory cannot hold more, and
Try's receipts (#786) say "in the last 4h" when a fortnight would be the
honest basis for loosening a threshold. `evaluation-engine.md` open question
2 named longer retention an anticipated decision to be taken with sizing
numbers; this is that decision.

## Sizing

At the recommended 12–15 events/s, 15 × 86 400 ≈ 1.3 M events a day. The
ring's 626 bytes per event is the *in-memory* cost (Go structs, string
headers); packed on disk an event is ~60–80 bytes, and syslog-shaped rows
compress 5–10×. So:

- packed: ~100 MB/day; packed and compressed: ~20 MB/day.
- 30 days compressed: ~600 MB. A 1 GiB cap is only reached at an untuned
  rate (the six-minute-ring case, ~560 events/s), which is why a byte cap
  exists alongside the day count.

## Decision

1. **One encrypted, compressed file per day** in the data directory, holding
   the same events the ring holds, appended as they arrive. Not Postgres:
   Postgres is optional here (`postgres-backend.md`), so a Postgres-only
   history would leave the default install with none; replay is a
   front-to-back scan that files serve as well as anything; and a Postgres
   table is no more protected at rest than a file.
2. **Encrypted at rest, key held outside the data directory.** The key comes
   from a secret file the operator mounts (never in the data directory, never
   in config, never in an environment variable — AGENTS.md's secret rule).
   Copying the data directory or a backup yields nothing readable. Root on
   the running host can still read what the process can; only not retaining
   at all avoids that.
3. **Memory-only stays a first-class mode, and is the default.** No key
   file means no disk retention: the corpus is the ring, exactly as today.
   With a key present, disk retention is still a switch the operator turns
   on, beside the memory slider, and can turn off again — turning it off
   deletes the retained files. An operator who wants nothing on disk is
   choosing that, not missing a step.
4. **Two operator settings**: days to keep (default 30) and a byte cap
   (default 1 GiB). Oldest day dropped first when either is hit.
5. **Replay reads ring plus files** through the existing `Corpus` interface
   (single construction site, `corpus.go` `NewMemoryCorpus`). A receipt
   states the window actually held, never the setting — the standing rule
   that an absence of ours is never reported as a fact about the network.
6. Retained files are excluded from any backup mikroview itself produces
   (#394) unless a later decision says otherwise.

## Superseded

- Plain files relying on disk encryption: rejected by the owner — thirty days
  of who-talked-to-whom in plaintext is exactly what an attacker on the box
  wants.
- Postgres as the retained store: not more secure at rest, and not always
  present. A "store history in Postgres when configured" option can be filed
  later behind the same interface.
- The in-memory 626 B/event figure as the disk estimate: it overstated the
  need by roughly ten times.

## Consequences

- The state store (flags, entities, watchlist) is still plaintext on disk;
  this ADR does not change that. Whether the same key mechanism should cover
  it is a separate question for M8 (#853).
- Anything replay-shaped keeps building against `Corpus`, not `MemoryCorpus`.
