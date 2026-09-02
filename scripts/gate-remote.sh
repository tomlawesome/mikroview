#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Run the live-check gate on the second host, from the tree you are sitting in.
#
# Why this exists: the gate takes 35-50 minutes and holds the machine it runs
# on, which is the bottleneck #673's image was built to relieve. AGENTS.md's
# "The second host live-check runs on" has the account and its limits.
#
# Transport is `git push` over SSH into a bare repo on the host. Not rsync,
# and not a clone from GitHub or GitLab:
#
#   - Both repos are private and that account deliberately holds no
#     credential, so it cannot pull. AGENTS.md: "Never put a token on that
#     host ... If a step seems to need one, the step is wrong." Pushing
#     authenticates from this side instead, so nothing lands over there.
#   - Only new objects cross after the first run, and node_modules and the
#     worktrees/ trees never do -- git simply does not carry them. That is a
#     saving, not an omission: the far side installs the frontend's
#     dependencies itself, below, because what lands there is a fresh clone.
#   - The host gets a real checkout, so live-env.sh's `git rev-parse HEAD`
#     stamps the build honestly rather than falling back to "nogit".
#
# The bare repo and the built image are left behind on purpose: they are the
# cache that makes the second run quick. The work tree is removed, so the
# host is tidy for whoever runs next.

set -euo pipefail

HOST="${MV_GATE_HOST:-mikroview-runner}"
BROWSER="${MV_BROWSER:-chromium}"
KEEP=0
REF="$(git rev-parse --abbrev-ref HEAD)"

while [ $# -gt 0 ]; do
  case "$1" in
    --browser) BROWSER="$2"; shift 2 ;;
    --host)    HOST="$2"; shift 2 ;;
    --keep)    KEEP=1; shift ;;
    -h|--help)
      echo "usage: scripts/gate-remote.sh [--browser chromium|firefox|webkit] [--host NAME] [--keep]"
      exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

case "$BROWSER" in
  chromium|firefox|webkit) ;;
  *) echo "--browser must be chromium, firefox or webkit (got '$BROWSER')" >&2; exit 2 ;;
esac

echo "==> gate on $HOST, engine $BROWSER, from $REF ($(git rev-parse --short HEAD))"

# A dirty tree would run code that is not what gets pushed, and the run would
# claim to have tested a commit it did not. Refuse rather than mislead.
if ! git diff --quiet HEAD || ! git diff --cached --quiet; then
  echo "working tree has uncommitted changes -- commit or set them aside first" >&2
  exit 1
fi

# One host, one branch (`gate-run`), one work tree (`~/gate-work`): the gate
# has always been single-tenant, but until #809 nothing enforced it. A
# second run used to force-push over the first run's branch and rm -rf the
# tree the first run was still standing in -- destroying both runs instead
# of running one after the other. The lock below makes that single-tenancy
# explicit: a run that cannot take the lock refuses immediately, which is
# cheaper than two runs that both fail an hour in. See #809.
# shellcheck disable=SC2317  # only invoked indirectly, via 'trap release_lock EXIT' below
release_lock() {
  local status=$?
  ssh "$HOST" 'rm -rf ~/gate-lock' >/dev/null 2>&1 || true
  exit "$status"
}

OWNER_INFO="host=$(hostname) user=$USER ref=$REF sha=$(git rev-parse --short HEAD) pid=$$ start=$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Single-quoted so $HOME, $(...) etc. resolve on the far side, not here --
# same convention as RECLAIM below. HOST_PLACEHOLDER is swapped for the
# local $HOST after the fact, since it must name the host as this side
# knows it, for the hint printed on refusal.
LOCK_ACQUIRE='set -eu
if mkdir ~/gate-lock 2>/dev/null; then
  cat > ~/gate-lock/owner
  exit 0
fi
existing=$(cat ~/gate-lock/owner 2>/dev/null || echo "(no owner file)")
if [ -n "$(find ~/gate-lock -maxdepth 0 -mmin +15 2>/dev/null)" ] && ! docker ps --format "{{.Names}}" | grep -qx mv-gate-run; then
  echo "==> gate-lock is older than 15 minutes and no mv-gate-run container is running -- treating it as abandoned" >&2
  echo "==> previous owner: $existing" >&2
  rm -rf ~/gate-lock
  mkdir ~/gate-lock
  cat > ~/gate-lock/owner
  exit 0
fi
echo "another gate run holds HOST_PLACEHOLDER: $existing; wait for it, or if it is dead: ssh HOST_PLACEHOLDER rm -r ~/gate-lock" >&2
exit 1
'
LOCK_ACQUIRE="${LOCK_ACQUIRE//HOST_PLACEHOLDER/$HOST}"

echo "==> taking the gate lock on $HOST"
if ! ssh "$HOST" "$LOCK_ACQUIRE" <<<"$OWNER_INFO"; then
  exit 1
fi
trap release_lock EXIT

echo "==> pushing the tree"
ssh "$HOST" 'mkdir -p ~/gate-repo.git && cd ~/gate-repo.git && git rev-parse --is-bare-repository >/dev/null 2>&1 || git init --bare -q'
git push --force --quiet "$HOST:gate-repo.git" "HEAD:refs/heads/gate-run"

# The gate's container chowns /work to uid 10001, which under rootless Docker
# lands on the host as a subuid this account does not own -- so a plain
# `rm -rf ~/gate-work` fails on every file, leaves a quarter of a gigabyte
# behind, and blocks the next run's checkout too. Hand the tree back through
# the image, which can, before each removal. A no-op when the tree or the
# image is not there yet, which is the first run.
RECLAIM='if [ -d ~/gate-work ]; then
  if ! docker run --rm --user 0 -v "$HOME/gate-work:/work" mv-gate:local \
    chown -R 0:0 /work >/dev/null 2>&1; then
    echo "==> warning: could not reclaim ~/gate-work through the image -- rm -rf may leave root-owned files behind" >&2
  fi
fi'

# A checkout that git clone reports as clean can still be missing tracked
# files -- #809's other half, where the second run's rm -rf landed mid
# checkout and the next clone inherited whatever the OS had not yet
# deleted. status --porcelain would not catch that on its own (a missing
# tracked file with nothing re-added just looks clean); ls-files --deleted
# is what actually sees it.
CHECKOUT_AND_BUILD='set -eu
rm -rf ~/gate-work
git clone -q --branch gate-run ~/gate-repo.git ~/gate-work
cd ~/gate-work
dirty=$(git status --porcelain)
deleted=$(git ls-files --deleted)
if [ -n "$dirty" ] || [ -n "$deleted" ]; then
  echo "checkout on $(hostname) is incomplete -- refusing to build:" >&2
  [ -n "$dirty" ] && printf "%s\n" "$dirty" >&2
  [ -n "$deleted" ] && printf "%s\n" "$deleted" >&2
  exit 1
fi
docker build -q -f live-check.Dockerfile -t mv-gate:local . >/dev/null'

echo "==> checking out and building the image (cached after the first run)"
ssh "$HOST" "$RECLAIM
$CHECKOUT_AND_BUILD"

echo "==> running the gate (35-50 minutes)"
# --user 0 then dropping to ci-gate inside is deliberate, and is what the
# GitLab job worked out: under rootless Docker this account maps to container
# root, so it owns the bind mount, while the gate itself must not run as root
# -- mikroview ships USER nonroot:nonroot, and one of the defects this gate
# exists to catch was a recovery key reaching the log through a TTY check that
# a Docker pty satisfies. See docs/decisions/gitlab-ci-root-in-container-test-failure.md.
set +e
ssh "$HOST" "set -eu
  cd ~/gate-work
  docker run --rm --name mv-gate-run --user 0 --shm-size=1g -v \"\$HOME/gate-work:/work\" -w /work mv-gate:local bash -c '
    set -e
    useradd -m -u 10001 ci-gate
    chown -R ci-gate:ci-gate /work
    su ci-gate -c \"cd /work/frontend && HOME=/home/ci-gate npm ci\"
    su ci-gate -c \"cd /work && HOME=/home/ci-gate MV_BROWSER=$BROWSER make live-check\"
  '" 2>&1 | tee gate-run.log
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
  echo "==> leaving ~/gate-work on $HOST (--keep)"
else
  echo "==> cleaning up the work tree on $HOST"
  ssh "$HOST" "$RECLAIM
rm -rf ~/gate-work"
fi
echo "==> log saved to gate-run.log"

exit "$gate_status"
