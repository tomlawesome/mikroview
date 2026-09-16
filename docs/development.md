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
see `../.claude/skills/live-check/SKILL.md`.

## Testing expectations

- New behavior needs a test that would fail without it.
- A bug fix should include a regression test reproducing the bug where
  practical.
- Anything touching `internal/auth` or `internal/api/auth.go` should be
  run with `-race` locally — the CI security job does this too, but
  catching it locally is faster.

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
- [docs/routeros-chr-exercise.md](routeros-chr-exercise.md) — the
  RouterOS CHR exercise the pipeline runs
- [docs/decisions/](decisions/) — design and delivery decisions
