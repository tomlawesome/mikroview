# Delivery on GitLab, with GitHub as the mirror

Reference for how MikroView is developed and shipped. `AGENTS.md` keeps the
rules (one writer per branch, `#N` versus `!N`, where issues live); this
holds the mechanics and the history behind them. Moved out of `AGENTS.md`
2026-10-10.

## Where development happens

Owner decision, 2026-09-04, tracked on #935: MikroView is developed on the
self-hosted GitLab instance, project `ai/mikroview` (project id 53), remote
name `gitlab`. Branches, merge requests and the `dev` -> `preview` -> `main`
promotions all happen there; full CI is `.gitlab-ci.yml` at the repo root.
GitHub (`origin`) is a read-only mirror of those three branches, and keeps
what already works there: CodeQL, the container registry and cosign signing,
Pages, secret scanning, and release tags. It runs no tests; they
moved to GitLab under #942.

Why: the owner's runner hardware is faster and never queued; the project
keeps moving on days GitHub does not; checks that need Docker on the host
are easy to wire in here; and GitHub still serves the public repo, its
history and registry pulls, so nothing that depends on those breaks.

## One branch has one writer

Never push to `origin`'s `dev`, `preview` or `main`, and never open or merge
a GitHub pull request into them -- a merge on that side forks the branch,
the mirror push is refused, and GitHub silently falls behind. Feature
branches are pushed to `gitlab`; merge requests are opened there
(`glab mr create`, with the `Cut:` line in `-d`).

## Mirror mechanics

After a merge, `sync:mirror-to-github` in `.gitlab-ci.yml` pushes the branch
to GitHub over SSH with a deploy key (one repository, push only --
deliberately not a token, so a leak of it cannot reach anything else). It
runs only when the `MIRROR_TO_GITHUB` CI/CD variable is `true`. A release is
a protected `v*` tag pushed to `gitlab`: the same job mirrors it, and
GitHub's `docker` workflow publishes from it
([release-versioning](decisions/release-versioning.md), "The tag is the
trigger"). `policy:promotion-hop` fails a merge request into `preview`
unless it comes from `dev`, and into `main` unless it comes from `preview`.

## Credentials (names only; values are never written down)

- The agent's `glab` config lives in `~/.config/glab-claude/`
  (`GLAB_CONFIG_DIR`); git over HTTPS to `gitlab` uses that same token
  through `GIT_ASKPASS`.
- `scripts/gate-local.sh` uses a read-only deploy token in
  `~/.config/mikroview/gitlab-credentials`.
- GitLab CI/CD variables: `GITHUB_MIRROR_SSH_KEY` (file, protected),
  `MIRROR_TO_GITHUB` (protected) and `GITLAB_MR_TOKEN`.

## Issues and merge requests

Issues, planning and decisions live on GitLab too (owner decision,
2026-09-04, on #935): the whole GitHub tracker was imported here, keeping
every issue and merge request number, and Issues is switched off on the
GitHub mirror (owner, 2026-09-05, #935; reaffirmed 2026-09-06: GitHub is a
mirror and nothing more). The mirror takes no pull requests either, so
nobody should raise issues there. #953's public-intake arrangement is
withdrawn; there is nothing to check on GitHub at session start. GitHub's
copies of issues are closed and read-only history.

GitLab counts issues and merge requests separately where GitHub shared one
sequence, so an issue and a merge request can carry the same number and mean
different things: always write `#941` or `!941`.

`Closes #N` goes in the merge request description as well as the commit
message. GitLab closes from either, but only on a merge into the default
branch -- which here is `dev`, so an ordinary merge closes its issue with no
extra job. Check the issue after every merge anyway.

`.gitlab/issue_templates/Work item.md` puts the current plan at the top for
new issues; existing issues get fixed as they are picked up. The reason:
on #97 a `tar.gz` design had been dropped in favour of a gzipped JSON
envelope, with the reasoning in a comment, and the tar design was later
picked back up as though it were still the plan, because the body still said
so. #162 was the same failure the other way: its body described a design
that had been implemented and then removed.

## A GitHub/GitLab environment difference worth knowing

[gitlab-ci-root-in-container-test-failure](decisions/gitlab-ci-root-in-container-test-failure.md)
is a concrete example of an environment difference that broke a test for a
reason that had nothing to do with the code. Check for similar defaults
mismatches before assuming a GitLab-only failure is a real regression.

## Dependency pins and the TypeScript hold

Renovate (`renovate.json`) watches gomod, npm, dockerfile, gitlabci and the
GitHub Actions pins, and groups the Go toolchain so its pins move together.
Staying back on a version needs a specific, defensible technical reason,
and a blocked upgrade needs three things written down: what blocks it, what
the version behind costs, and the concrete trigger for taking it.

`typescript` is held at `~6.0.2` (and `allowedVersions: "<7.0.0"` in
`renovate.json`) because `svelte-check` supports TypeScript 7 only behind an
experimental flag, under which it silently type-checks 46 files instead of
249 (#286 has the measurement and the trigger). The upgrade was rejected
because taking it would have made a *check* quieter while still reporting
success.

Base images are pinned by tag, not digest:
[base-images-on-tags](decisions/base-images-on-tags.md).
