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

- Anything replay-shaped keeps building against `Corpus`, not `MemoryCorpus`.

## Amendment, 2026-09-05: the same key now covers the state store and warm restart too (#853)

**Status:** Decided (owner, 2026-09-03; built 2026-09-05).

The consequence recorded above -- "the state store is still plaintext on
disk ... a separate question for M8" -- is settled. The owner's decision,
recorded verbatim on #853 and not reopened here:

1. **One key, not two.** The state store uses the same key mechanism as
   event retention (`history.keyFile` / `MIKROVIEW_HISTORY_KEY_FILE`).
   There is no second key file to generate, mount or lose track of.
2. **No key, no storage.** Without a key, the file-backed state store
   refuses to persist. No plaintext fallback: nothing mikroview writes to
   disk is ever in the clear. This is a **severe change from every earlier
   release** for a default install: before this amendment, none of the
   state store needed a key at all, and every mikroview release before it
   persisted accounts, flags, entities and the rest to plain JSON with no
   configuration. From this build, a deployment with no `history.keyFile`
   mounted keeps all of that in memory only -- including accounts and API
   tokens, which are not called out in the "accepted cost" list below
   because that list is illustrative, not exhaustive; the rule is "every
   file the file backend writes," with no exception carved out for any
   one store.
3. **No migration path from plaintext files to encrypted, pre-1.0.** If a
   key is removed after being used, storage stops; the files it left
   behind are not decrypted back, and a document that fails to decrypt is
   refused the same way a corrupt document is (`persist.Open`, #378) --
   not silently treated as absent.
4. **Postgres is unaffected.** Encrypting a Postgres-backed store was
   considered and rejected for this build: Postgres already has its own
   at-rest custody, and mikroview already requires `sslmode=verify-full`
   for the connection (see `docs/decisions/postgres-backend.md`), so a
   second encryption layer on top would protect against a narrower threat
   (the database's own storage being read directly) that the operator's
   choice of Postgres, and its own hardening, is already responsible for.
   Revisitable if that stops being a safe assumption for some deployment
   shape.
5. **Warm-restart snapshots (#795) are covered too**, under the same key,
   with the same "no key, no warm restart" rule: with none configured the
   process starts cold, exactly as it did before #795 existed.

### What actually changed

- `internal/retention`'s `Key` type gained an exported `Derive` (the HKDF
  step, previously private) and `Seal`/`Open` (a whole-document AES-256-GCM
  envelope, alongside the existing per-day event-file framing) so other
  packages holding the same key reuse this package's cipher and key
  derivation rather than each inventing their own. Nothing about the event
  file format itself changed.
- `internal/persist` gained `EncryptedFileBackend`, a `Backend` that wraps
  `FileBackend` and seals/opens every document under a `retention.Key`. It
  passes the same shared contract test (`internal/persist/contract_test.go`)
  every other backend does.
- `storage.go`'s `backendFor` -- the one place that decides where every
  JSON-file store lives -- now returns that encrypted backend when
  `history.keyFile` is configured, and `(nil, nil)` ("persistence not
  configured", the same signal an empty `storePath` already produced) when
  it is not. `openAuthStoreForCLI`/`openRecoveryStoreForCLI` (the
  `-recover-admin-account`/`-transfer-admin` family) refuse loudly on that
  `nil` rather than silently handing a recovery command an empty in-memory
  store.
- `internal/snapshot`'s `Writer`/`Load` take a small `Sealer` interface
  (matching `retention.Key`'s `Seal`/`Open` signatures) rather than the
  concrete type, because `internal/retention` imports `internal/store`,
  which imports `internal/snapshot` for the `Part` interface -- a direct
  import the other way would be a cycle.
- `-backup`/`-restore` read and write through their own key-aware backend
  choice (`backupBackendFor` in `backup_cli.go`), deliberately not
  `storage.go`'s `backendFor`: a backup's job is fidelity with whatever is
  actually on disk (a file predating this amendment, or one from a
  deployment that has since removed its key), which is a different
  question from whether the *live server* persists a store at all with no
  key configured. With a key, the backup envelope stays plain, readable
  JSON -- the tools run with the key available, so there is no reason for
  a backup file to itself be undecipherable without that same key mounted
  wherever it is opened.

### Not settled by this amendment

Whether accounts, tokens and recovery-key digests specifically should have
kept an unencrypted fallback when no key is configured -- given how central
login is to using the product at all -- was considered during the build and
decided against in favour of the literal "every file the file backend
writes" rule already quoted above, but the owner had not explicitly
ratified that specific consequence (as opposed to the general rule).
Resolved the same day; see the addendum below.

## Addendum, 2026-09-05: rule 6 -- accounts, tokens and recovery keys are exempt (#853)

**Status:** Decided (owner, 2026-09-05).

Rule 2 above ("no key, no storage ... no exception carved out for any one
store") is amended. The question left open in "Not settled" was put to the
owner and answered the other way: three stores keep persisting to a plain
JSON file with no key configured, exactly as every mikroview release
before this issue --

- `auth` (accounts, `cfg.Auth.StorePath`)
- `tokens` (`cfg.Auth.TokensStorePath`)
- `recovery_keys` (`cfg.Auth.RecoveryKeysPath`)

The reasoning: all three hold only one-way hashes -- Argon2id password
hashes, SHA-256 token hashes, hashed recovery keys -- so encrypting them
protects nothing that a plaintext copy would actually expose. What a
plaintext copy discloses instead (usernames and roles for accounts, token
names for tokens) is accepted, the same trade every earlier mikroview
release made by default. With a key configured, all three are still
encrypted like every other store -- this only changes what happens with no
key.

Every other store keeps rule 2 unchanged: flags, entities, the MAC
registry, rule usage, detector settings, watchlist suggestions and
definitions remain memory-only with no key, no exceptions.

`storage.go`'s `backendFor` implements the exemption with a small
`hashedStores` set checked before the `s.key == nil` return.
`openAuthStoreForCLI`/`openRecoveryStoreForCLI` (main.go) keep their nil
check -- an empty configured path still yields `nil` -- but no longer word
the refusal as needing a key, since that can no longer be the reason.
