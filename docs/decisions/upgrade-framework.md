# Upgrades: any release to current, staged by migration, proven by recordings

Date: 2026-09-15. Dialogue #1206; owner's brief 2026-09-13: "a framework
for upgrades that allows a user to go from any version -> current, even
if that means multi-stage upgrading." The operator-facing contract is
`docs/upgrades.md`; this records why it says what it says.

## The problem

The owner's own install, set up on an early build and carried through
v0.3.0, v0.4.0 and v0.5.1, had been silently misconfigured for months
(#1205, #1203, #1196). Nothing in the product or its tests noticed,
because nothing tests an upgrade at all: migrations are exercised only
against a schema the same build created, and no job ever starts the
current build on state an older one wrote.

Three things fall behind at an upgrade, and they are different problems:

1. **On-disk state** — documents and tables an older build wrote.
2. **Router-side setup** — the script the wizard pasted, which the
   router keeps as it was.
3. **The operator** — who was never told there was anything to do.

## What the code already settled

- The Postgres backend has numbered SQL migrations, kept forever with
  checksums, each in its own transaction, all pending ones run in order
  at start (`internal/persist/postgres.go`). That is already "any
  version to current, multi-stage": the stages are migrations, and no
  intermediate build ever needs to run.
- The file backend has no migrations. Old documents load because new
  fields default to zero (`persist.Open` and each store's decode). That
  works until a field changes meaning, and nothing tests it.
- `<data>/version` records the last build version and `main.go` already
  compares it at boot — to write a log line.
- Every release since v0.1.0 has a version-tagged image on GHCR
  (`.github/workflows/docker.yml`). The issue body's "no job tags an
  image by version" was true only of GitLab's `gate:image`.

## Decisions

**Contract: every released version to current, in one start.** No
floor that moves: the tested set is every release, and the cost of that
is bounded by how the proof works (below), not by how many releases
there are. An install older than the first release reads as schema 0
and takes the same path.

**Staged by migration, not by version.** One numbered list for both
backends; the file backend gets what Postgres has. A migration that
reads an old shape carries a frozen copy of that shape, so no migration
ever depends on a decoder the current store might drop. This is the
existing Postgres model extended, not a new one.

**Downgrade refuses, and there is no path back.** Data newer than the
build is never written to: the alternative — starting anyway with a
warning — would have the older build rewrite documents in its own
shape over fields it does not know, which is exactly the silent damage
this issue exists to stop. A pre-upgrade copy taken by the app was
proposed and dropped (owner, 2026-09-15): a migration that lands is the
new build's data, and the only reason to want the old build back is a
defect in the new one, whose fix is a newer build. Operators who want
the option take their own copy first; the doc says so.

**Atomic per migration, resumable.** New document written beside the
old, swapped in only when complete; stamp after each migration lands.
A failed or interrupted migration leaves the old document as it was —
which is also why no copy is needed for that case — and the next start
resumes. It never repairs, because there is nothing half-written to
repair.

**Proof is recordings, not booted images.** For each release, a data
directory that release actually wrote after a scripted session, with
obviously fake users and tokens, opened by the current build in an
ordinary Go test. Seconds
per fixture, on every merge request, no new gate time. An old image
runs exactly once — when its recording is made, by a tag-triggered job
(backfilled by hand for v0.1.0–v0.5.1). Postgres is proven per schema
version rather than per release, because its migrations are already
numbered and checksummed. Recordings live in the project's package
registry, not the repository: a data directory holds password and
token hashes and TLS keys, made-up or not, and gitleaks would flag
every one (owner, 2026-09-15). CI fetches them before the test; the
test skips locally when none are present.

**Router-side drift joins the framework** (owner, 2026-09-14) **and
needs the router to say what it has.** The push script sends nothing
about `/system logging` and carries no stamp. It gains a page with the
mikroview logging action and rules plus the wizard script version;
the server compares against what the current wizard would push. The
pasted script is itself the upgrade path for this leg: an old router
never sends the page, and that absence is the first signal. What is
sent is exactly the `mikroview` logging action and the rules feeding
it, plus the script version — nothing else from the router's logging.
Its own issue, in M14 with the rest (owner, 2026-09-15).

**The operator is told.** A notice after an upgrade, driven by the
version comparison `main.go` already does: from which version, and
what to paste again on each router. Acknowledged per instance, not per
browser; clears itself per router once routers report their setup.

## Superseded

- *Boot a matrix of released images in the gate.* Right idea, wrong
  cost: minutes per image on every merge request, growing with each
  release, and it proves the same thing a recording proves.
- *A support floor that moves forward.* Not needed once the proof is
  cheap, and "any version" is what the owner asked for.
- *Repair on downgrade or interruption.* Nothing tests writing older
  shapes; refusing and resuming are both simpler and both safe.
- *A pre-upgrade copy under `<data>/upgrade-backup/`.* Served only a
  downgrade, which is not offered; a failed migration already leaves
  the old document in place. Dropped before it was built.
- *Fixtures committed under `testdata/upgrade/`.* Hashes and keys trip
  the secrets scanner whether or not they are real.
- *MikroView writing the operator's `config.yaml` itself, in any form.*
  Ruled out in full in `config-yaml-ownership.md`; the paste block in
  Settings ▸ Upgrade stays the permanent answer.

## Implementation

- #1238 (persist: schema for the file backend, downgrade guard,
  resumable migrations) — M16
- #1239 (recorded fixtures per release, opened in CI) — M16
- #1240 (upgrade notice in the interface) — M14
- #1241 (router pushes its logging setup and script version) — M14
