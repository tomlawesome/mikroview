# The weekly CHR exercise (#894)

`scripts/routeros-freshness.sh` (GitHub Actions, weekly) already notices
when MikroTik ships a stable RouterOS release nobody has read the
release notes for. This job is the other half: it actually **runs**
mikroview's setup commands against that release, rather than relying
only on a human having read about it.

## What it does, in order

1. Asks MikroTik's own feed (the same one `routeros-freshness.sh` reads)
   for the newest stable RouterOS version.
2. Checks `internal/routeros/dialects.go`'s table. If a row already
   covers that version, the job stops here and passes — there is
   nothing new to exercise.
3. Otherwise it boots that exact RouterOS version as MikroTik's own CHR
   (Cloud Hosted Router) image, directly inside the job's own container,
   under QEMU with software emulation rather than hardware acceleration
   — this only needs to run configuration commands, not push real
   traffic, so no `/dev/kvm` access (and no Docker) is required.
4. It asks `scripts/routeroscommands` — a small program that reads
   straight from `internal/routeros`, the same table
   `POST /api/setup/commands` renders from — for the wizard's starting
   commands: CA trust, syslog, and rule tagging (the commands that get a
   router logging to mikroview; not the optional push/schedule steps,
   which need a live mikroview instance this job doesn't have). Reading
   from the table, rather than a copy typed into this job, is what
   keeps the exercise from silently disagreeing with what the wizard
   actually shows an operator.
5. It runs each of those commands on the booted router over its console
   and checks whether RouterOS accepted it.

## What "green" and "red" mean

**Red** means RouterOS refused one of the commands outright — the kind
of error that means the command's syntax no longer exists on that
release, not a network hiccup. The job fails and names exactly which
command was refused, with the full console transcript in the job log.
That is the signal a human needs to write a new dialect.

**Green after actually booting a CHR** means every command still parses.
The job then:

- adds one row to `internal/routeros/dialects.go` recording
  `exercised on CHR <version>, <date>`,
- saves the full console transcript alongside it, under
  `docs/routeros-verification-logs/<version>.log`,
- and opens a **merge request** on this project's GitLab instance
  (`ai/mikroview`), from a new branch into `dev`.

**Green with nothing to boot** (step 2 above) means the table already
covers the newest release. The job passes quietly — no merge request,
nothing to review.

## What the owner sees, and what to do with it

A merge request titled `RouterOS <version>: exercised on CHR, needs a
release-notes read`. Its description says plainly that this is
mechanical — the commands still *parse*, nothing has judged whether
anything *behaves* differently on the new release. The merge request's
own CI is expected to show one test red on purpose:
`TestReviewedVersionMatchesNewest`, because the job never bumps
`ReviewedVersion` (`internal/routeros/versions.go`) — that constant is
specifically the claim "a human read the release notes", which this job
never makes.

**Before merging**, read the new release's notes against
`internal/routeros/commands.go` and `docs/routeros-setup.md`, fix
anything that actually moved, and bump `ReviewedVersion` in the same
merge request. That is what turns the red test green and is the one
step this job cannot do for anyone.

**This merge request is never merged automatically.** Nothing in this
project auto-merges anything; a human reviews and merges it like any
other change.

## Setup this job needs before it can run

Two things the owner creates, neither of which this change can create
on its own (repository and CI/CD settings are the owner's alone).
Both already exist:

1. A GitLab Pipeline Schedule for this project, weekly, carrying one
   variable: `CHR_EXERCISE` = `true`. Nothing else runs this job — it
   is scoped to that one schedule so it cannot fire from a `dev` push
   or a merge request pipeline, and cannot collide with any other
   scheduled pipeline added later.
2. A GitLab CI/CD variable, `GITLAB_MR_TOKEN` (masked, protected): an
   impersonation token of the `mikroview-bot` user, who is a Developer
   on this project — the minimum needed to push a branch and open a
   merge request through GitLab's own API. It is read once from the
   job's environment and never written to disk, the same pattern
   `sync:mirror-to-github` already uses for its own token in
   `.gitlab-ci.yml`.

No runner setup is needed: the job runs on the same untagged shared
runner as the rest of this pipeline. It has no Docker-in-Docker to
enable, because it has no Docker at all — see `.gitlab-ci.yml`'s
`chr-exercise` stage comment and `scripts/routeros-chr-boot.sh` for why
software-emulated QEMU (`-accel tcg`) needs no privileged runner.

## What has not been run

Nobody has run this job. Building it happened without touching
GitLab, starting QEMU, downloading a CHR image, or running the live
gate. Verified without running it: the Go command's output is
byte-identical to what the wizard serves (`go test ./internal/api`),
every touched script passes shellcheck, and the CI file parses. Not
verified until the first real run: that `qemu-system-x86` installs and
boots cleanly on the shared runner's `golang:1.27` image, the boot and
run time under software emulation, and the refusal-detection pattern in
`scripts/routeros-chr-exercise.sh`, which must be calibrated against
the first real transcript. Record the first run's timing on #894.
