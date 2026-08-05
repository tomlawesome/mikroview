# Contributing

## Branching

Three lanes, same model as this project's sibling repos (`birdcage`):

- **`dev`** — unprotected, fast-moving. All issue work targets this.
  CI here is deliberately light: build/vet/test, `svelte-check`. This is
  where the messy stuff happens.
- **`preview`** — protected, PR-only (from `dev`). Gets the full gate:
  the auth-focused security job unconditionally, the container smoke
  test, and CodeQL. A merge here builds and publishes the actual release
  candidate image (`ghcr.io/tomlawesome/mikroview:preview`).
- **`main`** — protected, PR-only (from `preview`). A merge here never
  rebuilds anything; it retags the exact digest that was already built
  and tested from `preview`. The shop window — only what's actually
  ready ends up here.

A PR into `main` always gets the full security audit (`security` and
`container` jobs unconditionally, regardless of what changed) — see
`.github/workflows/ci.yml`. A PR into `dev`/`preview` runs the
auth-focused security job only when it actually touches auth-related
code (`internal/auth/**`, `internal/api/auth.go`, `internal/api/ws.go`,
the frontend auth components), so unrelated changes don't pay for
security-suite overhead they don't need.

## Testing expectations

- New behavior needs a test that would fail without it.
- A bug fix should include a regression test reproducing the bug where
  practical.
- Anything touching `internal/auth` or `internal/api/auth.go` should be
  run with `-race` locally before opening a PR — the CI security job
  does this too, but catching it locally is faster.

## Security

See [SECURITY.md](SECURITY.md). Dependabot watches Go modules, npm
packages, the Dockerfile's base images, and GitHub Actions versions
weekly, opening PRs against `dev`. CodeQL scans PRs into `preview`/`main`
plus a weekly full scan.
