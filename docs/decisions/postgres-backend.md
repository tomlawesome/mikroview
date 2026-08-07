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
on a machine without one. CI runs them via a service container.

A mock would test that the code calls the functions it calls. The things
worth testing here — that the CAS actually rejects a stale write, that
the migration is byte-identical, that a partially applied migration
rolls back — are properties of the database, and only a database can
demonstrate them.

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
