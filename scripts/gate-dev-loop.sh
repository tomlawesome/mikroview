#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Run the live-check gate on `dev`, after the merge, for as long as this
# process lives. AGENTS.md, "The gate runs on `dev`, after the merge", has
# the rule this implements; #831 is the issue.
#
# Each pass: fetch, move a detached worktree to the `gitlab` remote's dev
# -- GitLab is where merges happen and GitHub is its mirror (#935), so
# watching GitHub would run whatever the mirror last managed to push, and
# a dead mirror would look like a quiet dev -- and if that is
# a commit the loop has not run yet, run `scripts/gate-remote.sh` from it
# (the second host, under the #822 lock). The log is kept per commit and
# the failing-scenario set is diffed against the previous run's, because a
# raw count says nothing while the baseline is red (#667): what matters is
# which scenarios newly fail, and those are filed against the merges
# between the two commits.
#
# Runs from this machine, not the runner, because the runner holds no
# credential and cannot fetch (AGENTS.md: never put a token on that host);
# gate-remote.sh pushes the tree to it. The fetch authenticates with a
# GitLab deploy token -- read_repository only, this project only -- in
# git's credential-store format at $MV_GATE_CREDENTIALS, mode 600:
#
#   https://<deploy-token-username>:<deploy-token>@gitlab.tomlawson.io
#
# The owner creates the token (GitLab: Settings > Repository > Deploy
# tokens) and writes that file; nothing here ever prints it. Start the
# loop detached:
#
#   setsid nohup scripts/gate-dev-loop.sh >>~/projects/.gate-logs/mikroview/loop.log 2>&1 </dev/null & disown
#
# and watch loop.log: one line per event, `NEWFAIL`, `FIXED`, `CLEAN`,
# `SAME`, `ERROR` or `LOST`, each with the commit it is about. `LOST` is a
# run that died before producing a result (#861) -- distinct from a `FAIL`
# scenario inside a completed run -- and is retried next tick like any
# other unfinished commit; it also leaves a `gate-<sha>.lost` file in
# $LOGDIR so the loss is on record even if that commit is superseded
# before the retry lands. A commit that recovers on a later attempt gets
# a `RECOVERED` line alongside its normal result, so it never reads as a
# plain healthy run.

set -u

# --git-common-dir, not --show-toplevel: started from a worktree, the
# toplevel is that worktree, and the gate worktree hangs off the main one.
WORKTREE="${MV_GATE_WORKTREE:-$(dirname "$(git rev-parse --path-format=absolute --git-common-dir)")/.claude/worktrees/gate-dev}"
LOGDIR="${MV_GATE_LOGDIR:-$HOME/projects/.gate-logs/mikroview}"
POLL="${MV_GATE_POLL:-600}"
REMOTE="${MV_GATE_REMOTE:-gitlab}"
CREDENTIALS="${MV_GATE_CREDENTIALS:-$HOME/.config/mikroview/gitlab-credentials}"

if [ ! -r "$CREDENTIALS" ]; then
  echo "ERROR $(date -u +%Y-%m-%dT%H:%M:%SZ) no credential file at $CREDENTIALS -- see the header"
  exit 1
fi

mkdir -p "$LOGDIR"

# The scenarios a log shows failing, one name per line: a `RESULT: FAIL`
# after a `== ` header, or a header that no verdict ever followed -- the
# silent death #661 taught the gate to count. Same reading as
# gate-remote.sh's started-against-reported check, resolved to names.
# Only headers naming a script count: live-migrate-data.sh prints a
# subheading of its own, which is the one gate-remote.sh allows for.
failing() {
  awk '
    /^== (frontend\/)?scripts\// { if (cur != "" && !seen) print cur; cur = $2; seen = 0; next }
    /^RESULT: FAIL/           { print cur; seen = 1; next }
    /^RESULT: PASS|^PASS: /   { seen = 1; next }
    END                       { if (cur != "" && !seen) print cur }
  ' "$1" | sed 's|^frontend/scripts/||' | sort -u
}

stamp() { date -u +%Y-%m-%dT%H:%M:%SZ; }

while :; do
  if ! git -C "$WORKTREE" -c "credential.helper=store --file=$CREDENTIALS" fetch -q "$REMOTE"; then
    echo "ERROR $(stamp) fetch failed; retrying in ${POLL}s"
    sleep "$POLL"; continue
  fi
  sha=$(git -C "$WORKTREE" rev-parse --short "$REMOTE/dev")
  if [ -f "$LOGDIR/gate-$sha.log" ]; then
    sleep "$POLL"; continue
  fi
  git -C "$WORKTREE" checkout -q --detach "$REMOTE/dev"
  git -C "$WORKTREE" clean -qfd -e node_modules
  git -C "$WORKTREE" submodule update --init -q 2>/dev/null || true

  prev=$(find "$LOGDIR" -maxdepth 1 -name 'gate-*.log' -printf '%T@ %p\n' | sort -rn | head -1 | cut -d' ' -f2-)
  echo "START $(stamp) $sha"
  # gate-remote.sh writes gate-run.log in its cwd and exits non-zero on
  # any failing scenario, which on a red baseline is every run: the exit
  # code is not the signal here, the set difference below is.
  (cd "$WORKTREE" && scripts/gate-remote.sh >"$LOGDIR/gate-$sha.run" 2>&1)
  if [ ! -s "$WORKTREE/gate-run.log" ]; then
    # Nothing came back: the lock was held, the host is down, or the push
    # failed -- most often (#861) a transient build failure, such as an
    # IPv6 Docker Hub token fetch the host cannot source an address for.
    # The .run file says which. Retry without marking the commit done, so
    # the commit still gets its run -- but record the loss first, durably:
    # a build failure here previously left only a scrolling ERROR line
    # among fetch-retry ERRORs of the same word, so a run that never came
    # back (dev moved on before the retry landed) was indistinguishable
    # from a commit the loop simply had not reached yet. `gate-<sha>.lost`
    # persists in $LOGDIR regardless of what happens next, so that window
    # going unwatched stays visible even after the loop moves on.
    errline=$(tail -1 "$LOGDIR/gate-$sha.run")
    echo "$(stamp) $errline" >>"$LOGDIR/gate-$sha.lost"
    echo "LOST $(stamp) $sha no gate-run.log -- $errline"
    sleep "$POLL"; continue
  fi
  mv "$WORKTREE/gate-run.log" "$LOGDIR/gate-$sha.log"
  rm -f "$LOGDIR/gate-$sha.run"
  if [ -f "$LOGDIR/gate-$sha.lost" ]; then
    # This sha did eventually get a result, but not on the first attempt:
    # say so in the stream too, so a later CLEAN/SAME/NEWFAIL line for it
    # never reads as though nothing went wrong.
    echo "RECOVERED $(stamp) $sha: $(wc -l <"$LOGDIR/gate-$sha.lost") lost attempt(s) first -- see gate-$sha.lost"
  fi

  failing "$LOGDIR/gate-$sha.log" >"$LOGDIR/gate-$sha.failing"
  started=$(grep -cE '^== (frontend/)?scripts/' "$LOGDIR/gate-$sha.log" || true)
  nfail=$(wc -l <"$LOGDIR/gate-$sha.failing")
  if [ -n "$prev" ]; then
    psha=$(basename "$prev" .log); psha=${psha#gate-}
    new=$(comm -13 "$LOGDIR/gate-$psha.failing" "$LOGDIR/gate-$sha.failing" | tr '\n' ' ')
    fixed=$(comm -23 "$LOGDIR/gate-$psha.failing" "$LOGDIR/gate-$sha.failing" | tr '\n' ' ')
    [ -n "$new" ]   && echo "NEWFAIL $(stamp) $sha vs $psha: $new"
    [ -n "$fixed" ] && echo "FIXED $(stamp) $sha vs $psha: $fixed"
    [ -z "$new" ] && [ -z "$fixed" ] && echo "SAME $(stamp) $sha vs $psha: $nfail failing of $started"
  fi
  [ "$nfail" -eq 0 ] && echo "CLEAN $(stamp) $sha: $started scenarios, none failing"
  echo "END $(stamp) $sha: $nfail failing of $started"
done
