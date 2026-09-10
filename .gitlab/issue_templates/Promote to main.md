<!--
  KEEP THIS SECTION CURRENT.

  When a decision changes the plan, EDIT THIS BLOCK. Do not only add a
  comment saying so.

  Comments read in the order they were written, which is fine if you
  were here at the time and wrong for everyone else: a fresh reader gets
  the superseded plan first and the correction last. That has already
  caused a real misread -- see #97, where a tar-based design that had
  been dropped weeks earlier was picked back up because the decision to
  drop it was sitting in a comment.

  Comments remain the reasoning trail. The body is the answer.
-->

## Current plan

- [ ] `VERSION` bumped and `CHANGELOG.md` has a `## [<version>] - <date>` heading for it
- [ ] `README.md` Features and Quickstart describe what this version actually does (read them against the changelog)
- [ ] `site/index.html` copy and links describe the current product (it is the GitHub Pages site; it only redeploys when `site/` or the screenshots change)
- [ ] `docs/screenshots/*.png` recaptured from a seeded demo of this version if `frontend/src` changed since the last tag (`scripts/check-release-surfaces.sh` refuses stale ones)
- [ ] `SECURITY.md` and `CONTRIBUTING.md` reporting and contact channels are live
- [ ] `make release-surfaces` passes locally (same check `policy:release-surfaces` runs on the preview -> main merge request)
- [ ] `dev -> preview` merged green, `preview -> main` merged green, tag `v<version>` pushed, GitHub release run green, image pullable
- [ ] back-merge `main -> dev` opened

## Why

Release surfaces (README, site, screenshots, SECURITY, CONTRIBUTING) are not touched by normal development, so they drift. This template plus the CI check are the structure that stops the drift (owner decision, 2026-09-10).

## Done when

<!--
  How we'll know. Prefer things that can be checked by running
  something over things that can only be reviewed by reading.
-->
