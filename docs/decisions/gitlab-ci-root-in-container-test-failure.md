# `test:go` fails on GitLab CI: root-in-container vs. mikroview's own security posture

**Status:** Resolved. Confirmed CI-environment-only (mikroview's real container
already runs `USER nonroot:nonroot`, so this wasn't masking a production gap)
— fixed by running `test:go` as a non-root user inside the container. See the
"Decision needed" section below for the reasoning; option 1 was applied.

## Context

A new GitLab CI pipeline (`.gitlab-ci.yml`, root of this repo) now gates merges
into `dev` on a self-hosted GitLab instance (`gitlab.tomlawson.io`, project
`ai/mikroview`), running alongside the existing, untouched GitHub Actions
`dev`/`preview`/`main` flow. First real pipeline run
(https://gitlab.tomlawson.io/ai/mikroview/-/pipelines/7) got through `lint:go`
and `lint:frontend` cleanly, then `test:go` failed on one test:

```
--- FAIL: TestUnwritableStorePathSurfacesPersistErrButStillLoads (0.02s)
    servertls_test.go:178: expected a non-nil persistErr for an unwritable store path
FAIL	github.com/tomlawesome/mikroview/internal/servertls	0.323s
```

Full test suite otherwise passed (every other package `ok`, coverage numbers
all present) — this is not a flake, dependency problem, or CI-plumbing issue.

## Working hypothesis: root-in-container, not a real regression

The GitLab `test:go` job runs `go test ./...` inside the stock `golang:1.26`
Docker image via a rootless-Docker-executor runner. That image's default
process user is **root inside the container**. Root has `CAP_DAC_OVERRIDE` and
bypasses normal Unix permission-bit enforcement — so a test that `chmod`s a
directory to make it unwritable, expecting the code under test to hit a real
permission error, won't actually see one when the test process itself is
root.

GitHub's existing `ci.yml` `test` job runs the same test directly on the
`ubuntu-latest` runner VM via `actions/setup-go` — **not inside a container**
— as an unprivileged user, where the chmod actually takes effect and the test
has presumably always passed there.

Checked mikroview's actual `Dockerfile`: the real production image is built
`FROM gcr.io/distroless/static-debian12:nonroot` with `USER nonroot:nonroot`
(lines 35/38) — production mikroview never runs as root. So this looks like a
pure CI-environment artifact from picking a plain root-defaulting build image
for the GitLab test job, not a hole the test was accidentally covering for in
a real root deployment.

**Not yet confirmed, worth double-checking rather than assuming:** that
`TestUnwritableStorePathSurfacesPersistErrButStillLoads` and any sibling
permission-dependent tests don't have some other purpose (e.g. simulating a
misconfigured *volume mount* or *external storage* being unwritable, a
scenario that could still apply under a non-root production container too,
independent of who's running the test process). Read `servertls_test.go`
around line 178 and whatever `persistErr` covers in `internal/persist` before
concluding this is purely a test-harness identity issue.

## Decision needed

Two non-exclusive options:

1. **Fix the CI job**, not the test: make `test:go` in `.gitlab-ci.yml` run as
   a non-root user inside the `golang:1.26` container, matching both GitHub's
   existing behavior and mikroview's own real production posture. Sketch (not
   verified/tested):
   ```yaml
   script:
     - useradd -m -u 10001 ci-test
     - chown -R ci-test:ci-test "$CI_PROJECT_DIR"
     - export HOME=/home/ci-test
     - su ci-test -c "cd $CI_PROJECT_DIR && go test ./... -race -coverprofile=coverage.out"
   ```
   Caveats to check: `$GOPATH`/`$GOCACHE` will default under the new `$HOME`
   (fresh, no conflict with root-owned caches from earlier steps) — but verify
   nothing earlier in the job wrote root-owned files into `$CI_PROJECT_DIR`
   that `ci-test` then can't touch. Simpler alternative: find/use a non-root
   Go build image if one exists that still matches the pinned `1.26` version.

2. **Confirm this is purely environmental and stop there** if reading the
   test confirms it's specifically about process-identity permission
   enforcement (not volume/storage-misconfiguration semantics) — in which
   case fixing the CI job alone is sufficient and no mikroview code changes
   are needed.

Per this repo's own `AGENTS.md` conventions: reproduce/read before acting, and
if a decision changes what's being tracked, record it (new issue or edit to
whichever issue this ends up filed under) rather than leaving it only in this
doc.

## Relevant files

- `.gitlab-ci.yml` — `test:go` job, root of repo
- `internal/servertls/servertls_test.go:178` — the failing test
- `Dockerfile` — confirms production runs `USER nonroot:nonroot`
- Pipeline run: https://gitlab.tomlawson.io/ai/mikroview/-/pipelines/7
