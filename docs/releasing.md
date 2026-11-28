# Releasing MikroView

A release goes `dev` -> `preview` -> `main`, each step a merge request on
GitLab (`ai/mikroview`), and is made by a `vX.Y.Z` tag on `main`'s commit.
GitLab builds, tests, signs and publishes the image, to GHCR too; GitHub is a
mirror that verifies and attests the GHCR digest and adds the owner's second
signature. How
the two signatures work, and the owner-only signing setup, is in
[release-signing.md](release-signing.md). Why the version lives in `VERSION`
and the tag is the trigger is in
[decisions/release-versioning.md](decisions/release-versioning.md).

Words this guide uses:

- The **release agent** is whoever runs the release for the owner.
- The **promote issue** is the release's issue, opened from the "Promote to
  main" issue template. Evidence for each step goes there.
- The **release record** is `docs/releases/vX.Y.Z.md`, signed, one line per
  step below. See [The signed release record](#the-signed-release-record).

## What you do, in short

1. Close the milestone. Every issue in the `vX.Y.Z` milestone is closed or
   moved to the next one, and each capability milestone's exit criteria are
   checked off on the promote issue. Moving an issue out is the owner's call.
2. On the open batch, before the audit freezes it: set `VERSION` to `X.Y.Z`
   and the `written-by:` line in `deploy/config.example.yaml` to match (a
   Go test refuses a mismatch), and refresh the release surfaces. Read `README.md` (Features, Quickstart),
   `site/index.html`, `SECURITY.md` and `docs/development.md` against the
   changelog; recapture any `docs/screenshots/*.png` whose components changed,
   from a seeded demo of this version; if the wizard's router commands
   changed, ship the cleanup for what they replace
   ([routeros-setup.md](routeros-setup.md), "Cleaning up an earlier setup").
   `make release-surfaces` names what is stale.
3. Run the pre-release audit on the frozen batch. It leaves
   `.agents/reviews/<date>-vX.Y.Z-record.md` with an `Audited-through:` line
   and its review issue on the first line, and the public review record
   `docs/reviews/<date>-vX.Y.Z.md`
   ([quality-strategy.md](quality-strategy.md), "Pre-release reviews"). No
   older review record may still carry a `pending-disclosure` marker. Close
   the review issue with its evidence, then land the batch on `dev`.
4. On a `release/vX.Y.Z` branch from `dev`, cut the changelog: rename
   `## [Unreleased]` in `CHANGELOG.md` to `## [X.Y.Z] - YYYY-MM-DD` (today)
   and put a new, empty `## [Unreleased]` above it. Commit that, then write
   and sign the release record (below) in a second commit. Merge the branch
   into `dev` by merge request, without squashing: it changes only
   `CHANGELOG.md` and `docs/releases/`, so it needs no audit, but its
   pipeline must pass `private-details` and `security:gitleaks`. Then fetch
   and run the readiness check for `preview` (below) on `dev`.
5. Promote `dev` to `preview`: open the merge request only when the readiness
   check ends "Ready". Its pipeline runs `release_readiness` first, then the
   preview bar (all three browser engines, `perf:promotion`, the upgrade-fixture
   recorder). The owner approves; merge it when the pipeline is green.
6. Check the preview was published. The push pipeline on `preview` must pass
   `publish:image`, `sign:release-digest`, `publish:channel` and
   `publish:ghcr`, which puts `preview-<short sha>` (and `preview`) on GHCR,
   signed; then GitHub's `publish-from-gitlab` run must verify that GHCR
   digest and attest it. Link both runs on the promote issue.
7. Try the preview. Deploy that digest the way an operator would
   ([install.md](install.md)), upgrade to it from the last release
   ([upgrades.md](upgrades.md)), and do every operator step named on the
   promote issue (host scripts, config, secrets). Record what you saw on the
   promote issue. Only the owner waives this step.
8. Run the readiness check for `main` on `preview`, then promote `preview` to
   `main`. The merge request changes no file: `main` gets exactly the files
   tested on `preview`. Its reviewer checks the changelog section against the
   issues merged since the last release. The owner approves; merge it when the
   pipeline is green.
9. Tag the release, only on the owner's explicit instruction for this
   release: `git tag vX.Y.Z <main's merge commit>` and
   `git push gitlab vX.Y.Z`. The tag pipeline checks the tag equals
   `v<VERSION>` and the binary reports it (`release:check-version`), then
   publishes and signs, pushes the signed digest to GHCR as `vX.Y.Z`
   (moving `latest` when it is the highest release; `publish:ghcr`), and
   mirrors the tag to GitHub, whose `release-on-tag` run verifies that GHCR
   digest and attests it. A failed
   release is re-run on the same tag once the cause is fixed; nothing is
   deleted or renumbered.
10. The owner countersigns the tag's digest with the `countersign.yml`
    workflow on GitHub ([release-signing.md](release-signing.md)).
11. Check the upgrade fixture. `record:upgrade-fixture` on the tag pipeline
    may fail without turning the tag red. If it did, fix the cause and record
    the fixture with the command `scripts/check-upgrade-fixtures-recorded.sh`
    prints.
12. Back-merge `main` into `dev` by merge request (`main` -> `preview` is
    refused; the next `dev` -> `preview` carries it). Without it the next
    promotion fails the readiness check. Then, on a new `release/vX.Y.Z`
    branch, replace the record's `after-merge` lines with their evidence and
    sign it again, and close the promote issue and the milestone.

The rest of this guide says what is checked for you and what is not.

## The readiness check

Promotions used to fail late on mistakes the repository could have shown
before the long pipeline ran: a changelog still under `[Unreleased]`, a
`VERSION` nobody bumped, a stale screenshot (#1504). The `release_readiness`
job runs first on every merge request from `dev` into `preview` and from
`preview` into `main`, and nothing else in that pipeline starts until it is
green. It runs every check, does not stop at the first failure, and ends with
the list of every failure and what to do about it. A green job never
replaces the owner's approval.

Run the same checks yourself before opening either merge request, on a clean
checkout of the source branch:

```sh
git fetch --tags gitlab dev preview main
git switch --detach gitlab/dev            # gitlab/preview for the main leg
GITLAB_TOKEN=... python3 scripts/release-readiness.py --target preview
```

`--target main` for the second leg. `GITLAB_TOKEN` is a `read_api` token, set
in the environment, never on the command line; the checks that read issues
and pipelines fail without it. `make release-readiness TARGET=preview` runs the
same thing. The list of checks, what each reads and the message it prints is
[releases/readiness-checks.md](releases/readiness-checks.md).

## The signed release record

Running the checks is not proof that this guide was read, so the release
agent signs a record for each release. Copy
[releases/TEMPLATE.md](releases/TEMPLATE.md) to `docs/releases/vX.Y.Z.md` and
fill it in:

- `version:` the release, `vX.Y.Z`, matching `VERSION`.
- `guide-blob:` this guide's git blob id,
  `git rev-parse HEAD:docs/releasing.md`. Any later edit to this guide makes
  the record stale: read the guide again and write a new record.
- `guide-commit:` the last commit that changed this guide,
  `git log -1 --format=%H -- docs/releasing.md`, for people to read.
- `promote-issue:` the promote issue, `#N`.
- One `step N:` line for every numbered step above, in order, each one of
  `done: <link, commit or job id>`, `not-needed: <the owner decision that
  says so>`, or `after-merge` for a step that comes after the promotion.
- `Signed-off-by: <name>`: a name only, no email, equal to the commit's
  author name. Commit with `git commit --trailer "Signed-off-by: <name>"`,
  which adds the same line as a trailer (`git commit -s` would add an email,
  which the private-details check refuses).

The record is written once, before step 5, and the `main` leg reads the same
record from `preview`. The evidence for the `after-merge` steps goes on the
promote issue as you go, and into the record at step 12.

## What the job cannot check

These stay manual. The record names each one's evidence:

- Milestone exit criteria (step 1): the release agent; moving an issue out of
  the milestone is the owner's decision. The job only sees open issues.
- Whether the release surfaces say true things (step 2): the release agent.
  The job only sees that they are not stale.
- The audit itself (step 3): the auditing agents, on a session the owner
  opens. The job only sees its record and the closed review issue.
- Approving the promotion merge requests into `preview` and `main` (steps 5
  and 8): the owner.
- Trying the preview (step 7): the release agent; only the owner waives it.
- Checking the changelog against the merged issues (step 8): the `main` merge
  request's reviewer.
- Making the tag (step 9): the owner, or the release agent on the owner's
  explicit instruction for that release.
- The countersignature (step 10): the owner.
- The GitHub side (steps 6 and 9): the release agent reads the runs. The
  mirror, the GitHub secrets and the registry cleanup policy are the owner's
  ([release-signing.md](release-signing.md)).
- The back-merge and closing the issue and milestone (step 12): the release
  agent.
- Owner-side setup the repository cannot see: the `mikroview-signing` runner
  and its key, protected branches and tags, and the CI/CD variables: the
  owner.
