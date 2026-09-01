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

## Where code review happens: GitHub and GitLab, split by branch

> **UNDER CONSTRUCTION — NOT YET LIVE. DO NOT USE.**
>
> Everything below describes the **target state of a cutover that has not
> been activated**, written in the present tense before it happened. The
> GitLab lane is real and valid — the project exists, `dev` is pushed
> there, and its pipeline runs green through the `security` stage — but
> `dev` has **not** moved, and the lane is not a gate on anything yet.
>
> **Until it goes live, work through GitHub as normal**: push feature
> branches to `origin`, open GitHub pull requests, merge into `dev`
> there. Do not push to the `gitlab` remote, open merge requests there,
> or wait on a GitLab pipeline.
>
> **Do not "fix" the GitHub side to match this section.** The retirements
> it describes are cutover steps that have not been taken, and the things
> it says were replaced are still live and should stay live:
> `.github/workflows/close-issues-on-dev.yml` still fires on a PR merging
> into `dev`, and `.github/dependabot.yml` still carries its
> `target-branch: dev` entries. Removing either now would break working
> automation in exchange for a lane that cannot yet replace it.
>
> Known blocker on activation: the `sync` stage
> (`mirror-to-github`, `close-github-issues`) has never successfully run
> — it needs a `GITHUB_MIRROR_TOKEN` CI/CD variable that does not exist
> yet. Until that works, cutting over would silently stop GitHub issues
> closing on merge.
>
> The cutover involves **two** GitLab projects for different purposes —
> `ai/mikroview` (the eventual cutover target) and `ai/mikroview-mirror`
> (parallel checks only, no integration) — which are not interchangeable.

Issues, planning, and decisions stay on GitHub, per the rest of this file.
Code review does not, for one lane: `dev` (and the feature branches that
merge into it) now lives primarily on a private, self-hosted GitLab
instance — merges into `dev` happen as GitLab merge requests, not GitHub
pull requests. `preview` and `main`, and everything that already runs on
them (`ci.yml`, `docker.yml`, `codeql.yml`, `branch-policy.yml`), are
untouched and still live entirely on GitHub.

Why: GitHub's free tier doesn't include GHAS-equivalent SAST/dependency/
container scanning for private repos, and GitLab CE doesn't either — but
self-hosting GitLab means that same category of free/open-source tooling
(Semgrep, gosec, govulncheck, gitleaks, Trivy) can run on every merge into
`dev`, gated by `.gitlab-ci.yml` at the repo root, without paying for
either platform's higher tier. GitHub's own strengths (CodeQL, cosign
image signing + attestation, release tagging, `branch-policy.yml`'s
hop-order enforcement) had no reason to move, so they didn't.

Mechanically:
- A GitLab CI job mirrors `dev` to GitHub after each merge there, so the
  existing `dev` → `preview` GitHub PR flow keeps working unchanged —
  GitHub just sees `dev` updated by a push instead of a human PR merge.
- `close-issues-on-dev.yml` stopped firing (its trigger was a GitHub PR
  merging into `dev`, which no longer happens) and was replaced by an
  equivalent job in `.gitlab-ci.yml` that closes the same GitHub issues
  via the GitHub API on merge. **(Not done — a cutover step, not a
  completed one. The workflow is still in `.github/workflows/` and still
  firing; the GitLab job that would replace it has never run
  successfully.)**
- `dependabot.yml`'s `dev`-targeting entries were retired in favor of
  Renovate running on GitLab against `dev`, so two bots aren't both
  opening PRs against a branch that's now GitLab-canonical.
  **(Not done — a cutover step. All four `target-branch: dev` entries are
  still present and active, and `dev` is not GitLab-canonical yet.)**
- In-flight branches from before this change finished their existing
  GitHub PR flow normally — this was a soft cutover, not a rewrite of
  history.

If a check ever needs to move between the two (something currently gated
on GitHub would fit better as a fast `dev`-lane gate, or vice versa), the
boundary to reason from is: **GitHub keeps anything about the release
lane or an already-working platform feature (signing, CodeQL, hop-order
enforcement); GitLab handles fast, free security/quality scanning on the
inner dev loop.** Deliberately not documented here: which host anything
actually runs on, network topology, or credentials — that's operational
detail with no reason to live in a file an agent (or anyone else) might
read.

See
[docs/decisions/gitlab-ci-root-in-container-test-failure.md](docs/decisions/gitlab-ci-root-in-container-test-failure.md)
for a concrete example of a GitHub/GitLab environment difference that
broke a test for a reason that had nothing to do with the code — worth
checking for similar defaults mismatches before assuming a GitLab-only
failure is a real regression.

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
the engine. `scripts/gate-remote.sh` carries the reasoning.

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

Open does not mean undone: `Closes #N` fires only on the default branch, so
check the branch before picking an issue up.

Issue-body, decision-recording and supersession rules follow the global
agent instructions. Project-specific: `.github/ISSUE_TEMPLATE/work-item.md`
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

## Run it before you ship it

Every change that touches the server, the UI or the CLI gets a live check
before its PR opens: `make live-check` stands up the real binary with the
real UI and drives it in a real browser.

This is not a substitute for the test suite, and the suite is not a
substitute for this. Nearly every defect worth finding in this project was
found by running it, and none of them were visible from the code or from
`go test`: recovery keys reaching the container log through a terminal
check that a Docker pty satisfies; CLI commands writing files nothing read
on a Postgres deployment, reporting success while the operator stayed
locked out; a filter that became unevaluable only once matching events
arrived.

Add a scenario for the change -- `frontend/scripts/live-<thing>.mjs` --
rather than running the baseline and calling it done. See
`.claude/skills/live-check/SKILL.md`.

Where something genuinely cannot be exercised here (no RouterOS device, no
external identity provider), say so plainly in the PR rather than letting
"tested" imply more than was observed.

### A scenario cannot be judged on its own

Scenarios share one instance and run in filename order, so most depend on
state an earlier one left -- `live-flags-clearing.mjs` alone raises no
flags at all. To tell a regression from a pre-existing failure, run the
whole `make live-check` on both trees, a worktree at `origin/dev` and the
branch, never the one scenario twice.

### Match CI's exact commands, not the obvious equivalents

Local verification is incomplete unless it used the same commands CI uses —
check `.github/workflows/*.yml` for the precise invocation. Three known traps:

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

    MV_DEMO_DEVICES=1 MV_BIND=<addr> scripts/live-env.sh up

    export MV_URL=... MV_USER=... MV_PASS=...
    export MV_SYSLOG_HOST=<the bind address> MV_SYSLOG_PORT=<tls port>
    scripts/seed-demo.py all      # push, entities, accounts, watchlist
    scripts/seed-demo.py feed &   # long-running; leave it running
    scripts/seed-demo.py mutate   # once the feed has produced real flags

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
