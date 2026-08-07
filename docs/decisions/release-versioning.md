# Releases: rebuild from main, and re-earn the property that loses

Date: 2026-08-07.

## The problem

The version baked into the binary is set once, at build time. Preview
builds it; `main` promoted by retagging that exact digest, never
rebuilding. So a promoted image's binary honestly reported
`preview:<sha>` — it *is* that artefact — and there was no way for it to
say `v1.2.3` without being a different artefact.

Owner decision: building the container on the way into `main` is
acceptable in order to get a version-tagged binary.

## What rebuilding costs, and how it's paid back

Promote-by-retag existed for a reason: **the exact bytes that were
smoke-tested are the bytes that ship.** A rebuild produces different
bytes, which were never tested in container form. Rebuilding without
addressing that would trade a real property for a cosmetic one.

So the release job **smoke-tests the image it just built, before
`latest` moves to it**. The `v<x.y.z>` tag is pushed first, the
healthcheck runs against that exact image, and only then is `latest`
retagged. The property is preserved; it is just re-earned on the
release artefact instead of inherited from preview's.

## Where the version comes from

A `VERSION` file at the repo root, plain semver (`0.1.0`), no `v`.

Not a git tag pushed after the fact, because the release then depends on
someone remembering a second manual step. Not derived from the branch,
because there is nothing there to derive. A file means **the version
bump is part of the promotion PR's diff** — visible, reviewable, and
impossible to do by accident.

`v` is prefixed for the image tag and the git tag, so the file says
`0.1.0` and the world sees `v0.1.0`.

Starting at `0.1.0`: there are no releases yet, and pre-1.0 is an honest
statement about a project still changing shape weekly.

## What each merge to main does

The release job is keyed on **whether the tag `v<VERSION>` already
exists**:

- **VERSION bumped** (no such tag): build with `VERSION=v<x.y.z>`,
  publish `v<x.y.z>`, smoke-test it, move `latest`, sign, attest, and
  create the git tag.
- **VERSION unchanged** (tag exists): not a release. The existing
  promote job retags preview's tested digest to `latest`, exactly as
  before.

That check is what stops a docs fix merged to `main` republishing
`latest` from identical source under a new digest, and makes the whole
thing idempotent if a workflow is re-run.

## What this does not change

The preview lane is untouched: it still builds, publishes, signs and
smoke-tests every release candidate, and it remains where changes are
proven before they go near `main`.
