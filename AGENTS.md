# Mikroview agent instructions

Applies to Claude Code and any other AI tooling working in this
repository, alongside the global agent instructions (instruction
authority, trust of outside content, git-state discipline, issue and
decision recording, and credential rules all live there).

Outside pull requests are not accepted at all — see `CONTRIBUTING.md`.

## What mikroview is for, and what belongs somewhere else

Mikroview is a **firewall-log interrogation helper**: it ingests the log
stream a MikroTik router exports, and helps someone view, filter and make
sense of it. It is not a security suite and does not grow into one.

When a security-adjacent feature is proposed, the test is where its signal
comes from:

- **Derived from mikroview's own event stream** — belongs inside
  mikroview. The behavioural flags subsystem and its detectors qualify:
  every judgement they make is computed from traffic mikroview already
  sees.
- **Produced by a separate tool, and merely correlated alongside
  mikroview's data** — belongs in a companion project. Default
  recommendation for anything in this category is a separate repo, not an
  integration here.

The worked example is OpenCanary. Centralising honeypot alerts went
through several rounds and settled on **no OpenCanary-specific logic in
mikroview at all**: a rare honeypot hit flushed into the live view is lost
in a stream of real firewall lines, and a multi-instance honeypot
dashboard is a different product — it became `birdcage`. Mikroview's only
part is deliberately generic: a bounded before/after lookback query by IP
and timestamp (#29) that *any* external trigger can call to pull
surrounding traffic context. Not honeypot-aware, not coupled to birdcage.

This is not tidiness. `README.md` is a public advert for the app rather
than a personal readme, and an app that does one job well is a different
proposition from one accreting adjacent integrations. Absorbing a
neighbouring tool also inherits its failure modes, its dependencies and
its data licensing — see the next two sections for why the last of those
is not free.

## Why mikroview suggests, and why there is a wizard

A real deployment's accept-rule logging was ~90% of volume, burying the deny
signal; dropping it cut volume 97–99%
([docs/routeros-setup.md](docs/routeros-setup.md)). Fixing what an operator
logs is the product's opening argument, not onboarding decoration.

Coverage is about watching the right things in the right places, not about
accepts versus denies. An accept where there should not be one is as much
signal as a deny that keeps repeating; which matters is a property of the
boundary, not the rule's action. The volume story above is about noise, not
about which action to log -- the owner has corrected the "log denies, not
accepts" reading more than once (last 2026-09-03, designing #435).

## mikroview observes; it never scans or connects

Owner decision, ratified 2026-08-15, and a design invariant, not a
current limitation: **mikroview never probes, scans, or initiates a
connection to any host on the operator's network — the router
included.** It ingests what is pushed to it (syslog, router-state
pushes) and fetches only its own external reference feeds (blocklists,
network-class data, and their kin). The wizard's long-standing promise
— "MikroView still never connects to your router" — is the special case
of this general rule.

Two reasons, either sufficient alone. An observer that starts probing
changes character: it appears in other tools' logs as a scanner, and
its trust model shifts from "a place logs go" to "a thing with reach
into the network", which is a different product with a different
security posture. And the honesty ethos depends on it: mikroview's
evidence is what *arrived*, not what was elicited — a claim built on
traffic the product itself provoked is a different kind of claim, and
the interface has no way to mark the difference.

The permitted shape for anything identification- or diagnosis-flavoured
(#410 is the worked example): assemble the evidence already flowing,
state conclusions with their confidence, and where an active step would
genuinely help, **print the command for the operator to run** — never
run it. A feature that needs mikroview itself to touch the network is
either redesigned around pushed or passive data, or it belongs in a
different tool.

## Building a ratified design

Port the mockup's markup and CSS; never build from an impression of it. Drawn
but built differently is a **defect** -- build it as drawn. Something the app
does that the design draws nowhere is a **gap** -- leave it off, keep its code,
write it down, never invent a home mid-build. Fidelity work goes to a top-tier
model.

## A stale origin/dev incident

**This is not hypothetical.** In this same repo, a branch was cut with
`git checkout -b <name> origin/dev` shortly after several PRs had just
been merged into `dev` via the GitHub API -- which does not update a
local clone's `origin/dev` remote-tracking ref, only `git fetch` does.
The local ref was hours stale, so the new branch was built on the
*pre-merge* `dev`, not the real one. Nothing was lost -- `dev` on GitHub
was fine -- but every file the recent PRs had touched appeared "reverted"
in the new branch's working tree, and opening a PR from it would have
shown all of that work being undone. Caught by checking `git log
--oneline` against a freshly-fetched `origin/dev` before pushing, not by
assuming the checkout had done the right thing.

## Where development happens: GitLab, with GitHub as the mirror

Owner decision, 2026-09-04, tracked on #935: mikroview is developed on the
self-hosted GitLab instance, project `ai/mikroview` (project id 53), remote
name `gitlab`. Branches, merge requests and the `dev` → `preview` → `main`
promotions all happen there; full CI is `.gitlab-ci.yml` at the repo root.
GitHub (`origin`) is a read-only mirror of those three branches, and keeps
what already works there: CodeQL, the container registry and cosign signing,
Pages, secret scanning, and release tags. It runs no tests: the last two
(`ci.yml`) moved to GitLab under #942.

Why: the owner's runner hardware is faster and never queued; the project
keeps moving on days GitHub does not; checks that need Docker on the host
are easy to wire in here; and GitHub still serves the public repo, its
history and registry pulls, so nothing that depends on those breaks.

The rule that makes this safe: **one branch has one writer.** Never push to
`origin`'s `dev`, `preview` or `main`, and never open or merge a GitHub pull
request into them — a merge on that side forks the branch, the mirror push
is refused, and GitHub silently falls behind. Feature branches are pushed to
`gitlab`; merge requests are opened there (`glab mr create`, with the `Cut:`
line in `-d`).

Mechanics: after a merge, `sync:mirror-to-github` in `.gitlab-ci.yml` pushes
the branch to GitHub over SSH with a deploy key (one repository, push only —
deliberately not a token, so a leak of it cannot reach anything else). It
runs only when the `MIRROR_TO_GITHUB` CI/CD variable is `true`.
`policy:promotion-hop` fails a merge request into `preview` unless it comes
from `dev`, and into `main` unless it comes from `preview`.

Issues, planning and decisions live on GitLab too (owner decision,
2026-09-04, on #935): the whole GitHub tracker was imported here, keeping
every issue and merge request number, and Issues is switched off on the
GitHub mirror (owner, 2026-09-05, #935) — the mirror takes no pull requests
either, so nobody should raise issues there. `Closes #N` goes in the merge request
description as well as the commit message. GitLab closes from either, but
only on a merge into the default branch — which here is `dev`, so an
ordinary merge closes its issue with no extra job. Check the issue after
every merge anyway.

Credentials, names only — values are never written down here. The agent's
`glab` config lives in `~/.config/glab-claude/` (`GLAB_CONFIG_DIR`, host
`gitlab.tomlawson.io`); git over HTTPS to `gitlab` uses that same token
through `GIT_ASKPASS`; `scripts/gate-local.sh` uses a read-only deploy token in
`~/.config/mikroview/gitlab-credentials`. On the GitLab side, the CI/CD
variables are `GITHUB_MIRROR_SSH_KEY` (file, protected), `MIRROR_TO_GITHUB`
(protected) and `GITLAB_MR_TOKEN`.

See
[docs/decisions/gitlab-ci-root-in-container-test-failure.md](docs/decisions/gitlab-ci-root-in-container-test-failure.md)
for a concrete example of a GitHub/GitLab environment difference that
broke a test for a reason that had nothing to do with the code — worth
checking for similar defaults mismatches before assuming a GitLab-only
failure is a real regression.

### The docs-only lane

#1038: `.gitlab-ci.yml` skips the jobs a prose change cannot break. Two
lists at the top of that file, `*docs_only_paths` and `*code_paths`, and
every gated job asks them in one order: a code path changed → run; else a
docs path changed → never; else run. The last rung is the safety property.
Skipping is opt-in against the docs list and is never a fallback, so a path
in neither list, an empty diff, or a pipeline whose diff GitLab cannot
compute runs the full set. Do not reorder those rungs, and do not try to
express "docs only" in a single `changes:` — it asks whether *any* listed
path changed, so one list alone would skip a merge request that touched
`main.go` and `README.md` together.

Four jobs stay outside the lane on purpose, and each says so in a comment
where it is defined:

- `test:go` — five tests read and assert on `docs/configuration.md`.
- `test:build-checks` — runs the markdown anchor and link check.
- `security:gitleaks` — a secret pastes into prose as easily as into code.
- `gate:image` — near-free when `live-check.Dockerfile` is unchanged, and
  three other jobs pull the tag it publishes.

`policy:promotion-hop` and `sync:mirror-to-github` are outside it too:
neither keys off the diff at all.

`lint:changed-paths-guard` closes the one gap the ladder cannot: it runs
unconditionally and goes red naming any changed path in neither list, with
the instruction to add its tree to `*code_paths`. Its script reads the lists
from `.gitlab-ci.yml`; `scripts/ci-changed-paths-guard.test.sh` is its test.

Adding a job: it gets `<<: *dev_or_mr_code` (or `*dev_or_mr_code_frontend`)
only if a documentation change genuinely cannot fail it. If in doubt use
plain `*dev_or_mr` — running a job that did not need to run costs minutes;
skipping one that did costs a green pipeline that tested nothing.

## The second host live-check runs on

Live-check is slow and it holds the workstation while it runs, which is
the bottleneck #673 set out to relieve: its image exists so `make
live-check` can run somewhere other than the machine you are working on.
Somewhere else and local -- **not** in CI. No pipeline on either host
runs the gate, and none should; a job proposing to was closed unmerged
(#704, #705) once that was clear.

The image carries **all three engines**, and `MV_BROWSER` picks one --
`chromium` (the default), `firefox` or `webkit`. That is the second half
of what it is for: a suite any of the three can be driven against,
whenever a change warrants it, rather than a box dedicated to one of
them. Chromium is not the safe choice merely because it is the default:
#659 shipped a static `style` attribute Chromium tolerates and Firefox
refuses under this app's CSP, past live-check, vitest and every
screenshot, found by the owner opening the app.

So the two hosts are interchangeable for a run. If the workstation is
already busy with one, run the next one here instead -- that is the
point of it existing.

The second host is the box that also serves the GitLab runner. It has a
dedicated unprivileged account, `mvagent`, provisioned by the owner on
2026-08-31.

Reach it as **`mikroview-runner`** — an ssh config entry on the agent
host, key `~/.ssh/mikroview_runner`, key-only, no passphrase. The
hostname is deliberately not written here: this file already refuses to
carry network topology (see the GitLab section above), and the address
lives in the agent's own ssh config where it is needed. Ask the owner if
it is missing rather than guessing.

**What the account can do.** Run containers under its own rootless
Docker — its own daemon on its own socket
(`/run/user/1001/docker.sock`, `DOCKER_HOST` set in its `.bashrc`),
separate from the runner's. That is enough to build
`live-check.Dockerfile` and run `make live-check` inside it, which is
the whole job.

**Build the image, never pull it.** The published
`ghcr.io/tomlawesome/mikroview/live-check` is private, and a registry
credential on that host would be a secret nobody is watching. Building
from the Dockerfile needs none and tests the same thing. The repository
is private too, so the host cannot clone it -- from GitHub or GitLab --
and nothing should be added to let it.

**Use `make live-check-remote`.** It pushes the tree over SSH into a bare
repo on the host, checks it out, builds the image if it is not cached,
runs the gate, brings the log back as `gate-run.log`, and removes the
work tree afterwards. `MV_BROWSER=firefox make live-check-remote` picks
the engine; `MV_SHARDS=4 make live-check-remote` runs the browser phase
as four slices at once, in about a quarter of the wall time (#1004; the
live-check skill has the shape). `scripts/gate-remote.sh` carries the reasoning. The host is
single-tenant -- one branch, one work tree -- so the script takes a lock
(`~/gate-lock`) before it pushes and refuses (exit 75, distinct from a
gate failure) if another run already holds it (#809); run
`scripts/gate-remote.sh --wait` (or `MV_GATE_WAIT=1`) to poll until it
frees instead of refusing (#811). If the holder looks dead, follow the
`ssh ... rm -r ~/gate-lock` hint the refusal prints.

**No loop runs here (#831), and none runs anywhere since 2026-09-08.**
This host also carries GitLab CI, so a `scripts/gate-dev-loop.sh` run
landing here fought CI for the same CPU and a scenario lost to that
contention read as `dev` being broken. The loop moved to the workstation,
then retired when the CI gate became blocking (see "The gate blocks the
merge, in CI"). What remains: `scripts/gate-local.sh` -- same container,
same checks, no SSH -- to reproduce a red CI gate job on the workstation,
and this host available for a deliberate manual `scripts/gate-remote.sh`
run, taking the lock as before. `gate-dev-loop.sh` is kept, unused; its
logs went to `~/projects/.gate-logs/mikroview/`.

`git push` rather than rsync or a clone, because authentication then
happens from this side: nothing has to live over there. Only new objects
cross after the first run, `node_modules` and `worktrees/` never do, and
the host gets a real checkout so `live-env.sh` stamps a true SHA instead
of `nogit`. The bare repo and the built image stay behind as the cache;
the work tree does not.

**What it deliberately cannot do**, and none of it should be worked
around: no sudo; no read access to `/etc/gitlab-runner/config.toml`; no
SSH forwarding of any kind, so the account cannot be used as a route
into the rest of the network. It also holds no registry credential, so
it cannot pull the private gate image — build from the Dockerfile
instead, which needs no credential and tests the same thing.

**Never put a token on that host**, for a pull or a clone or anything
else. If a step seems to need one, the step is wrong.

**Quiet host (#1003).** perf:promotion writes `/srv/quiet-host/hold` on
this box before it measures; a root-owned unit sets `concurrent = 1` on
the runner until the flag goes or expires, so no sibling job starts
mid-measurement. No gate loop runs any more (see above)
and no longer checks this flag -- a manual `scripts/gate-remote.sh` run
here can still collide with a hold, same as any other job would. Install
steps: `deploy/quiet-host/README.md`. Stuck with `concurrent = 1` and
nothing measuring: `rm /srv/quiet-host/hold`.

Two traps met while setting this up, recorded so the next person does
not:

- `adduser` assigns a subordinate UID/GID range of its own. Adding a
  second with `usermod --add-subuids` gives the account two ranges, and
  rootless Docker then fails at `newuidmap` with an unhelpful map dump.
  One range only.
- `usermod` refuses while any process of that user is alive, and with
  lingering enabled `systemd --user` restarts faster than
  `loginctl terminate-user` returns. Editing the single line out of
  `/etc/subuid` and `/etc/subgid` avoids the fight entirely.

Unrelated to mikroview but observed on that host: `gitlab-runner` and
`tom` are assigned the *same* subordinate range in both files, so the
isolation between those two accounts' containers is thinner than it
looks. Reported to the owner; not an agent's to change.

## Security by design

New features are researched before they are designed, including an
explicit CVE search and a comparison against known secure and insecure
implementations. Industry norms carry weight but are verified rather than
assumed. See [docs/security-by-design.md](docs/security-by-design.md).

Findings are reproduced before being acted on — including findings from
automated research, which has in practice produced wrong version numbers
and inflated severity scores.

## List and lookup data: why the no-vendoring rule is absolute here

The global rule (fetch feeds at runtime, never vendor) applies; what is
project-specific is that for mikroview it is a licensing constraint
before it is a design preference.
Mikroview ships under the **GNU AGPL-3.0** (see [LICENSE](LICENSE)), with
a commercial licence offered alongside it for anyone who needs to escape
the AGPL's obligations (see
[COMMERCIAL-LICENSE.md](COMMERCIAL-LICENSE.md)). That second option is
what turns share-alike data into a real conflict rather than a formality:
CC BY-SA sources (IP2Location LITE, IPinfo's free tier, older GeoLite2)
cannot be sublicensed on commercial terms, so vendoring one would quietly
make the commercial licence undeliverable for every copy that contained
it. Separately, the cloud providers grant no redistribution licence on
their published IP-range documents at all. Fetching at runtime is
uncontroversial; shipping a copy is not.

It also means a stale release cannot ship stale security data — a failure
nobody would notice, because everything would appear to work.

So: before adopting any feed or dependency, check its licence and record
what you found in the issue. Permissive (MIT/BSD/Apache/ISC) is fine;
copyleft and share-alike are not — **not** because they conflict with the
AGPL (GPL-family code is compatible with it), but because of the
commercial licence above: a copyleft dependency you do not own cannot be
sublicensed on those terms. Attribution terms still bind data that
is fetched rather than shipped — Spamhaus DROP requires credit, and its
date and copyright text must travel with the data.

## Pin to the latest version

The Go toolchain, npm packages, Docker base images and GitHub Actions are
pinned to the latest available. When you touch a file that pins a version
— `go.mod`'s `go` directive, a `FROM` line, a `package.json` dependency,
`uses: action@vX` — check whether it is current and bump it in the same
pass rather than waiting to be asked.

Staying back needs a specific, defensible technical reason. "It would
enlarge the diff" and "it isn't broken" are not reasons.

Nor is "the newer version dropped something we use", on its own. That is
a reason to adapt to the newer version, not to sit on the older one —
unless the older line is itself still supported with security fixes.
Being unable to move is a blocked upgrade, and a blocked upgrade needs
three things written down: what blocks it, what the version behind costs,
and the concrete trigger for taking it. Without a trigger it is not a
decision, it is a version that stopped being looked at.

Where a bump is blocked, record it on the tracker rather than only in a
pin. The current example is `typescript`, held at `~6.0.2` because
`svelte-check` supports TypeScript 7 only behind an experimental flag,
under which it silently type-checks 46 files instead of 249 — see #286
for the measurement and the trigger. Note what that case turns on: the
upgrade was rejected because taking it would have made a *check* quieter
while still reporting success, which is the same failure this file warns
about for CI gates.

The cost of the alternative is not hypothetical: mikroview's `go`
directive sat at 1.23.4 long enough for Go 1.23 to leave its support
window entirely, leaving 22–29 unpatched standard-library CVEs in the
build. Nothing chose that; it simply happened because nothing bumped it.

Verify a bump the way you would verify any other change — build, tests,
container smoke test — before committing to it. A bump is a change, not a
formality.

### Base images stay on tags, not digests

A `FROM` line names a tag (`golang:1.26.6-alpine`,
`gcr.io/distroless/static-debian12:nonroot`), never a `@sha256:` digest.
That is a decision, not an omission — a digest is more reproducible and
immune to a tag being repointed under you, which is a real supply-chain
property to give up.

What it costs is the reason: a digest freezes the base image, so the
distroless base stops receiving the security patches that are published
*by moving the tag*. Nothing here bumps digests automatically, so pinning
them means a build that quietly gets more vulnerable the longer nobody
touches it — the same "it simply happened because nothing bumped it"
failure described above, applied to the one layer with no version number
to notice.

**Trigger for revisiting:** adopting a bot that opens digest bumps
(Renovate, or Dependabot's Docker ecosystem). With something bumping
them, digests cost nothing and should be taken. Until then the tag is
the safer of two imperfect options, and the images in question are
official ones from Google and Docker.

This is about `FROM` lines only. It does not affect promoting the exact
tested preview digest to the release tag — that digest pin is the point
of the release step, per the global delivery rules.

## Removals are wholesale

When a feature, flag, endpoint, or code path is removed, it is removed
completely. No aliases that print "this moved", no handlers that exist
only to return a friendlier error, no fields read solely to warn about a
state that no longer exists.

The reasoning is that pre-1.0 there is no installed base to protect, and
a shim is not free: it is code reachable from the outside that no longer
has a job, which is the definition of avoidable attack surface. A
retired command still parses argv, still opens config, still touches the
account store. Keeping it trades a real, permanent cost against a
one-time inconvenience for an operator with a stale runbook.

Removals are communicated in `CHANGELOG.md`, which is what release notes
are for. That is the correct channel for "this is gone and here is what
to do instead" -- not a stub in `main.go`.

## Where documentation goes

`docs/configuration.md` carries every configuration option exhaustively, with
opt-in ones present but commented out. Never trim it to "just the essentials":
an operator who cannot find an option assumes it does not exist.

`README.md` does not carry those snippets. It stays a lean public advert for
the app, for the reason given further up.

Deep security rationale belongs in `docs/security-by-design.md`, not in a setup
guide. The setup guide says what a setting does and why you would want it, in
plain language.

## Issues

Issues live on GitLab (`glab issue`, not `gh issue`). They were imported
from GitHub with their numbers intact, so #97 there is #97 here; GitHub's
copies are closed and read-only history. Merge requests kept their old pull
request numbers too, but GitLab counts issues and merge requests separately
where GitHub shared one sequence, so from now on an issue and a merge
request can carry the same number and mean different things. Always write
which — `#941` or `!941` — and never assume one implies the other.

Open does not mean undone: GitLab closes an issue only on a merge into the
default branch. Here that branch is `dev`, so a normal merge closes it.

GitHub issues are off: GitHub is a mirror and nothing more (owner,
2026-09-06). #953's public-intake arrangement is withdrawn; there is
nothing to check there at session start.

Issue-body, decision-recording and supersession rules follow the global
agent instructions. Project-specific: `.gitlab/issue_templates/Work item.md`
puts the current plan at the top for new issues; existing issues get
fixed as they are picked up.

This is not hypothetical. On #97 a `tar.gz` design had been dropped in
favour of a gzipped JSON envelope, with the reasoning in a comment — and
the tar design was later picked back up and re-analysed as though it
were still the plan, because the body still said so. The wasted work was
the small cost; proposing a superseded design back to the owner as a
live option was the real one.

#162 is the same failure the other way: its body still described a
hand-over-file design that had been implemented and then removed, so the
issue actively misinformed anyone reading it -- the exact failure #97
was about, repeated.

## UX and UI design is Fable 5's work

The global rules route design and architecture judgement to Fable 5. This
section records what that covers **here**, because "is this design?" is the
question that decides which rule applies.

What counts as design here: choosing an interaction model, laying out a
screen or a family of screens, deciding what a control affords and how it
is discovered, wording what the interface tells the operator, and the
storyboard rounds #385's phase 2 is built around. If the question is
"what should this feel like to use", it is design.

What does not: implementing a design already decided, wiring an
already-specified control, styling to match an existing pattern, or
fixing a defect in shipped UI. Those are ordinary implementation and
follow the normal delegation rules.

**Why.** The interface is the product here -- a firewall-log
interrogation helper is the sense its operator can make of the log, and
that sense is made or lost in the interface. Design mistakes are also the
expensive kind: an interaction model chosen badly is inherited by every
screen built on it and by the next feature that has to fit alongside, and
it costs a redesign rather than a patch. #439 is the worked example --
the first model proposed was rejected as fiddly, and the replacement came
from design judgement rather than from process.

**How to apply.** Record the design decision on the issue when it lands
(the body, per the issues rule below), so the next person implements
against a written model rather than re-deriving it.

## The gate blocks the merge, in CI

`make live-check` stands up the real binary with the real UI and drives it
in a real browser. It is the project's real test, and since 2026-09-08 it
is the gate of record on every MR and `dev` pipeline: the `gate` stage
(#1004), four slices of the browser phase in parallel plus the standalone
scripts, on the second host's runner, **blocking**. Sharding is what made
that affordable -- a nine-minute window, not the 45-minute run that the
2026-09-02 decision took off the critical path.

So:

- **An MR merges on green CI plus review, and green includes the gate.**
  No local gate run before a cut: a gate nobody is made to read is not a
  gate (owner, 2026-09-08), which is why the `allow_failure` tripwire and
  the workstation loop (`scripts/gate-dev-loop.sh`, #831) have retired.
- **A red gate job is either the code or the host.** Reproduce it with
  `scripts/gate-local.sh` on the quieter workstation: green there means a
  flake -- add its line to `docs/flakes.md` (testing-and-ci skill) and
  retry the job; red there is the defect, fixed on the branch.
- **`dev -> preview` still needs one clean gate**, which the `dev`
  pipeline supplies.

Still true: this is not a substitute for the test suite, and the suite is
not a substitute for this. Nearly every defect worth finding in this
project was found by running it, and none of them were visible from the
code or from `go test`: recovery keys reaching the container log through a
terminal check that a Docker pty satisfies; CLI commands writing files
nothing read on a Postgres deployment, reporting success while the
operator stayed locked out; a filter that became unevaluable only once
matching events arrived.

Add a scenario for the change -- `frontend/scripts/live-<thing>.mjs` --
in the same MR, so the gate exercises it on that pipeline. See
`.claude/skills/live-check/SKILL.md`.

Where something genuinely cannot be exercised here (no RouterOS device, no
external identity provider), say so plainly in the PR rather than letting
"tested" imply more than was observed.

### A scenario cannot be judged on its own

Scenarios share one instance and run in filename order, so most depend on
state an earlier one left -- `live-flags-clearing.mjs` alone raises no
flags at all. To tell a regression from a pre-existing failure, run the
whole `make live-check` on both trees, a worktree at `gitlab/dev` and the
branch, never the one scenario twice.

### Match CI's exact commands, not the obvious equivalents

Local verification is incomplete unless it used the same commands CI uses —
check `.gitlab-ci.yml` (the gate on merge requests and `dev`) and
`.github/workflows/*.yml` (what still runs on GitHub) for the precise
invocation. Three known traps:

- `npx svelte-check --tsconfig ./tsconfig.json` (the solution-level config)
  reports 0 errors even when the app has a real type error; it only checks
  the referenced-project setup. CI runs
  `npx svelte-check --tsconfig ./tsconfig.app.json` directly — do the same.
- Run `gofmt -l $(git ls-files '*.go')` before pushing. `go build`, `go
  test` and `go vet` do not catch formatting drift, and CI has a dedicated
  gofmt step that fails the whole Go + frontend job on it.
- Run `go test ./...` before pushing **even for a frontend-only change**.
  `injection_sinks_test.go` is a Go test scanning `frontend/src` for
  `{@html`, `innerHTML`, `outerHTML` and `insertAdjacentHTML`, matched as
  text anywhere — a comment saying a fixture avoids `innerHTML` fails for
  naming it.

The first two were found on PR #257 (Watchlist frontend), the third on
#581 — each a supposedly complete local pass that CI caught out.

## Demos the owner reviews

### History it: seven nights from a restored envelope, not a week of waiting

The demo cannot wait seven nights for real watchlist streaks and a week of
retained corpus, and faking them by ingesting back-dated events would break
what mikroview promises: a night is only ever claimed once this process
watched it end to end, or it was already recorded (`internal/engine/
definitions_nights.go`). Owner decision on #959: **restore it, don't ingest
it**. `-restore` writes a store straight to disk -- it is not the live
ingest path the honesty check guards, so history arriving that way breaks
nothing.

`cmd/demo-history` builds the envelope: a `definitions` store with seven
nights of streaks for the same six watchlist entries `scripts/seed-demo.py`
seeds (#738's feeder characters, so the story continues into today once the
feed starts), and a `retained_events` store of matching back-dated traffic
-- both ending yesterday, so today is genuinely the live feed's. `-restore`
seals `retained_events` into `history.dir` under this deployment's own key,
the same encrypted per-day files a live process writes; it never travels as
ciphertext in the envelope itself.

Generate it before seeding, and restore it into the instance
`live-env.sh` just brought up, stopping the server for the write -- the
full sequence, folded into "Seed it" below, is: generate the envelope,
bring the instance up, restore the envelope, then seed everything else and
start the feeder.

### Seed it: a demo on bare syslog is not a demo

`scripts/seed-demo.py` (#687) gives a running instance a whole story
rather than a log stream: pushed filter/NAT/address tables, named
entities, a user and a viewer account, and a watchlist with a healthy
entry, a held one and a deliberately broken ring. One estate throughout,
so a name on the stream is the same thing on the topography and in
Entities.

**Bring the instance up with `MV_DEMO_DEVICES=1`**, or the seeding is
half-wasted. A pushed rule/NAT/address table is keyed by device id;
`seed-demo.py` mints its tokens against router names and streams syslog
from one loopback address per router. Unless `live-env.sh` declares those
addresses, the registry invents a discovered device per source IP and the
pushed tables sit under ids no device has. Both halves report success and
never meet (#709).

`MV_DEMO_BUILD=1` matters here too: it ships the self-destroying service
worker (#713) that stops a browser serving back a stale precached build
across the many rebuilds a demo goes through. `live-env.sh up` no longer
sets it by default -- the live-check gate needs a real, non-self-destroying
worker to prove its own navigation scenario (#753) -- so a demo has to ask
for it explicitly.

    go run ./cmd/demo-history -out /tmp/demo-history.backup -force

    MV_DEMO_DEVICES=1 MV_DEMO_BUILD=1 MV_BIND=<addr> scripts/live-env.sh up
    export MV_URL=... MV_USER=... MV_PASS=... MV_DIR=...

    kill "$(cat "$MV_DIR/pid")"
    MIKROVIEW_CONFIG="$MV_DIR/cfg.yaml" "$MV_DIR/mikroview" -restore /tmp/demo-history.backup --force
    MIKROVIEW_CONFIG="$MV_DIR/cfg.yaml" "$MV_DIR/mikroview" > "$MV_DIR/server.log" 2>&1 &
    echo $! > "$MV_DIR/pid"

    export MV_SYSLOG_HOST=<the bind address> MV_SYSLOG_PORT=<tls port>
    scripts/seed-demo.py push
    scripts/seed-demo.py entities
    scripts/seed-demo.py accounts
    scripts/seed-demo.py feed &   # long-running; leave it running
    scripts/seed-demo.py mutate   # once the feed has produced real flags

Push/entities/accounts, not `all`: `all` also runs `watchlist`, which would
create six more, blank watchlist entries beside the six the restored
envelope already carries with their seven nights of history.

`feed` is the piece that takes time: the metrics hourline, the register
and the fall's memory stay flat until real time has passed under them.

Without this the fall's bands read "other traffic — not in a pushed rule
table", the topography degrades to boundary-derived zones, and the empty
surfaces get reported as UI defects. Every UI review this project ran
before the seeder existed was hampered that way, and reviews after it
existed were still handed bare-syslog instances because nobody checked.

### Stamp it: a demo must say which build it is

**Show a version on every demo instance the owner is asked to look at,
and quote the same version when handing it over.** Round 30 lost most of
a day to this: fault after fault was fixed in the tree while the running
instance had been built before the fixes, so the owner kept finding
faults that were already fixed and reasonably concluded the work had been
lost. Nothing in the browser said which build it was, so neither side
could tell.

A demo whose version cannot be read off the screen is not evidence of
anything. If the owner reports a fault that the tree says is fixed,
compare the demo's stamp against the branch before touching the code —
the usual answer is a stale build, not a lost fix.
