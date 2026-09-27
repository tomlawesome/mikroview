# Testing

See [docs/development.md](development.md) ("Testing expectations") for what
a test has to prove. This page covers the Go coverage ratchet (#1333).

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

`test:postgres` runs `internal/persist` and `internal/matchlog`'s
Postgres-backed tests separately, without `-coverprofile` — `test:go` never
sets `MIKROVIEW_TEST_POSTGRES`, so those tests skip themselves there. Their
floors read low for that reason, not because the Postgres backend is
untested; capturing that coverage is out of scope for this ratchet (a
separate decision, not yet made).
