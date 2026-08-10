# Postgres backend: the six decisions #131 left open

Date: 2026-08-07. Resolves the "Open questions for the implementation
pass" section of issue #131 before any code was written.

Recap of the motivation, because it constrains every answer below: the
point is **separation between the mikroview host and its persisted
state**, so that compromising the host doesn't hand over the accounts
file. It is not query power, not reporting, not scale. mikroview's
persisted stores are small — hundreds of records, not millions.

## 1. Schema: one blob table, not six relational ones

```sql
CREATE TABLE store_blob (
    name       text        PRIMARY KEY,   -- 'auth', 'flags', 'entities', ...
    payload    text        NOT NULL,      -- exactly the bytes the file backend writes
    version    bigint      NOT NULL,      -- optimistic-concurrency token
    updated_at timestamptz NOT NULL
);
```

One row per store, holding the same JSON document the file backend
writes today.

**Why not relational tables per store.** Every store already keeps its
whole dataset in memory and serves reads from there; none of them issues
a query. Normalising would mean rewriting six stores' persistence *and*
their read paths, to gain a query capability nothing asks for — while
touching `internal/auth`, the most security-critical code in the
project, most heavily. The risk is concentrated exactly where the cost
of a bug is highest.

**Why `text` and not `jsonb`.** `jsonb` normalises: it reorders keys,
collapses duplicates, and rewrites number formats. `text` round-trips
the bytes unchanged, so one marshal/unmarshal path serves both backends
and a JSON→Postgres migration is byte-identical rather than
merely-equivalent. An operator can still read it in `psql`, which was
the only real argument for `jsonb`.

**What this gives up**, stated plainly: no SQL querying of mikroview
state, and every write rewrites a whole document. Both are acceptable at
these data sizes; neither is on the path of the motivation above.

## 1a. The match log gets its own table -- this does not reopen §1

Added 2026-08-10, once #243's match log (issue #243, `internal/matchlog`)
raised a genuine range-query need this decision hadn't accounted for.

§1's reasoning was scoped to the stores that existed when it was
written: auth, tokens, flags, entities, audit, watchlist entries -- all
small, bounded, already fully loaded into memory, none of them issuing a
query. The match log was never a candidate for that set. It doesn't hold
its data in memory at all (flat memory regardless of retention is the
point), and querying it by source and time range is the entire reason it
exists. It was already exempt from the *file* backend's equivalent
pattern for the same reason -- `internal/matchlog` doesn't implement
`persist.Backend`; it's its own package with its own append-only format,
because a whole-document rewrite per match doesn't fit an append-heavy
log. A dedicated Postgres table continues that same exemption onto the
second backend. It is not a rewrite of anything §1 actually covers --
`store_blob` and everything on it (auth included) stays exactly as
decided.

So: mix shapes. `store_blob` for the six bounded stores above (and
anything future that looks like them), a purpose-built, indexed table
for anything that structurally isn't one of those -- decided per store,
not as a blanket rule either way. Migrated through the same embedded-SQL
runner §3 already commits to, reviewed under the same injection-sink
process §7 already commits to. Nothing about those decisions changes.

## 2. Concurrency: compare-and-swap, which is better than today

`version` is not decoration. The current file backend has a real
lost-update window: the server holds the store in memory, an operator
runs a CLI command that rewrites the file, and the server's next persist
silently clobbers it. `reloadIfStale` narrows that with an mtime check
but cannot close it.

Every write is `UPDATE ... WHERE name = $1 AND version = $2`, and a zero
row count means someone else wrote first — the caller reloads and
retries rather than overwriting. That is a genuine correctness
improvement over the file backend, not parity with it.

## 3. Migration tooling: embedded SQL, applied in-house

No `golang-migrate`. This codebase writes its own leveled logging, its
own AEAD state codec, and its own bucketed windows rather than taking
dependencies for them — a schema runner is roughly sixty lines over a
`schema_version` table, and a migration framework is a large dependency
surface for that. Migrations are `.sql` files under `internal/persist/
migrations/`, embedded with `go:embed`, applied in filename order inside
a transaction, recorded by version.

## 4. Driver: `pgx/v5`, stdlib `database/sql` interface not used

`jackc/pgx/v5` directly, not through `database/sql`. It's the standard
choice for Go+Postgres, and going through `database/sql` would add an
abstraction whose portability we don't want (there is no second SQL
backend planned, and pretending otherwise invites SQL that works
everywhere and is optimal nowhere).

**Every statement is a compile-time constant with bound parameters.**
There is no string concatenation anywhere in this package, and the
injection guard added in `injection_sinks_test.go` lists `database/sql`
precisely so this arrival forces the question. See §7.

## 5. Migration direction: one-way, and the JSON files are left alone

JSON → Postgres, automatically, on the first boot with Postgres
configured. No reverse migration.

The JSON files are **not** deleted, and mikroview says so in the log.
Reverting is therefore "remove the Postgres config and restart", which
comes back up on the last file state — stale, but present and readable.
Deleting the files would make the decision irreversible on the strength
of a config change, which is the wrong shape for something an operator
might be trying out.

Migration only runs into an *empty* store row. A store that already has
a row in Postgres is never overwritten from a file, so a stale JSON file
left on disk can't roll back live data on a later restart.

## 6. Testing: a real Postgres, not a mock

Integration tests run against an actual server, skipped (not failed)
when `MIKROVIEW_TEST_POSTGRES` is unset so `go test ./...` still works
on a machine without one. CI runs them against a Postgres started with
TLS via docker -- not a `services:` block, which cannot provision the
certificate the code requires.

A mock would test that the code calls the functions it calls. The things
worth testing here — that the CAS actually rejects a stale write, that
the migration is byte-identical, that a partially applied migration
rolls back — are properties of the database, and only a database can
demonstrate them.

## 6a. No Postgres service in docker-compose.yml

Issue #131's open questions assumed one ("Postgres as an optional
additional service in the compose file, following this project's
existing comprehensive, commented-out-if-optional convention"). That is
now explicitly rejected.

A compose file that brings up Postgres next to mikroview puts the
database on the same host, and a same-host database delivers **none** of
the separation this feature exists for. The connection credential sits
in the same compose file, on the same disk, inside the same blast
radius as the accounts file it was meant to protect. It is strictly
worse than the JSON file it replaces: same exposure, more moving parts.

The deeper problem is that shipping one would make the documentation
lie. Every page here says "off-box, network-restricted, or this buys you
nothing" — and then the copy-pasteable example would hand the operator
the exact deployment that buys them nothing. People copy the example.
The example has to be the thing we actually recommend.

So the compose file gains no Postgres service, commented out or
otherwise. What it gains is the `postgres.dsnFile` setting pointing at
an external server, and a comment saying plainly why there is nothing
here to uncomment.

Checked when this was decided: no Postgres service, image or credential
existed anywhere under `deploy/`, in the README, or in
`docs/configuration.md`, so there was nothing to remove — only
something not to add.

## 6b. Migration integrity: what checksums are and aren't for

Raised as a review question: if someone can edit the migration script
and trigger it, they run arbitrary SQL. Should the scripts be
checksummed, with the app verifying itself against GitHub?

**The threat doesn't exist in this shape.** The migrations are
`go:embed`-ed -- compiled into the binary, not read from disk. Verified
by copying the binary into an otherwise empty directory, pointing it at
a database, and confirming the schema applied with no `migrations/`
anywhere. To change the SQL you must change the binary, and at that
point you can change anything: a checksum stored in that binary is
checked by code in that same binary, so the attacker edits both. **A
program cannot verify its own integrity.**

The supply-chain version -- someone edits the `.sql` in the repository
and it gets built -- is real, but checksums add nothing there either.
Anyone who can commit a migration change can commit a hash change on the
same commit, and could more easily edit a `.go` file. That risk is
answered by branch protection and review, not by a value stored beside
the thing it describes.

Self-verification against GitHub at runtime was rejected outright:
tampered code skips the check, it makes an external service a startup
dependency and a new trust anchor, and it leaks deployment activity to a
third party.

**What was done instead**, splitting the question into the two real
problems underneath it:

1. *Verifying what you are about to run* is a signing problem, and the
   trust anchor has to sit outside the artefact. Release images are now
   signed with keyless cosign and carry SLSA provenance plus an SBOM,
   bound to the digest rather than a tag. See
   `.github/workflows/docker.yml`.

2. *Migrations changing when they shouldn't* is a correctness problem
   worth a checksum -- for a completely different reason than the one
   asked about. Editing an already-applied migration means deployed
   databases never re-run it while fresh installs get the new text, and
   the schemas silently diverge. `TestAppliedMigrationsAreImmutable`
   pins each released migration's hash so that edit fails in review. Its
   doc comment states plainly that it is not tamper protection, so
   nobody later mistakes it for one.

## 7. Security review of the new surface

Required by `docs/decisions/injection-audit.md`, which named
`database/sql` as the first SQL sink and committed to re-auditing before
it shipped.

- **No SQL injection surface.** Every statement is a `const` with `$n`
  placeholders; parameters are never interpolated. The guard test now
  carries a scoped exemption for this package with that reasoning
  attached, rather than being deleted.
- **Credentials.** The DSN carries a password, so it is accepted from a
  file (`postgres.dsnFile`) or an environment variable, never a CLI
  argument — the same reasoning `-recover-admin-account` uses for not
  taking a password as an argument. It is never logged; connection
  errors are logged without the DSN.
- **TLS in transit is required, and enforced rather than requested.**
  `sslmode` below `require` is refused at startup instead of being
  quietly upgraded, because silently changing what the operator asked
  for hides a misconfiguration they need to know about. Note for the
  docs: `require` encrypts but does not verify the server certificate;
  `verify-full` is what actually stops an active attacker on the path,
  and is what the docs recommend.
- **This only delivers the separation it promises if Postgres is
  genuinely off-box.** Same-host Postgres exposes its credential to
  exactly the compromise it's meant to survive — the issue says this and
  the docs must too, prominently, because a same-host deployment looks
  identical from inside the app.
