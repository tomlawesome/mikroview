# MikroView agent instructions

Applies alongside the global agent instructions, which hold instruction
authority, trust of outside content, git-state discipline, issue and
decision recording, and credentials. Outside pull requests are not accepted
at all (README, "Contributions").

## What MikroView is for

MikroView is a **firewall-log interrogation helper**: it ingests the log
stream a MikroTik router exports and helps someone view, filter and make
sense of it. It is not a security suite and does not grow into one.

The test for a security-adjacent feature is where its signal comes from:

- **Derived from MikroView's own event stream** (the behavioural flags and
  their detectors): belongs here.
- **Produced by a separate tool and merely correlated alongside our data**:
  belongs in a companion project. Default to a separate repo, not an
  integration.

Worked example, OpenCanary and `birdcage`, and the reasoning:
[docs/decisions/scope-and-companion-projects.md](docs/decisions/scope-and-companion-projects.md).
README is a public advert, not a personal readme; keep it lean.

Coverage is about boundaries, not accepted versus denied traffic: an accept
where there should not be one is as much signal as a deny that repeats. The
owner has corrected the "log denies, not accepts" reading more than once.

## MikroView observes; it never scans or connects

A design invariant (owner, ratified 2026-08-15), not a current limitation:
**MikroView never probes, scans, or initiates a connection to any host on
the operator's network, the router included.** It ingests what is pushed to
it (syslog, router-state pushes) and fetches only its own external reference
feeds (blocklists, network-class data and their kin).

For anything identification- or diagnosis-flavoured, assemble the evidence
already flowing, state conclusions with their confidence, and where an
active step would help, **print the command for the operator to run, never
run it**. A feature that needs MikroView itself to touch the network is
redesigned around pushed or passive data, or belongs in a different tool.
Reasons: [docs/decisions/observe-never-probe.md](docs/decisions/observe-never-probe.md).

## Design

Design here means choosing an interaction model, laying out a screen or a
family of screens, deciding what a control affords and how it is discovered,
wording what the interface tells the operator, and the storyboard rounds
(#385 phase 2). If the question is "what should this feel like to use", it is
design, and it goes to the top-tier model under the global rules. The
interface is the product here, and a badly chosen interaction model is
inherited by every screen built on it (#439 is the worked example).
Implementing a decided design, wiring a specified control, styling to match
a pattern and fixing a shipped defect are ordinary implementation. Record
the design decision on the issue body when it lands, so the next person
builds against a written model. Fidelity work (building as drawn) also goes
to a top-tier model.

Building a ratified design: port the mockup's markup and CSS, never build
from an impression of it.

- Drawn but built differently is a **defect**: build it as drawn.
- Something the app does that the design draws nowhere is a **gap**: leave
  it off, keep its code, write it down, and never invent a home mid-build.

Text: [docs/design/screens/README.md](docs/design/screens/README.md).

## GitLab, with GitHub as the mirror

MikroView is developed on the self-hosted GitLab, project `ai/mikroview`
(id 53), remote `gitlab`. Full CI is `.gitlab-ci.yml`. GitHub (`origin`) is a
**read-only mirror** of `dev`, `preview` and `main`; it runs no tests and
takes no issues or pull requests.

- **One branch has one writer.** Never push to `origin`'s `dev`, `preview` or
  `main`, and never open or merge a GitHub pull request into them: the
  mirror push is refused and GitHub falls behind. Push feature branches to
  `gitlab`; open merge requests there (`glab mr create`, `Cut:` line in `-d`).
- Issues live on GitLab (`glab issue`, not `gh issue`). Always write `#941`
  (issue) or `!941` (merge request); they are counted separately and can
  share a number.
- `Closes #N` goes in the MR description and the commit message. GitLab
  closes only on a merge into the default branch, `dev`; check the issue
  after every merge anyway.
- `.gitlab/issue_templates/Work item.md` puts the current plan at the top of
  new issues; fix existing issues as you pick them up.
- The agent's `glab` config is `~/.config/glab-claude/` (`GLAB_CONFIG_DIR`).
  Never write token values down.
- `policy:promotion-hop` fails an MR into `preview` unless it comes from
  `dev`, and into `main` unless it comes from `preview`.
- A release is a protected `v*` tag pushed to `gitlab`
  ([release-versioning](docs/decisions/release-versioning.md)).
- Mirror mechanics, CI variable names, credentials and the issue history:
  [docs/gitlab-delivery.md](docs/gitlab-delivery.md).

## Verify before you push

Run `make precheck`: gofmt (with the empty-list guard CI uses),
`go test ./...` and `svelte-check --tsconfig ./tsconfig.app.json`. Traps it
exists for:

- `svelte-check --tsconfig ./tsconfig.json` reports 0 errors even with real
  type errors; CI uses `tsconfig.app.json`.
- Run `go test ./...` even for a frontend-only change:
  `injection_sinks_test.go` scans `frontend/src` as text, so a comment naming
  `innerHTML` fails it.
- `gofmt -l` drift is not caught by build, test or vet.
- Vitest timeouts in a full local run under load are not failures: re-run
  the file alone, and do not halve the workers (#1379).

Reasons: [docs/runner-host.md](docs/runner-host.md), "Match CI's exact commands".

## The gate and the second host

`make live-check` is the project's real test. Since 2026-09-08 it is the
**blocking** `gate` stage on every MR and `dev` pipeline (four shards on the
second host's runner); no local gate run is needed before a cut. It is not a
substitute for the test suite, nor the suite for it: the defects worth
finding here were found only by running the real binary in a real browser.

- `dev -> preview` needs one clean gate, which the `dev` pipeline supplies.
- A red gate job is the code or the host. Reproduce it with
  `scripts/gate-local.sh`: green there is a flake (log it in
  `docs/flakes.md`, retry), red is the defect.
- Scenarios share state and run in filename order. Judge a failure with a
  whole `make live-check` on both trees (a worktree at `gitlab/dev` and the
  branch), never one scenario twice.
- Add a `frontend/scripts/live-<thing>.mjs` scenario for the change in the
  same MR (`.claude/skills/live-check/SKILL.md`). Say plainly when something
  cannot be exercised (no RouterOS device, no identity provider).
- `MV_BROWSER` picks `chromium` (default), `firefox` or `webkit`; Chromium
  is not the safe choice just for being the default (#659 shipped a
  `style` attribute Firefox refuses under our CSP).
- Run WebKit only through the container
  (`scripts/gate-local.sh --browser webkit`), never a bare host checkout.
- Run the second host with `make live-check-remote`; reach it as the ssh alias
  `mikroview-runner` (account `mvagent`). **Build the image there, never
  pull it. Never put a token or registry credential on that host**, and do
  not work around its limits (no sudo, no ssh forwarding, no clone). It takes
  a lock (`~/gate-lock`); exit 75 means another run holds it, and
  `scripts/gate-remote.sh --wait` polls until it frees. It prunes the Docker
  cache after every run. `MV_SHARDS=4` runs the browser phase as four slices.
  If the runner is stuck at `concurrent = 1` with nothing measuring, the
  quiet-host hold is stale (`docs/runner-host.md`).

Account, rootless Docker socket, subuid traps, prune, lock and quiet-host
details: [docs/runner-host.md](docs/runner-host.md).

## The docs-only lane

`.gitlab-ci.yml` skips jobs a prose change cannot break. **Do not reorder the
rungs** (code path changed: run; else docs path changed: skip; else run), and
do not express "docs only" as a single `changes:`: one list alone would skip
an MR that touched `main.go` and `README.md` together. A new job takes
`*dev_or_mr_code` only if a docs change genuinely cannot fail it; if in
doubt use `*dev_or_mr`. The exempt jobs and why:
[docs/quality-strategy.md](docs/quality-strategy.md), "The docs-only lane".

## Demos the owner reviews

Run `scripts/demo-up.sh`: demo history, `live-env.sh up` with the demo flags,
envelope restore, seeding and the feed, then it prints the build stamp.

- Demo history is **restored, not ingested** (#959): back-dated ingest would
  break the "a night is only claimed once we watched it" promise.
- `seed-demo.py all` duplicates watchlist entries. Use `push`, `entities`
  and `accounts`. The instance needs `MV_DEMO_DEVICES=1` and
  `MV_DEMO_BUILD=1`, or the seeding half-wastes (#709, #713).
- The `feed` takes real time to fill the metrics hourline and register; run
  `seed-demo.py mutate` only once it has produced real flags.
- **Quote the build stamp** when handing a demo over. If the owner reports a
  fault the tree says is fixed, compare stamps before touching code.

Reasoning and the full sequence: [docs/demos.md](docs/demos.md).

## Dependencies, pins and licence

- MikroView is **AGPL-3.0 only**. Never vendor list or lookup data: fetch
  at runtime. Attribution still binds fetched data (Spamhaus DROP needs
  credit, with its date and copyright text). Reasoning:
  [scope-and-companion-projects](docs/decisions/scope-and-companion-projects.md).
- A new third-party dependency needs the owner's approval on an issue first,
  with the provenance case the global dependencies rule asks for (#1288).
  Check any feed's or dependency's licence and record it on the issue;
  permissive (MIT/BSD/Apache/ISC) is fine, copyleft or share-alike needs
  explicit written consideration. Never vendor CC BY-SA geo or IP datasets:
  their terms would travel with every copy (fetching at runtime is fine).
- The licence gate (`security:licence-policy`) looks inside shipped packages:
  bundled assets, embedded files and inlined third-party code need a reviewed
  `bundled-assets:` entry in `supply-chain/licence-policy.yml` (#1432).
- Renovate watches Go modules, npm, Dockerfiles, CI images and GitHub
  Actions (`renovate.json`); take its bumps. A
  version held back needs what blocks it, what it costs, and the trigger.
- `typescript` is held at `~6.0.2` (also in `renovate.json`; reason and
  trigger are on #286): `svelte-check` under TypeScript 7 silently checks
  46 files of 249, a quieter check still reporting success.
- **Base images are pinned by tag, never digest.** Revisit only when a bot
  opens digest bumps: [base-images-on-tags](docs/decisions/base-images-on-tags.md).
  This does not touch promoting the tested preview digest to the release tag.

## Rules

- **Removals are wholesale.** No alias, shim or friendlier-error handler; say
  it in `CHANGELOG.md` ([docs/development.md](docs/development.md)).
- **Security by design:** research a feature, with a CVE search, before
  designing it ([docs/security-by-design.md](docs/security-by-design.md)).
  Reproduce findings before acting on them, automated research included.
- **Where docs go.** `docs/configuration.md` carries every option
  exhaustively, opt-in ones present but commented out; never trim it to the
  essentials. README stays lean and carries no config snippets. Deep security
  rationale goes in `docs/security-by-design.md`, not a setup guide.
- The GitHub mirror is read-only; GitHub issues are off.
- Changelog and release notes, release surfaces and the promotion steps are in
  [release-versioning](docs/decisions/release-versioning.md).

## Where the detail lives

| Topic | File |
|---|---|
| Scope, companion projects, list data, why the wizard | [docs/decisions/scope-and-companion-projects.md](docs/decisions/scope-and-companion-projects.md) |
| Observe, never probe | [docs/decisions/observe-never-probe.md](docs/decisions/observe-never-probe.md) |
| Base images on tags | [docs/decisions/base-images-on-tags.md](docs/decisions/base-images-on-tags.md) |
| GitLab delivery, mirror, credentials names | [docs/gitlab-delivery.md](docs/gitlab-delivery.md) |
| Second host, gate in CI, CI-matching commands | [docs/runner-host.md](docs/runner-host.md) |
| Demo sequence and honesty rules | [docs/demos.md](docs/demos.md) |
| Docs-only lane, reuse gate | [docs/quality-strategy.md](docs/quality-strategy.md) |

Code, CI and docs cite these files, never this one.
