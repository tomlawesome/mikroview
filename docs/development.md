# Developing MikroView

How to run, feed and test a checkout. Outside contributions are not
accepted (see the README), so this is for the owner, the agents that
work here, and anyone maintaining a fork.

## Local development

Requires Go 1.27+ and Node 22+.

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

Authentication is the gauntlet Go library (`github.com/tomlawesome/gauntlet`,
pinned in `go.mod`, #1202). Set `GOPRIVATE=github.com/tomlawesome/gauntlet` for
any Go command that might fetch it (`go get`, `go mod download`, `go mod
tidy`). That sends the fetch straight to GitHub and skips the checksum
database; resolving it through the public Go proxy would publish the version
permanently. `.gitlab-ci.yml` sets the same variable for every job.

Feed it fixture syslog lines without a real router. Since #189 there is
no plaintext listener -- the only one is RouterOS's own
`remote-protocol=tls` on 6514 -- so this has to speak TLS, which `nc`
cannot. The harness already has a sender that does:

```sh
eval "$(scripts/live-env.sh up)"     # exports MV_URL, MV_USER, MV_PASS
scripts/live-env.sh syslog 200       # 200 synthetic firewall events
scripts/live-env.sh raw '<134>Jan 15 10:22:31 MikroTik A|lan-wan|forward: in:ether1 out:bridge1, connection-state:new, proto TCP (SYN), 192.168.1.50:51234->203.0.113.4:443, len 60'
scripts/live-env.sh down
```

For the real thing rather than fixtures, `make live-routeros-container`
boots a genuine RouterOS CHR and points it at the shipped container --
see `../.claude/skills/live-check/SKILL.md`.

## Testing expectations

- New behavior needs a test that would fail without it.
- A bug fix should include a regression test reproducing the bug where
  practical.
- Anything touching the authentication wiring (`gauntlet_wiring.go`,
  `internal/api/gauntlet.go`, `storage.go`) should be run with `-race`
  locally — the CI security job does this too, but
  catching it locally is faster.
- Go coverage is gated per package by a ratchet — see
  [docs/testing.md](testing.md).

## Conventions

**Removals are wholesale.** When a feature, flag, endpoint, or code path is
removed, it is removed completely. No aliases that print "this moved", no
handlers that exist only to return a friendlier error, no fields read solely
to warn about a state that no longer exists.

The reasoning is that pre-1.0 there is no installed base to protect, and a
shim is not free: it is code reachable from the outside that no longer has a
job, which is the definition of avoidable attack surface. A retired command
still parses argv, still opens config, still touches the account store.
Keeping it trades a real, permanent cost against a one-time inconvenience for
an operator with a stale runbook.

Removals are communicated in `CHANGELOG.md`, which is what release notes are
for. That is the correct channel for "this is gone and here is what to do
instead" -- not a stub in `main.go`.

**Dependencies and list data.** A new third-party dependency needs the
owner's approval on an issue first, and list or lookup data is fetched at
runtime, never vendored. The reasons are in
[docs/decisions/scope-and-companion-projects.md](decisions/scope-and-companion-projects.md).

## Security

See [SECURITY.md](../SECURITY.md).

## Security by design

New features are researched before they are designed — including an
explicit CVE search and a comparison against known secure and insecure
implementations. See
[docs/security-by-design.md](security-by-design.md) for what that
requires and why.

## Project records

- [docs/quality-strategy.md](quality-strategy.md) — what the
  review and gate process is
- [docs/flakes.md](flakes.md) — the flake record: checks that
  failed and passed again on unchanged code
- [docs/ci-scanners.md](ci-scanners.md) — which scanners the CI
  pipeline runs and what each one checks
- [docs/routeros-chr-exercise.md](routeros-chr-exercise.md) — the
  RouterOS CHR exercise the pipeline runs
- [docs/release-signing.md](release-signing.md) — how a release's two
  independent signatures are produced, and the owner-only setup on the
  signing runner behind them
- [docs/gitlab-delivery.md](gitlab-delivery.md) — how development and
  release run on GitLab, with GitHub as the mirror
- [docs/runner-host.md](runner-host.md) — the second host: live-check,
  the CI gate and the runner
- [docs/demos.md](demos.md) — building a demo instance the owner can review
- [docs/decisions/](decisions/) — design and delivery decisions
