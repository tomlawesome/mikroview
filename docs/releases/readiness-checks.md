# Release readiness checks: specification (#1504)

What `scripts/release-readiness.py` and the `release_readiness` CI job must
do. The guide that uses them is [docs/releasing.md](../releasing.md); its
numbered steps are the "guide steps" below. The model is ai/orbit#1860; this
is mikroview's own list, worked out from its guide and its failed promotions.

## The command

```sh
python3 scripts/release-readiness.py --target preview|main [--remote NAME]
```

- `--target` is required: `preview` checks a promotion from `dev`, `main` a
  promotion from `preview`. The **source branch** is `dev` or `preview`, the
  **target branch** `preview` or `main`.
- `--remote` names the git remote holding the branches and tags: `gitlab` by
  default (the name the guide uses locally), `origin` in CI.
- The **source commit** is `HEAD`. Every file the checks read is read from
  the source commit (`git show <sha>:<path>`), never the working tree, except
  check 6, which runs an existing script on the working tree (check 1 makes
  that the same thing).
- The script never fetches. The caller fetches the three branches and the
  tags first (the guide says how; the CI job does it in its script).
- GitLab API: base URL from `CI_API_V4_URL`, else
  `https://gitlab.tomlawson.io/api/v4`; project from `CI_PROJECT_ID`, else
  `ai%2Fmikroview`. Token from `CI_REUSE_TOKEN` if set, else `GITLAB_TOKEN`,
  sent as the `PRIVATE-TOKEN` header. The token never appears in output.
- Go's release list is `https://go.dev/dl/?mode=json`, or
  `RELEASE_READINESS_GO_DL_URL` when set (for tests).
- `make release-readiness TARGET=preview` runs the same command.

Language: Python 3, standard library only, like
`scripts/ci-changed-paths-guard.py` and `scripts/coverage-floor.py`. It runs
in `alpine:3.24` with `git` and `python3` added, the image and packages
`lint:changed-paths-guard` already uses, so no new pin. Its tests are
`scripts/release-readiness.test.sh` (bash, as the other `*.test.sh`), run by
`lint:script-tests`.

## How it reports

Every check runs, in the order below, whatever the earlier ones found. A
check that cannot run (an API error, a missing file, an unexpected
exception) fails with `could not run: <reason>` as its message; it never
stops the run and never passes. Output, one line per check, then a summary:

```
release-readiness: target preview, source dev at <sha12>, version <X.Y.Z>
ok: <id>: <what passed>
FAIL: <id>: <what is wrong> -- <what to do>
...
release-readiness: Ready (12 of 12 checks passed)
```

or, when any check failed, the failures again in one block so the list is
complete at the end of the log:

```
release-readiness: 2 of 12 checks failed:
  FAIL: <id>: ...
  FAIL: <id>: ...
```

Exit 0 when every check passed, 1 when any failed, 2 for a usage error (no
or bad `--target`, not inside a git work tree), with no checks run.

`<version>` below is `VERSION` at the source commit, trimmed; when it is not
`X.Y.Z` (check 3 fails), checks that need it fail with
`could not run: VERSION is not X.Y.Z`. `{...}` in a message is filled in at run
time.

## The checks

Both legs run all twelve. Where a check reads differently per leg it says so.

### 1. `source-tip`

- Reads: `HEAD`, `<remote>/<source branch>`, `git status --porcelain
  --untracked-files=no`.
- Passes: `HEAD` equals the remote source branch tip and no tracked file is
  modified.
- Fails: either is false.
- Message: `FAIL: source-tip: HEAD {sha12} is not {remote}/{source} ({tip12}), or tracked files are modified -- fetch, then check out {remote}/{source} cleanly and run again`
- Past failure: none seen. It keeps a local run from checking a stale or
  edited checkout.

### 2. `contains-target`

- Reads: `<remote>/<target branch>`.
- Passes: the target tip is an ancestor of the source commit
  (`git merge-base --is-ancestor`).
- Fails: it is not. The project merges with rebase (`rebase_merge`), so
  GitLab would refuse the merge as needing a rebase of a protected branch.
- Message, `preview` leg: `FAIL: contains-target: dev does not contain preview's tip {tip12} -- the main -> dev back-merge of the last release is missing; merge it first (docs/releasing.md step 12)`
- Message, `main` leg: `FAIL: contains-target: preview does not contain main's tip {tip12} -- main has a commit preview lacks; back-merge main -> dev and promote dev -> preview again`
- Past failure: none directly; v0.5.1 needed an extra `dev` -> `preview`
  promotion (!1032) to unblock its back-merge.

### 3. `version`

- Reads: `VERSION`; tags `v*` on the remote (`git ls-remote --tags`).
- Passes: `VERSION` is one line `X.Y.Z` (digits only) and is higher, by
  numeric comparison, than every `vX.Y.Z` tag.
- Fails: wrong shape, or not higher than the newest tag.
- Message: `FAIL: version: VERSION is '{value}', not above the newest tag {tag} -- bump VERSION on the batch before the audit (docs/releasing.md step 2)`
- Past failure: v0.4.0. The first `dev` -> `preview` cut (!602) went without
  the bump and needed a third promotion (!607); a `main` merge with an
  unchanged `VERSION` would have retagged the old digest instead of
  releasing.

### 4. `tag-free`

- Reads: the remote's tags.
- Passes: no tag `v<version>` exists.
- Fails: it does.
- Message: `FAIL: tag-free: tag v{version} already exists -- this version is released; bump VERSION for a new release`
- Past failure: none seen.

### 5. `changelog`

- Reads: `CHANGELOG.md`.
- Passes: there is a heading `## [<version>] - YYYY-MM-DD` with a real
  calendar date, and at least one line starting `- ` between it and the next
  `## ` heading; and the `## [Unreleased]` section, if present, has no line
  starting `- ` or `### ` before the next `## ` heading.
- Fails: no such heading, a bad date, an empty section, or entries still under
  `[Unreleased]`. One FAIL line names every problem found.
- Message: `FAIL: changelog: {problems} -- rename '## [Unreleased]' to '## [{version}] - YYYY-MM-DD' and add an empty '## [Unreleased]' above it, on the release branch (docs/releasing.md step 4)`
- Past failure: v0.4.0. The notes were still under `[Unreleased]` and missing
  three merged changes when `dev` went to `preview` (!602), so a second
  promotion (!604) carried them.

### 6. `release-surfaces`

- Reads: runs `sh scripts/check-release-surfaces.sh` from the repository root
  (it reads the working tree; check 1 makes that the source commit).
- Passes: it exits 0.
- Fails: it exits non-zero. Each of its `FAIL:` lines is repeated in this
  check's message, in order, so the summary carries them.
- Message: `FAIL: release-surfaces: {n} surface check(s) failed: {each FAIL line, joined with '; '} -- fix each as it says, on the batch; make release-surfaces runs it alone`
- Past failure: v0.6.0, pipeline 1349 on !1071: `policy:release-surfaces`
  found `docs/screenshots/stream-dark.png` stale. That job already ran early;
  what changes is that nothing else starts while it is red.
  `policy:release-surfaces` stays as it is.

### 7. `release-audit`

- Reads: `.agents/reviews/*-v<version>-record.md`.
- Passes: exactly one such file exists, it has a line
  `Audited-through: <40-hex sha>`, and that commit is an ancestor of the
  source commit.
- Fails: no file, more than one, no marker, or the marker is not in the
  source branch's history.
- Message: `FAIL: release-audit: {problem} -- the pre-release audit for v{version} must land on dev with its record before promoting (docs/releasing.md step 3)`
- Past failure: none in a promotion pipeline. v0.7.0's promotion (#1355) is
  held until its audit (#1426) is done; this makes that a check.

### 8. `review-issue`

- Reads: the first line of the check 7 record, which names its review issue
  (`# v0.7.0 audit record, #1426`: the first `#<digits>` on the line); the
  GitLab issue by API.
- Passes: the issue's `state` is `closed`.
- Fails: no issue number on the first line, the issue is open, or no token.
- Message: `FAIL: review-issue: {problem} -- close the review issue #{n} with its evidence before promoting (docs/releasing.md step 3)`; with no
  token: `FAIL: review-issue: could not run: no GitLab token -- set GITLAB_TOKEN to a read_api token (CI: CI_REUSE_TOKEN)`
- Past failure: none seen. It is #1355's step 2 ("its own review issue closed
  with evidence").

### 9. `milestone`

- Reads: `promote-issue:` from the release record (check 12's file); the
  GitLab milestone titled `v<version>` and its open issues, by API.
- Passes: the milestone exists and its only open issue, if any, is the
  promote issue. With no record or no `promote-issue:` line, every open
  issue counts.
- Fails: no milestone, or any other open issue in it. The message lists up to
  ten open issue numbers and the count.
- Message: `FAIL: milestone: v{version} has {n} open issue(s) besides the promote issue: {list} -- close them, or have the owner move them to the next milestone (docs/releasing.md step 1)`
- Past failure: none seen. It is #1355's step 1.

### 10. `source-pipeline`

- Reads: GitLab pipelines by API, `sha=<source commit>`,
  `ref=<source branch>`, `source=push`, newest first.
- Passes: the newest such pipeline's status is `success`. On the `main` leg
  that is `preview`'s publishing pipeline, so `publish:image`,
  `sign:release-digest` and `publish:channel` passed for the commit being
  promoted.
- Fails: none found, or the newest is anything but `success` (running counts
  as a fail, with its own message).
- Message: `FAIL: source-pipeline: the {source} push pipeline for {sha12} is {status} ({web_url}) -- wait for it, or fix it and push again, before promoting`
- Past failure: none seen as a promotion failure. It also covers
  `check:upgrade-fixture-recorded`, which runs only on `dev` pushes and
  would have flagged v0.6.1's missing fixture after its tag (pipeline 1671).

### 11. `go-toolchain`

- Reads: `Dockerfile`'s `FROM golang:<X.Y.Z>-alpine` line;
  `https://go.dev/dl/?mode=json` (Go's list of supported releases; not the
  module proxy).
- Passes: the list has a stable release for the same `X.Y` and it is
  `go<X.Y.Z>`.
- Fails: a newer patch of `X.Y` exists, `X.Y` is no longer listed
  (out of support), or the page cannot be read.
- Message: `FAIL: go-toolchain: the image builds with go{have}, newest is go{want} -- bump Go on the batch everywhere lint:supply-chain-pins checks (Dockerfile, .gitlab-ci.yml, supply-chain/pins-policy.json)`
- Past failure: v0.7.0 (#1355, 2026-11-13): `main` shipped go1.27.3, which
  has GO-2026-6859; nothing checked the patch level at promotion.

### 12. `release-record`

- Reads: `docs/releases/v<version>.md`; `docs/releasing.md`; the last commit
  that changed the record (`git log -1 --format=%H <source> --
  docs/releases/v<version>.md`), its author name and its `Signed-off-by`
  trailers (`git log -1 --format='%(trailers:key=Signed-off-by,valueonly)'`).
- Passes, all of:
  - the file exists;
  - `version:` is `v<version>`;
  - `guide-blob:` equals `git rev-parse <source>:docs/releasing.md`;
  - `guide-commit:` and `promote-issue:` (`#<digits>`) are present;
  - its `step N:` lines are exactly 1 to N in order, N being the number of
    numbered steps in the guide's "What you do, in short" section (lines
    starting `<digits>. ` at column 0 between that heading and the next `## `);
  - each step's value is `done: <text>`, `not-needed: <text>` or
    `after-merge`, the text non-empty and without `<` or `>`;
  - a `Signed-off-by: <name>` line whose name has no `@`, `<` or `>`, equals
    the commit's author name, and is one of that commit's `Signed-off-by`
    trailers.
- Fails: any of these. One FAIL line names every problem found.
- Message: `FAIL: release-record: {problems} -- {fix}`, where `{fix}` is the
  first that applies:
  - missing: `copy docs/releases/TEMPLATE.md to docs/releases/v{version}.md and fill it in (docs/releasing.md step 4)`
  - guide-blob differs: `guide changed; re-read docs/releasing.md and write a new record`
  - unsigned or the name differs: `commit the record with git commit --trailer "Signed-off-by: <your name>", the name only, and do not squash its merge request`
  - otherwise: `fix the record to match docs/releasing.md "The signed release record"`
- Past failure: none; it is new with #1504. On the `main` leg it reads the
  same record from `preview`, as the promotion into `main` changes no file.

## Which past failures it covers

Promotion merge requests into `preview` and `main` since v0.4.0, and the
release pipelines after them:

| Release | What failed | Check |
| --- | --- | --- |
| v0.4.0 | changelog still `[Unreleased]`, entries missing (!602, fixed by !604) | 5 `changelog` |
| v0.4.0 | `VERSION` not bumped (fixed by !607) | 3 `version` |
| v0.5.0 | !980: `perf:promotion` on a busy host (12 pipelines), scenario shards, `test:go` (`TestHandleFlagsList`), `security:gitleaks` (2 findings), `security:semgrep` | none: test and scanner results on the build itself |
| v0.5.0 | !1021: `perf:promotion` | none |
| v0.6.0 | !1071 pipeline 1349: stale screenshot | 6 `release-surfaces` (already caught early by `policy:release-surfaces`) |
| v0.6.0, v0.6.1 | tag pipelines 1364 and 1671: `record:upgrade-fixture` | none at promotion; `check:upgrade-fixture-recorder` on the `preview` merge request has covered it since 2026-09-27 |
| v0.6.1 | !1096: `gate:scenarios:firefox 4/4`, `perf:promotion` timeout | none |
| v0.7.0 | go1.27.3 with GO-2026-6859 on `main` (#1355) | 11 `go-toolchain` |

Most past promotion failures were tests and scans on the build itself, which
a readiness check cannot run sooner. What it does change for those: they no
longer start on a promotion whose release state was already wrong.

## The CI job, and how everything else waits on it

The job is `release_readiness`, the name the global delivery rule uses, so it
reads the same in every project:

- a new stage `readiness`, first in `stages:`, holding only this job;
- `tags: [light]`, `image: alpine:3.24`, `needs: []`,
  `interruptible: false`, `timeout: 10m`, `GIT_DEPTH: 0` (check 12 reads the
  record's commit, check 2 the target's history);
- rules: the `.dev-landing` and `.catchup-check` job rules first, as every job
  here has, then only a merge request from `dev` into `preview` or from
  `preview` into `main`; nothing else;
- `before_script`: `apk add --no-cache git python3`, and
  `git config --global --add safe.directory "$CI_PROJECT_DIR"`;
- script: fetch the source and target branches and the tags from `origin`
  into `refs/remotes/origin/*`, check out `$CI_COMMIT_SHA` detached, then
  `python3 scripts/release-readiness.py --target "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME" --remote origin`.
  `$CI_COMMIT_SHA` is the source tip because merged-results pipelines are
  not in use; if they are ever turned on, check out
  `$CI_MERGE_REQUEST_SOURCE_BRANCH_SHA` instead;
- it reads `CI_REUSE_TOKEN`, the `read_api` token the reuse gate already
  uses.

Every other job in a promotion pipeline waits on it:

- Jobs with no `needs:` wait by stage order, because `readiness` is the first
  stage and a failed stage stops the later ones.
- Jobs with `needs: []` start at once whatever their stage, so each gets
  `needs: [{job: release_readiness, optional: true}]` instead. `optional`
  keeps them starting at once in every pipeline that has no readiness job
  (pushes, ordinary merge requests). Today that is: `policy:promotion-hop`,
  `policy:release-surfaces`, `lint:changed-paths-guard`, `lint:flake-check`,
  `private-details`, `lint:script-tests`, `lint:shellcheck`, `lint:golangci`,
  `lint:ruff`, `lint:knip`, `lint:jscpd`, `build:image`, `gate:image`,
  `security:zizmor`, `sync:mirror-to-github`,
  `check:upgrade-fixture-recorded`, `chr-exercise:run`, `chr-watch:run`,
  `renovate:run` (the last five never run on a merge request; giving them the
  same need keeps one rule with no exceptions).
- Jobs whose `needs:` names other jobs wait through them: everything built on
  `build:image` waits because `build:image` does.
- No exceptions: the static checks wait too. The job takes well under a
  minute.

The merge needs a green pipeline (the project's "pipelines must succeed"
setting is on), so a red readiness job blocks the merge.

`scripts/release-readiness.test.sh` also checks this wiring from
`.gitlab-ci.yml` (with `py3-yaml`, as `ci-changed-paths-guard.py` does): it
fails naming any job whose rules can match a promotion merge request and
that neither sits in a later stage with no `needs:`, nor lists
`release_readiness` in `needs:`, nor needs only jobs that wait. A new job
added later with `needs: []` and no readiness need turns that test red.

## What the tests must cover

For the test writer, from this specification alone:

- each check passing, and each failing for each reason listed, with its
  exact message, against a throwaway git repository with a bare remote;
  API and `go.dev` answers stubbed (e.g. a local HTTP server via
  `CI_API_V4_URL`, and an override for the `go.dev` URL);
- several checks failing at once: all reported, summary block complete,
  exit 1;
- a check raising an error: `could not run`, the rest still run;
- usage errors: exit 2, no checks run;
- the token never printed;
- the CI wiring check above, against the real `.gitlab-ci.yml`.
