# The second host: live-check, the gate and the runner

Reference for the box that serves the GitLab runner and runs `make
live-check` away from the workstation. `AGENTS.md` keeps the short rules;
this holds the account, the mechanics and the history. Moved out of
`AGENTS.md` 2026-10-10. The host's address is deliberately not written
anywhere in the repository: reach it as the ssh alias `mikroview-runner`.

## Why it exists

Live-check is slow and it holds the workstation while it runs, which is the
bottleneck #673 set out to relieve: its image exists so `make live-check`
can run somewhere other than the machine you are working on. Since
2026-09-08 that somewhere also includes GitLab CI: `gate:scenarios` runs it
as four parallel shards, on every MR and `dev` pipeline, blocking the merge
(see "The gate in CI" below). #704 and #705, which closed a job proposing
that unmerged, were superseded once the move became deliberate -- they are
not still-standing objections.

So the two hosts are interchangeable for a run. If the workstation is
already busy with one, run the next one on the second host instead.

## Browser engines

The image carries **all three engines**, and `MV_BROWSER` picks one --
`chromium` (the default), `firefox` or `webkit`: a suite any of the three
can be driven against, whenever a change warrants it, rather than a box
dedicated to one of them. Chromium is not the safe choice merely because it
is the default: #659 shipped a static `style` attribute Chromium tolerates
and Firefox refuses under this app's CSP, past live-check, vitest and every
screenshot, found by the owner opening the app.

Run WebKit through the container (`scripts/gate-local.sh --browser
webkit`), never from a bare host checkout: Playwright's WebKit needs system
libraries (`libicudata.so.76` and others) that only the image has, and the
host error reads as "WebKit is broken" (#1311).

## The account

The box has a dedicated unprivileged account, `mvagent`, provisioned by the
owner on 2026-08-31. Reach it as **`mikroview-runner`** -- an ssh config
entry on the agent host, key `~/.ssh/mikroview_runner`, key-only, no
passphrase. The address lives in the agent's own ssh config; ask the owner
if the entry is missing rather than guessing.

**What the account can do.** Run containers under its own rootless Docker --
its own daemon on its own socket (`/run/user/1001/docker.sock`, `DOCKER_HOST`
set in its `.bashrc`), separate from the runner's. That is enough to build
`live-check.Dockerfile` and run `make live-check` inside it, which is the
whole job.

**What it deliberately cannot do**, and none of it should be worked around:
no sudo; no read access to `/etc/gitlab-runner/config.toml`; no SSH
forwarding of any kind, so the account cannot be used as a route into the
rest of the network. It holds no registry credential, so it cannot pull the
private gate image.

**Build the image, never pull it.** The published
`ghcr.io/tomlawesome/mikroview/live-check` is private, and a registry
credential on that host would be a secret nobody is watching. Building from
the Dockerfile needs none and tests the same thing. The repository is
private too, so the host cannot clone it -- from GitHub or GitLab -- and
nothing should be added to let it.

**Never put a token on that host**, for a pull or a clone or anything else.
If a step seems to need one, the step is wrong.

## Running the gate there: `make live-check-remote`

It pushes the tree over SSH into a bare repo on the host, checks it out,
builds the image if it is not cached, runs the gate, brings the log back as
`gate-run.log`, and removes the work tree afterwards.
`MV_BROWSER=firefox make live-check-remote` picks the engine;
`MV_SHARDS=4 make live-check-remote` runs the browser phase as four slices
at once, in about a quarter of the wall time (#1004; the live-check skill
has the shape). `scripts/gate-remote.sh` carries the reasoning.

`git push` rather than rsync or a clone, because authentication then
happens from this side: nothing has to live over there. Only new objects
cross after the first run, `node_modules` and `worktrees/` never do, and the
host gets a real checkout so `live-env.sh` stamps a true SHA instead of
`nogit`. The bare repo and the built image stay behind as the cache; the
work tree does not.

**Lock.** The host is single-tenant -- one branch, one work tree -- so the
script takes a lock (`~/gate-lock`) before it pushes and refuses (exit 75,
distinct from a gate failure) if another run already holds it (#809). Run
`scripts/gate-remote.sh --wait` (or `MV_GATE_WAIT=1`) to poll until it frees
instead of refusing (#811). If the holder looks dead, follow the
`ssh ... rm -r ~/gate-lock` hint the refusal prints.

**Prune.** The script prunes `mvagent`'s Docker cache after every run, pass
or fail (#1387): dangling images and the whole build cache go, but the
tagged `mv-gate:local` image stays, so the next run is still cached. A hand
prune from another account, same rootless daemon:
`sudo -u mvagent env DOCKER_HOST=unix:///run/user/1001/docker.sock docker builder prune -af`
(and the same with `docker image prune -af` for images).

## The dev loop is retired

No loop runs here (#831), and none runs anywhere since 2026-09-08. This host
also carries GitLab CI, so a `scripts/gate-dev-loop.sh` run landing here
fought CI for the same CPU and a scenario lost to that contention read as
`dev` being broken. The loop moved to the workstation, then retired when the
CI gate became blocking. What remains: `scripts/gate-local.sh` -- same
container, same checks, no SSH -- to reproduce a red CI gate job on the
workstation, and this host available for a deliberate manual
`scripts/gate-remote.sh` run, taking the lock as before. `gate-dev-loop.sh`
is kept, unused; its logs went to `~/projects/.gate-logs/mikroview/`.

## The gate in CI

`make live-check` stands up the real binary with the real UI and drives it
in a real browser. It is the project's real test, and since 2026-09-08 it is
the gate of record on every MR and `dev` pipeline: the `gate` stage (#1004),
four slices of the browser phase in parallel plus the standalone scripts, on
this host's runner, **blocking**. Sharding is what made that affordable -- a
nine-minute window, not the 45-minute run that the 2026-09-02 decision took
off the critical path.

- **An MR merges on green CI plus review, and green includes the gate.** No
  local gate run before a cut: a gate nobody is made to read is not a gate
  (owner, 2026-09-08), which is why the `allow_failure` tripwire and the
  workstation loop (`scripts/gate-dev-loop.sh`, #831) have retired.
- **A red gate job is either the code or the host.** Reproduce it with
  `scripts/gate-local.sh` on the quieter workstation: green there means a
  flake -- add its line to `docs/flakes.md` (testing-and-ci skill) and retry
  the job; red there is the defect, fixed on the branch.
- **`dev -> preview` still needs one clean gate**, which the `dev` pipeline
  supplies.

This is not a substitute for the test suite, and the suite is not a
substitute for this. Nearly every defect worth finding in this project was
found by running it, and none of them were visible from the code or from
`go test`: recovery keys reaching the container log through a terminal check
that a Docker pty satisfies; CLI commands writing files nothing read on a
Postgres deployment, reporting success while the operator stayed locked out;
a filter that became unevaluable only once matching events arrived.

Add a scenario for the change -- `frontend/scripts/live-<thing>.mjs` -- in
the same MR, so the gate exercises it on that pipeline. See
`.claude/skills/live-check/SKILL.md`. Where something genuinely cannot be
exercised here (no RouterOS device, no external identity provider), say so
plainly in the PR rather than letting "tested" imply more than was observed.

### A scenario cannot be judged on its own

Scenarios share one instance and run in filename order, so most depend on
state an earlier one left -- `live-flags-clearing.mjs` alone raises no flags
at all. To tell a regression from a pre-existing failure, run the whole
`make live-check` on both trees, a worktree at `gitlab/dev` and the branch,
never the one scenario twice.

### A vitest timeout in a full local run is not a failure

On a host loaded by other agents' suites, a full `npx vitest run` times out a
different handful of slow tests each time (LiveTable, City, EngineRoom,
Topography). Re-run the failing file alone: if it passes, carry on and log
nothing. Only an assertion failure, or a timeout that repeats in isolation
or in CI, is real. No config change fixes this: halving vitest's workers
helped locally and broke `test:frontend` on the runner (#1379, owner,
2026-09-27).

### Match CI's exact commands

`make precheck` runs the commands CI runs: gofmt (with the empty-list guard
CI uses), `go test ./...` and `svelte-check --tsconfig ./tsconfig.app.json`.
The reasons behind each:

- `npx svelte-check --tsconfig ./tsconfig.json` (the solution-level config)
  reports 0 errors even when the app has a real type error; it only checks
  the referenced-project setup. CI runs `--tsconfig ./tsconfig.app.json`.
- `go build`, `go test` and `go vet` do not catch formatting drift, and CI
  has a dedicated gofmt step that fails the whole Go + frontend job on it.
- `go test ./...` is needed **even for a frontend-only change**:
  `injection_sinks_test.go` is a Go test scanning `frontend/src` for
  `{@html`, `innerHTML`, `outerHTML` and `insertAdjacentHTML`, matched as
  text anywhere -- a comment saying a fixture avoids `innerHTML` fails for
  naming it.

The first two were found on PR #257 (Watchlist frontend), the third on #581
-- each a supposedly complete local pass that CI caught out.

## Quiet host (#1003)

`perf:promotion` writes `/srv/quiet-host/hold` on this box before it
measures; a root-owned unit sets `concurrent = 1` on the runner until the
flag goes or expires, so no sibling job starts mid-measurement. No gate loop
runs any more and none checks this flag -- a manual `scripts/gate-remote.sh`
run here can still collide with a hold, same as any other job would. Install
steps: `deploy/quiet-host/README.md`. Stuck with `concurrent = 1` and nothing
measuring: `rm /srv/quiet-host/hold`.

## Setup traps

Two traps met while provisioning the account, recorded so the next person
does not:

- `adduser` assigns a subordinate UID/GID range of its own. Adding a second
  with `usermod --add-subuids` gives the account two ranges, and rootless
  Docker then fails at `newuidmap` with an unhelpful map dump. One range
  only.
- `usermod` refuses while any process of that user is alive, and with
  lingering enabled `systemd --user` restarts faster than `loginctl
  terminate-user` returns. Editing the single line out of `/etc/subuid` and
  `/etc/subgid` avoids the fight entirely.

Unrelated to MikroView but observed on that host: `gitlab-runner` and the
human account are assigned the *same* subordinate range in both files, so the
isolation between those two accounts' containers is thinner than it looks.
Reported to the owner; not an agent's to change.
