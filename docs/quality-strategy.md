# Quality strategy

## What stands on what (the reuse gate, #1066)

A push to a merge request used to rerun the whole pipeline, even when the
push only fixed one thing. Fixing one scenario shard's flake still ran the
other three shards, the live-check image build and the container and
Postgres tests, because nothing in `.gitlab-ci.yml` had a way to tell "this
job's answer cannot have changed" from "run it and find out."

The reuse gate (ported from Orbit's `scripts/ci/reuse-gate.sh`, orbit #898)
closes that gap for the jobs expensive enough to be worth it. It never
changes *whether* a job runs at all -- that is the docs-only lane's job
(`*docs_only_paths` / `*code_paths` in `.gitlab-ci.yml`, and AGENTS.md, "The
docs-only lane") -- only whether a job that would run repeats real work it
has already done on this same merge request.

### How it decides

`scripts/ci-reuse-inputs.js` lists, per job, the paths whose contents decide
that job's answer (`JOB_INPUTS`). At the top of its `script:`,
`scripts/ci-reuse-gate.js gate <job name>`:

1. Hashes the paths that job declares, from `git ls-tree -r HEAD` -- the
   committed tree, never the working directory, so an earlier job's own
   artefacts cannot change the answer.
2. Looks through this merge request's earlier pipelines (newest first, up
   to ten) for a successful run of the same job whose recorded evidence
   names the same hash.
3. If it finds one: prints which job and pipeline it is standing on, and
   exits 0. The `.reuse_gate` anchor in `.gitlab-ci.yml` turns that into
   `exit 0` for the whole job -- none of the real work runs.
4. If not: exits 1, and the job does the real work as before. The last
   line of its `script:` (the `.reuse_evidence` anchor) then records
   `ci-reuse-evidence/<job>.json` -- the proof this run really happened and
   on which inputs -- as an artifact the next pipeline's lookup can read
   back. A job that gated itself out, failed, or was cancelled writes no
   evidence, so "did not really run" can never be misread as "ran and
   passed."

Merge requests only. `.gitlab-ci.yml` is itself in every job's input list,
so a pipeline on `dev`, `preview` or `main`, and any change to this file,
both run everything -- delivery-branch pipelines are unchanged, by
construction rather than by a separate check.

Nothing here can fail a job. Every fault the gate meets -- an unset token,
an unreachable API, an expired evidence artefact, an unknown job name --
reads as "no earlier run to stand on," which reruns the job. That is the
only direction that can never be wrong, and it is also why this does not
adopt `workflow: auto_cancel: on_job_failure` (orbit #923's other change):
the owner wants the full pipeline to run so one push shows every failure,
not stop at the first red job.

### Jobs covered

| Job(s)                                              | Stands on a match of |
|------------------------------------------------------|-----------------------|
| `gate:scenarios 1/4` … `4/4`                         | the Go tree, `frontend/`, `scripts/`, `.gitlab-ci.yml` |
| `gate:image`, `test:container`, `test:postgres`      | the above, plus `Dockerfile` and `live-check.Dockerfile` |
| `test:install-line`                                  | the above, plus `install.sh` (#1242 -- root-level, so none of the other lists' globs see it) |
| `test:go`, `test:frontend`                           | not covered -- already cheap enough (#1066) |

The four `gate:scenarios` shards share one input set even though each only
runs a slice of the scenario list (`scripts/run-scenarios.sh`'s `plan()`):
every shard builds and runs the same live-check image over the same
binary, so a change anywhere in that set can move any shard's result.
Splitting the shards' own inputs to match `plan()`'s contiguous slices is
future work, not required by #1066 -- today a scenario-only change still
reruns all four shards, just not the rest of the pipeline.

`gate:image`, `test:container` and `test:postgres` share one combined,
deliberately over-broad list rather than three narrow ones: the first
builds `live-check.Dockerfile`, the second builds and runs the product
image (`Dockerfile`), and the third is Go-only against a real Postgres.
Too broad only costs an occasional unnecessary rerun; too narrow would
reuse a stale result, which is a correctness bug.

### The one credential this needs

`CI_REUSE_TOKEN` -- a GitLab CI/CD variable the owner creates (Settings >
CI/CD > Variables), **not** protected, **masked**:

- a project access token on `ai/mikroview` only
- scope: `read_api` (read-only; it never needs to write anything)
- role: Reporter

It must not be protected: an ordinary merge request runs on the source
branch, which is not a protected ref, and a protected variable is simply
absent there -- the gate would then always report "no token" and every job
would run, which is safe but gives up the whole feature. It is deliberately
narrower than `GITLAB_MR_TOKEN` (used elsewhere for opening merge requests):
this gate only ever reads pipelines, jobs and their artifacts, so a
dedicated read-only token limits what a leak of it could reach.

Until `CI_REUSE_TOKEN` exists, nothing changes: every candidate job runs
exactly as it did before #1066. Creating it is what turns reuse on.

## Upgrade fixtures (`testdata/upgrade/`, #1239, #1247)

Two jobs open real recordings before running their tests. `test:go`
opens one recorded data directory per released version; `test:postgres`
restores one `pg_dump` per *schema* version into its own fresh database
and opens that. See `internal/persist/upgrade_test.go`,
`internal/persist/upgrade_postgres_test.go` and `docs/upgrades.md`, "How
this is tested", for what that proves and why.

Per schema version, not per release, for the database half: the Postgres
migrations are numbered and checksummed, so two releases on the same
schema version restore the same database. That is v0.1.0 (schema 1,
`store_blob`), v0.2.0 (schema 2, `match_log`) and v0.3.0 (schema 3,
`match_log.provisional`) today. Nothing in a dump is encrypted whatever
the manifest says: #853's key encrypts what the *file* backend writes,
and the Postgres path never consults it.

What lives where:

- `testdata/upgrade/<version>/manifest.json` -- committed. What the
  recording holds (usernames, an entity, a flag, ...), never a hash,
  token or key.
- The recordings themselves -- not committed, ever: a data directory
  holds password/token hashes and a TLS key, made-up or not, and
  gitleaks would flag every one. They live in the project's generic
  package registry (`upgrade-fixtures/<version>/`, holding
  `upgrade-fixture-<version>.tar.gz`, an optional
  `upgrade-fixture-<version>-postgres.sql.gz`, and `manifest.json`), and
  `.upgrade-fixtures/` (gitignored) is where fetched copies land.
- `scripts/fetch-upgrade-fixtures.sh` -- downloads every version with a
  committed manifest *and* every version the registry holds, skipping a
  file already present. A missing Postgres dump is expected and logged,
  not a failure. `test:go` and `test:postgres` both run it first.
- `scripts/record-upgrade-fixture.sh [--postgres] <version>` -- the other
  half: boots the released image, drives
  `scripts/upgrade-fixture-session.py` against its own API, and keeps
  either the data directory (a tarball) or a `pg_dump`. Booting an old
  image happens here and nowhere else in the gate.
- `record:upgrade-fixture` in `.gitlab-ci.yml` -- runs both of those on
  every `v*` tag, after waiting up to 45 minutes for that version's
  image to appear on GHCR. `allow_failure: true`, so a slow release
  never reddens a tag pipeline; if it does fail, run the script by hand.

The registry copy of the manifest is the fallback, not the source. A job
token can upload a package but cannot push or open a merge request, so
the tag job uploads the manifest beside the recording and the tests read
the committed copy where there is one (#1247's decision, 2026-09-16).
Committing it is then a review step -- check `testdata/upgrade/v0.x.y/
manifest.json` holds no secret and commit it -- rather than something
the gate waits for.

## Pre-release reviews (`docs/reviews/`)

Each release gets a code review before it is cut: cheap finder agents per
area and topic (security, quality, efficiency, robustness, performance),
an independent refuter per medium/high finding, every confirmed finding
fixed before the tag. The record is `docs/reviews/<date>-<version>.md`,
kept forever: it names the commit reviewed, so the next release reviews
only what changed since, plus new code. Security findings are listed there
only once fixed robustly; anything open lives in a confidential issue.
First one: `docs/reviews/2026-09-10-v0.5.0.md`.
