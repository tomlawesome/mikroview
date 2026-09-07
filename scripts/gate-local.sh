#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Run the live-check gate on this machine, in the same container
# gate-remote.sh runs on the second host.
#
# Why this exists (#831): the second host now also serves the GitLab
# runner -- mikroview moved to GitLab-first delivery -- so a gate run
# landing there fights CI for the same CPU, and a scenario that dies of
# that contention used to get recorded as `dev` being broken. This
# workstation is the quiet machine now, so gate-dev-loop.sh runs its
# scenarios here instead. gate-remote.sh is not removed: it stays as the
# way to run the gate on the second host on purpose, e.g. while this box
# is busy with something else.
#
# No SSH, no push, no lock. A lock exists in gate-remote.sh because the
# second host is shared between a manual run and the loop and only has
# one `~/gate-work`; here the loop is the only thing expected to call
# this script back-to-back, and a manual run that wants to overlap it can
# point MV_GATE_LOCAL_WORK at a different scratch directory.
#
# Transport is a scratch clone, never a mounted worktree. The container
# chowns /work to its ci-gate user (uid 10001), which under rootless
# Docker lands as a subuid this account does not itself own -- so a plain
# `rm -rf` on a bind-mounted worktree would fail on every file, and would
# do it to a checkout something else still needs. Cloning into a
# directory outside every repository -- ~/projects/.gate-work/mikroview
# -- keeps that fallout off any real checkout; reclaiming ownership
# through the image before removing the clone (RECLAIM below) is the
# same fix gate-remote.sh's RECLAIM applies on the second host.

set -euo pipefail

WORK="${MV_GATE_LOCAL_WORK:-$HOME/projects/.gate-work/mikroview}"
BROWSER="${MV_BROWSER:-chromium}"
# --shards N / MV_SHARDS: run `make live-check-sharded` instead of `make
# live-check` -- N instances, N slices, the same scenarios in a fraction
# of the window (#1004). Empty means the unsharded gate.
SHARDS="${MV_SHARDS:-}"
KEEP=0
SRC="$(git rev-parse --show-toplevel)"
REF="$(git rev-parse --abbrev-ref HEAD)"
SHA="$(git rev-parse HEAD)"

while [ $# -gt 0 ]; do
  case "$1" in
    --browser) BROWSER="$2"; shift 2 ;;
    --shards)  SHARDS="$2"; shift 2 ;;
    --keep)    KEEP=1; shift ;;
    -h|--help)
      echo "usage: scripts/gate-local.sh [--browser chromium|firefox|webkit] [--shards N] [--keep]"
      exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

case "$BROWSER" in
  chromium|firefox|webkit) ;;
  *) echo "--browser must be chromium, firefox or webkit (got '$BROWSER')" >&2; exit 2 ;;
esac

case "$SHARDS" in
  ''|[1-8]) ;;
  *) echo "--shards must be 1 to 8 (got '$SHARDS')" >&2; exit 2 ;;
esac
if [ -n "$SHARDS" ]; then
  GATE_TARGET="MV_SHARDS=$SHARDS live-check-sharded"
else
  GATE_TARGET="live-check"
fi

echo "==> gate on this machine, engine $BROWSER${SHARDS:+, $SHARDS shards}, from $REF (${SHA:0:12})"

# A dirty tree would run code that is not what this checkout holds, and the
# run would claim to have tested a commit it did not. Refuse rather than
# mislead.
if ! git diff --quiet HEAD || ! git diff --cached --quiet; then
  echo "working tree has uncommitted changes -- commit or set them aside first" >&2
  exit 1
fi

# See header: hand the scratch tree back through the image before each
# removal, since a plain rm -rf cannot touch what the container chowned to
# uid 10001. A no-op the first time, when $WORK or the image isn't there yet.
reclaim() {
  if [ -d "$WORK" ]; then
    if ! docker run --rm --user 0 -v "$WORK:/work" mv-gate:local \
      chown -R 0:0 /work >/dev/null 2>&1; then
      echo "==> warning: could not reclaim $WORK through the image -- rm -rf may leave root-owned files behind" >&2
    fi
  fi
}

echo "==> cloning $SRC into $WORK"
reclaim
rm -rf "$WORK"
mkdir -p "$(dirname "$WORK")"
git clone -q "$SRC" "$WORK"
git -C "$WORK" checkout -q "$SHA"

# A checkout git reports as clean can still be missing tracked files -- the
# same #809 failure mode gate-remote.sh guards against, just local instead of
# over ssh. status --porcelain alone would not catch it (a missing tracked
# file with nothing re-added just looks clean); ls-files --deleted is what
# actually sees it.
dirty=$(git -C "$WORK" status --porcelain)
deleted=$(git -C "$WORK" ls-files --deleted)
if [ -n "$dirty" ] || [ -n "$deleted" ]; then
  echo "checkout at $WORK is incomplete -- refusing to build:" >&2
  [ -n "$dirty" ] && printf "%s\n" "$dirty" >&2
  [ -n "$deleted" ] && printf "%s\n" "$deleted" >&2
  exit 1
fi

echo "==> building the image"
docker build -q -f "$WORK/live-check.Dockerfile" -t mv-gate:local "$WORK" >/dev/null

echo "==> running the gate (35-50 minutes unsharded; about 36 divided by the shard count plus the standalone scripts, sharded)"
# --user 0 then dropping to ci-gate inside is deliberate, and is what the
# GitLab job worked out: under rootless Docker this account maps to
# container root, so it owns the bind mount, while the gate itself must not
# run as root -- mikroview ships USER nonroot:nonroot, and one of the
# defects this gate exists to catch was a recovery key reaching the log
# through a TTY check that a Docker pty satisfies. See
# docs/decisions/gitlab-ci-root-in-container-test-failure.md.
set +e
docker run --rm --name mv-gate-run --user 0 --shm-size=1g -v "$WORK:/work" -w /work mv-gate:local bash -c '
  set -e
  useradd -m -u 10001 ci-gate
  chown -R ci-gate:ci-gate /work
  su ci-gate -c "cd /work/frontend && HOME=/home/ci-gate npm ci"
  su ci-gate -c "cd /work && HOME=/home/ci-gate MV_BROWSER='"$BROWSER"' make '"$GATE_TARGET"'"
' 2>&1 | tee gate-run.log
gate_status=${PIPESTATUS[0]}
set -e

# Never judge a run by counting PASS against FAIL. A scenario that throws --
# a stale selector, an import error -- dies before printing any verdict, so
# counting verdicts cannot see it. That is #661, and it was read as a clean
# browser phase across two full runs. The honest check is scenarios started
# against scenarios that reported: equal means every one of them spoke.
#
# live-migrate-data.sh prints its own "== " subheading, so started is
# legitimately one higher than reported. Anything beyond that is a scenario
# that died silently.
started=$(grep -c '^== ' gate-run.log || true)
reported=$(grep -cE '^RESULT: |^PASS: ' gate-run.log || true)

echo
echo "==> scenarios started: $started   reported: $reported   (started may exceed reported by exactly 1)"
silent=$(( started - reported - 1 ))
if [ "$silent" -gt 0 ]; then
  echo "==> $silent scenario(s) died without reporting -- see gate-run.log"
fi

if [ "$KEEP" -eq 1 ]; then
  echo "==> leaving $WORK (--keep)"
else
  echo "==> cleaning up the scratch tree"
  reclaim
  rm -rf "$WORK"
fi
echo "==> log saved to gate-run.log"

exit "$gate_status"
