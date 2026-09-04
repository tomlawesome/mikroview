# Contributing

## Local development

Requires Go 1.26+ and Node 22+.

```sh
make dev-backend    # go run ., syslog TLS on :6514, https on :8080 (TLS on by default -- see docs/configuration.md#tls)
make dev-frontend   # vite dev server on :5173, proxies /api to :8080 over TLS
make test           # go test ./... + svelte-check
make build           # full build: frontend -> web/dist -> single Go binary
make docker          # docker build -t mikroview .
```

Working in a linked git worktree (`git worktree add`), add
`-buildvcs=false` to any `go` command you type directly:

```sh
go build -buildvcs=false ./...
```

The Makefile and the scripts already set it. Go's VCS stamping looks for
a `.git` *directory*, a worktree's `.git` is a file, so the lookup walks
past the checkout onto whichever repository is above it -- which either
fails the build or stamps someone else's commit into your binary (#357,
golang/go#58218, fixed in Go 1.27).

Feed it fixture syslog lines without a real router. Since #189 there is
no plaintext listener -- the only one is RouterOS's own
`remote-protocol=tls` on 6514 -- so this has to speak TLS, which `nc`
cannot. The harness already has a sender that does:

```sh
eval "$(scripts/live-env.sh up)"     # exports MV_URL, MV_USER, MV_PASS
scripts/live-env.sh syslog 200       # 200 synthetic firewall events
scripts/live-env.sh raw '<134>Jan 15 10:22:31 MikroTik A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new, proto TCP (SYN), 192.168.1.50:51234->1.2.3.4:443, len 60'
scripts/live-env.sh down
```

For the real thing rather than fixtures, `make live-routeros-container`
boots a genuine RouterOS CHR and points it at the shipped container --
see `.claude/skills/live-check/SKILL.md`.

## Branching

Development happens on the owner's self-hosted GitLab; this GitHub
repository is its mirror. `dev`, `preview` and `main` are written on
GitLab and pushed here after every merge, so a pull request into any of
them here is refused by the `branch policy` check. Three lanes, same
model as this project's sibling repos:

- **`dev`** — the integration branch. All issue work targets this by
  merge request on GitLab, where the lint, test and security jobs run
  (`.gitlab-ci.yml`).
- **`preview`** — promoted from `dev` only. A merge here builds and
  publishes the release candidate image
  (`ghcr.io/tomlawesome/mikroview:preview`) from GitHub, via
  `.github/workflows/docker.yml`.
- **`main`** — promoted from `preview` only. A merge here never
  rebuilds anything; it retags the exact digest that was already built
  and tested from `preview`. The shop window — only what's actually
  ready ends up here.

GitHub still runs CodeQL and secret scanning on what arrives, and
`.github/workflows/ci.yml` still runs the container smoke test and the
Postgres integration check until those move to GitLab too.

## Testing expectations

- New behavior needs a test that would fail without it.
- A bug fix should include a regression test reproducing the bug where
  practical.
- Anything touching `internal/auth` or `internal/api/auth.go` should be
  run with `-race` locally before opening a PR — the CI security job
  does this too, but catching it locally is faster.

## Security

See [SECURITY.md](SECURITY.md). Dependabot alerts stay on for Go
modules, npm packages, the Dockerfile's base images and GitHub Actions
versions, as notifications only: it opens no pull requests, because
nothing merges here. CodeQL scans every push to `dev`/`preview`/`main`
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
