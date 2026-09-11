# Releases: rebuild from main, and re-earn the property that loses

Date: 2026-08-07. **Amended 2026-09-10** — see "The tag is the trigger" at
the end: a release is now a `v*` tag made on GitLab, not a merge to `main`
whose `VERSION` has no tag yet. The rebuild, the smoke test and the
`VERSION` file all stand.

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

## The tag is the trigger (2026-09-10)

Owner decision on #572, the first release after development moved to
GitLab (#935): GitLab is the source of truth, so the release tag is made
there and GitHub reacts to it. Under the earlier rule the GitHub workflow
minted the tag itself, as its last step, and GitLab would never have held
it at all.

What changed:

- `sync:mirror-to-github` pushes `v*` tags as well as the three branches.
  The tag must be protected on GitLab (`v*`), or the protected deploy-key
  variable is empty and the push fails on its first check.
- `docker.yml` runs its release job on a `v*` tag push, and refuses unless
  the tag equals `v<VERSION>` at the tagged commit. A merge to `main` now
  only retags preview's tested digest as `latest`; it never releases.
- A failed release is re-run on the same tag once the cause is fixed.
  Nothing is deleted or renumbered, because the tag never claimed the
  publish had happened — the registry does.

What did not change: the `VERSION` file is still where the version lives,
and the bump is still part of the promotion diff. "Not a git tag pushed
after the fact" above was about *deriving* the version from a tag; the
tag now says when, the file still says what, and the two must agree.

Release steps, in order: promote `preview` → `main`; wait for the
promote job on GitHub; on GitLab, `git tag v<VERSION> <main sha>` and
push it to `gitlab`; watch the tag pipeline's mirror job, then the
`docker` run on GitHub; back-merge `main` → `preview` → `dev`.

## Release surfaces (2026-09-10)

The public surfaces ordinary development never touches — `README.md`,
`site/index.html` (GitHub Pages), `docs/screenshots/`, `SECURITY.md`,
`CONTRIBUTING.md` — are refreshed as part of every release, not left
to chance. Two pieces of structure hold that: the "Promote to main"
issue template (`.gitlab/issue_templates/`) carries the checklist, and
`scripts/check-release-surfaces.sh` (CI job `policy:release-surfaces`,
on every merge request into `preview` or `main`; locally `make
release-surfaces`) refuses a promotion whose surfaces are stale:
changelog heading missing for `VERSION`, links to the switched-off
GitHub issue tracker, relative links to files that do not exist, a
`docs/*.md` file linked from neither README nor CONTRIBUTING, a Go
version in CONTRIBUTING that does not match `go.mod`, or a referenced
screenshot whose last change predates the previous `v*` tag while
`frontend/src` changed since it. GitHub Pages redeploys only when
`site/` or the screenshots change, so a release with no such change
leaves the site as it was, by design. It also checks that
`docs/reviews/` has a record for the version being cut, and that no
older record still carries the literal marker `<!-- pending-disclosure:
#n #m -->` -- a review may defer naming its security findings with that
line, but the line must be replaced by the real findings before the
next release.
