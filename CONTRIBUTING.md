# Contributing

## Local development

Requires Go 1.26+ and Node 22+.

```sh
make dev-backend    # go run ., syslog on :1514, https on :8080 (TLS on by default -- see docs/configuration.md#tls)
make dev-frontend   # vite dev server on :5173, proxies /api to :8080 over TLS
make test           # go test ./... + svelte-check
make build           # full build: frontend -> web/dist -> single Go binary
make docker          # docker build -t mikroview .
```

Feed it fixture syslog lines without a real router, e.g.:

```sh
printf '<134>Jan 15 10:22:31 MikroTik A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60' \
  | nc -u -w1 127.0.0.1 1514
```

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

## Code contributions

**MikroView doesn't accept outside pull requests.** That isn't hostility
or a comment on anyone's code — it's simply that reviewing contributions
properly takes time this project doesn't have, and reviewing them badly
would be worse than not reviewing them at all.

**Issues are genuinely welcome**, and they're the right channel for
everything:

- bug reports, including ones you've already diagnosed
- feature requests and ideas
- "this is wrong and here's why", with as much detail as you like
- questions about how something works

Pointing at the exact line and describing the fix in an issue is useful
and appreciated. It just gets implemented here rather than merged from
elsewhere.

**You're free to fork.** The AGPL gives you that right and nothing here
restricts it. If you want your own version, maintain it — you just need
to keep it AGPL and publish your source (see [LICENSE](LICENSE)).

If an exception is ever made and a pull request is accepted, then by
submitting it you confirm the work is yours to contribute, and you
license it under the AGPL v3.0 **and** additionally grant Tom Bridgwater-Lawson the
right to license it under other terms. That second part is what keeps
the commercial licence possible (see
[COMMERCIAL-LICENSE.md](COMMERCIAL-LICENSE.md)). You keep the copyright
in your own work either way.

## Security by design

New features are researched before they are designed — including an
explicit CVE search and a comparison against known secure and insecure
implementations. See
[docs/security-by-design.md](docs/security-by-design.md) for what that
requires and why.
