# Base images stay on tags, not digests

Date: moved out of `AGENTS.md` 2026-10-10, which keeps the rule and its
trigger as one line and links here for the reasoning.

## The decision

A `FROM` line names a tag (`golang:1.27.1-alpine`,
`gcr.io/distroless/static-debian12:nonroot`), never a `@sha256:` digest.
That is a decision, not an omission -- a digest is more reproducible and
immune to a tag being repointed under you, which is a real supply-chain
property to give up.

## What it costs, and why that decides it

A digest freezes the base image, so the distroless base stops receiving the
security patches that are published *by moving the tag*. Nothing here bumps
digests automatically, so pinning them means a build that quietly gets more
vulnerable the longer nobody touches it -- the same failure as a version
pin nothing bumps (the `go` directive once sat at 1.23.4 until Go 1.23 left
its support window, leaving 22-29 unpatched standard-library CVEs in the
build), applied to the one layer with no version number to notice.

## Trigger for revisiting

Adopting a bot that opens digest bumps (Renovate, or Dependabot's Docker
ecosystem). With something bumping them, digests cost nothing and should be
taken. Until then the tag is the safer of two imperfect options, and the
images in question are official ones from Google and Docker.

Status check, 2026-10-10: `renovate.json` watches the `dockerfile` and
`gitlabci` managers, so base-image tag bumps arrive as merge requests, but
it does not enable digest pinning. The trigger is therefore not yet met.

## Scope

This is about `FROM` lines only. It does not affect promoting the exact
tested preview digest to the release tag -- that digest pin is the point of
the release step, per the global delivery rules.
