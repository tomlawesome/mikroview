# Testing

See [docs/development.md](development.md) ("Testing expectations") for what
a test has to prove. This page covers the Go and frontend coverage ratchets
(#1333).

## Coverage is a ratchet, not a target

`test:go` writes `coverage.out` (`go test ./... -race -coverprofile=...`)
and `scripts/coverage-floor.py` checks it against
`supply-chain/coverage-floors.yml`, one floor per package. A package fails
below its floor, and also fails more than 5 points *above* it: sitting on
unlocked headroom means the floor is stale, and the fix is to raise it in
the same change, never to leave the gap sitting there. Floors only ever go
up — never lowered to make a change pass; a change that would need that
needs more tests instead.

The owner's band is 70–85% statement coverage (Go measures statements, not
branches — a 100% package means every line ran, not that every condition
was tried both ways). A package inside the band is done; effort goes to
packages below 70%, each of which carries its reason in the yml.

Measure the way `test:go` does or the numbers are wrong in both directions:
the `golang:1.27.1` image, `scripts/fetch-upgrade-fixtures.sh` run first,
and the test run itself as a non-root user (uid 10001) — root bypasses the
Unix permission bits some tests deliberately violate to prove a refusal.
`supply-chain/coverage-floors.yml`'s header has the exact commands.

`test:postgres` owns `internal/persist` and `internal/matchlog` outright
(#1290): `test:go` never sets `MIKROVIEW_TEST_POSTGRES`, so their
Postgres-backed tests skip themselves there, and `coverage-floor.py`'s
default mode (what `test:go` runs) skips both packages rather than
ratcheting a number it cannot see the whole of. `test:postgres` runs both
packages in full — file-backend and Postgres tests together, a strict
superset of what `test:go` alone exercises for them — with
`-coverprofile=coverage-postgres.out`, then checks it with
`python3 scripts/coverage-floor.py --section postgres-floors
coverage-postgres.out` against the `postgres-floors:` mapping at the
bottom of `supply-chain/coverage-floors.yml`: the same three ratchet rules
as above, scoped to that section.

## Frontend

`npm test -- --coverage` (`vitest run --coverage`, what `test:frontend`
runs) enforces `coverage.thresholds` in `frontend/vitest.config.ts` —
statements, branches, functions and lines, set at measured values and never
lowered, same ratchet rule as the Go floors.
Vitest reports branch coverage directly (unlike Go), so all four numbers are
real branch/statement/function/line percentages, not a statement-only proxy.

Measure the way `test:frontend` does: the `node:26-alpine` image, `npm ci`,
then `npm test -- --coverage` from `frontend/`. Measured 2026-09-27:
statements 82.4%, branches 69.28%, functions 82.34%, lines 84.58% — rounded
down to statements 82, branches 69, functions 82, lines 84 in the config.
