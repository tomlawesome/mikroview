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
# a dead mirror would look like a quiet dev -- and run
# `scripts/gate-local.sh` from it, on this machine. No SSH: the loop used
# to push the tree to the second host because that box was quiet and this
# one wasn't, but the second host now also runs GitLab CI (mikroview moved
# to GitLab-first delivery) and a scenario that died of contention with a
# CI job got recorded as `dev` being broken. This workstation is the quiet
# machine now, so the run happens here, and the `ssh $GATE_HOST test -e
# /srv/quiet-host/hold` skip that used to defer to perf:promotion is gone
# with it -- that flag lives on the second host, which this loop no longer
# touches at all.
#
# If `dev` has not moved, the loop does not go idle: it re-runs the same
# commit instead, up to MV_GATE_MAX_REPEATS (20) times, each attempt in its
# own log (gate-<sha>-<n>.log, n starting at 1). An idle spell is exactly
# when repeat runs are most useful -- running one unchanged commit
# repeatedly is the only way to measure how often a scenario fails rather
# than whether it failed once, which a single run can never distinguish
# from a fresh regression (#667). Once a commit has used its 20 runs the
# loop idles on the poll interval until `dev` moves; a new commit resets
# the count and is picked up on the very next fetch, ahead of any quota
# left on the old one.
#
# The failing-scenario comparison is now two comparisons, not one:
#   - scenarios that failed in *every* run of a commit so far (its
#     "consistent" failing set) are compared against the previous
#     *different* commit's consistent set -- NEWFAIL / FIXED / CLEAN /
#     SAME, unchanged in meaning from before, just fed a deflaked set
#     instead of a single run's raw one;
#   - scenarios that failed in *some but not all* runs of the current
#     commit are flakes, not regressions: each gets its own FLAKE line,
#     e.g. "failed 2 of 7 runs". Recomputed after every run, so the
#     picture sharpens (or a NEWFAIL turns out to be a FLAKE) as more
#     runs land on an unmoving `dev`.
#
# Runs from this machine, fetching over HTTPS with a GitLab deploy token --
# read_repository only, this project only -- in git's credential-store
# format at $MV_GATE_CREDENTIALS, mode 600:
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
# `SAME`, `FLAKE`, `ERROR` or `LOST`, each with the commit it is about.
# `LOST` is a run that died before producing a result (#861) -- distinct
# from a `FAIL` scenario inside a completed run -- and is retried next tick
# like any other unfinished commit (it does not consume one of the 20
# repeats, since it never produced a numbered log); it also leaves a
# `gate-<sha>.lost` file in $LOGDIR so the loss is on record even if that
# commit is superseded before the retry lands. A commit that recovers on a
# later attempt gets a `RECOVERED` line alongside its normal result, so it
# never reads as a plain healthy run; that line prints once per new loss,
# not once per successful run after it, using `gate-<sha>.lost.acked` to
# remember how much of `gate-<sha>.lost` has already been reported.

set -u

# Fixed under ~/projects/.worktrees, not derived from --git-common-dir: a
# default under .claude/worktrees was pruned by worktree clean-up on
# 2026-09-05 and the loop died for four hours before anyone noticed (#831).
WORKTREE="${MV_GATE_WORKTREE:-$HOME/projects/.worktrees/mikroview/gate-dev}"
LOGDIR="${MV_GATE_LOGDIR:-$HOME/projects/.gate-logs/mikroview}"
POLL="${MV_GATE_POLL:-600}"
REMOTE="${MV_GATE_REMOTE:-gitlab}"
CREDENTIALS="${MV_GATE_CREDENTIALS:-$HOME/.config/mikroview/gitlab-credentials}"
# Repeats of one unchanged commit before the loop idles on it. See header.
MAX_REPEATS="${MV_GATE_MAX_REPEATS:-20}"

if [ ! -r "$CREDENTIALS" ]; then
  echo "ERROR $(date -u +%Y-%m-%dT%H:%M:%SZ) no credential file at $CREDENTIALS -- see the header"
  exit 1
fi

mkdir -p "$LOGDIR"

# The scenarios a log shows failing, one name per line: a `RESULT: FAIL`
# after a `== ` header, or a header that no verdict ever followed -- the
# silent death #661 taught the gate to count. Same reading as
# gate-local.sh's started-against-reported check, resolved to names.
# Only headers naming a script count: live-migrate-data.sh prints a
# subheading of its own, which is the one gate-local.sh allows for.
failing() {
  awk '
    /^== (frontend\/)?scripts\// { if (cur != "" && !seen) print cur; cur = $2; seen = 0; next }
    /^RESULT: FAIL/           { print cur; seen = 1; next }
    /^RESULT: PASS|^PASS: /   { seen = 1; next }
    END                       { if (cur != "" && !seen) print cur }
  ' "$1" | sed 's|^frontend/scripts/||' | sort -u
}

stamp() { date -u +%Y-%m-%dT%H:%M:%SZ; }

# printf '%s\n' on a variable that holds the empty string still emits one
# blank line, which then reads to comm as a real "line only on this side"
# once the other side has no matching blank -- silently poisoning NEWFAIL/
# FIXED with a phantom entry whenever a commit's consistent failing set is
# genuinely empty. Emit nothing at all for an empty variable instead.
nz() { [ -n "$1" ] && printf '%s\n' "$1"; return 0; }

# The intersection of every completed run's failing set for commit $1: the
# scenarios that failed in *all* of them. This, not any single run's raw
# failing set, is what NEWFAIL/FIXED/CLEAN/SAME compare -- a scenario only
# some runs saw fail is a flake (see flaky() below), not a confirmed
# failure of this commit.
consistent_failing() {
  local sha="$1" f acc
  local files
  mapfile -t files < <(find "$LOGDIR" -maxdepth 1 -name "gate-$sha-*.failing" 2>/dev/null | sort)
  [ "${#files[@]}" -eq 0 ] && return 0
  acc=$(sort -u "${files[0]}")
  for f in "${files[@]:1}"; do
    acc=$(comm -12 <(printf '%s\n' "$acc") <(sort -u "$f"))
  done
  printf '%s\n' "$acc" | sed '/^$/d'
}

# Scenarios whose result differs between this commit's runs so far: failed
# in at least one, passed in at least one other. Prints one line per flake,
# tab-separated: name, how many runs it failed, how many runs there have
# been. Nothing to say before a commit's second run.
flaky() {
  local sha="$1" scen cnt total
  local files
  mapfile -t files < <(find "$LOGDIR" -maxdepth 1 -name "gate-$sha-*.failing" 2>/dev/null | sort)
  total=${#files[@]}
  [ "$total" -lt 2 ] && return 0
  while IFS= read -r scen; do
    [ -z "$scen" ] && continue
    cnt=$(grep -Fxl "$scen" "${files[@]}" | wc -l)
    if [ "$cnt" -gt 0 ] && [ "$cnt" -lt "$total" ]; then
      printf '%s\t%s\t%s\n' "$scen" "$cnt" "$total"
    fi
  done < <(cat "${files[@]}" | sort -u)
}

while :; do
  if ! git -C "$WORKTREE" -c "credential.helper=store --file=$CREDENTIALS" fetch -q "$REMOTE"; then
    echo "ERROR $(stamp) fetch failed; retrying in ${POLL}s"
    sleep "$POLL"; continue
  fi
  sha=$(git -C "$WORKTREE" rev-parse --short "$REMOTE/dev")

  # How many completed runs this commit already has, purely from what is on
  # disk -- so a restarted loop resumes the count correctly instead of
  # giving every commit a fresh 20 on every process start.
  n=$(( $(find "$LOGDIR" -maxdepth 1 -name "gate-$sha-*.log" 2>/dev/null | wc -l) + 1 ))
  if [ "$n" -gt "$MAX_REPEATS" ]; then
    sleep "$POLL"; continue
  fi

  # The previous *different* commit's log, most recent first, also read
  # from disk rather than remembered in-process -- the same durability the
  # .lost file relies on. Skipped once we hit one belonging to the current
  # sha, which is what a repeat run's own earlier logs would otherwise look
  # like.
  prev_sha=""
  while IFS= read -r fname; do
    [ -z "$fname" ] && continue
    base=$(basename "$fname")
    rest=${base#gate-}
    candidate=${rest%-*.log}
    if [ "$candidate" != "$sha" ]; then
      prev_sha="$candidate"
      break
    fi
  done < <(find "$LOGDIR" -maxdepth 1 -name 'gate-*.log' -printf '%T@ %p\n' 2>/dev/null | sort -rn | cut -d' ' -f2-)

  git -C "$WORKTREE" checkout -q --detach "$REMOTE/dev"
  git -C "$WORKTREE" clean -qfd -e node_modules
  git -C "$WORKTREE" submodule update --init -q 2>/dev/null || true

  echo "START $(stamp) $sha run $n/$MAX_REPEATS"
  # gate-local.sh writes gate-run.log in its cwd and exits non-zero on any
  # failing scenario, which on a red baseline is every run: the exit code
  # is not the signal here, the set difference below is.
  (cd "$WORKTREE" && scripts/gate-local.sh >"$LOGDIR/gate-$sha.run" 2>&1)
  if [ ! -s "$WORKTREE/gate-run.log" ]; then
    # Nothing came back: the host is down, or the build failed -- most
    # often (#861) a transient failure such as an IPv6 Docker Hub token
    # fetch this machine cannot source an address for. The .run file says
    # which. Retry without marking the commit done or consuming a repeat,
    # so the commit still gets its run -- but record the loss first,
    # durably: a build failure here previously left only a scrolling ERROR
    # line among fetch-retry ERRORs of the same word, so a run that never
    # came back (dev moved on before the retry landed) was
    # indistinguishable from a commit the loop simply had not reached yet.
    # `gate-<sha>.lost` persists in $LOGDIR regardless of what happens
    # next, so that window going unwatched stays visible even after the
    # loop moves on.
    errline=$(tail -1 "$LOGDIR/gate-$sha.run")
    echo "$(stamp) run $n: $errline" >>"$LOGDIR/gate-$sha.lost"
    echo "LOST $(stamp) $sha run $n/$MAX_REPEATS no gate-run.log -- $errline"
    sleep "$POLL"; continue
  fi
  mv "$WORKTREE/gate-run.log" "$LOGDIR/gate-$sha-$n.log"
  rm -f "$LOGDIR/gate-$sha.run"

  if [ -f "$LOGDIR/gate-$sha.lost" ]; then
    # This sha did eventually get a result, but not on its first attempt.
    # Print RECOVERED once per new loss, not once per successful run after
    # it -- gate-$sha.lost.acked remembers how many lines of gate-$sha.lost
    # have already been reported, so a commit that keeps running cleanly
    # after recovering does not repeat the line on every remaining repeat.
    lost_lines=$(wc -l <"$LOGDIR/gate-$sha.lost")
    acked_file="$LOGDIR/gate-$sha.lost.acked"
    acked=0
    [ -f "$acked_file" ] && acked=$(cat "$acked_file")
    if [ "$lost_lines" -gt "$acked" ]; then
      echo "RECOVERED $(stamp) $sha run $n/$MAX_REPEATS: $lost_lines lost attempt(s) so far -- see gate-$sha.lost"
      echo "$lost_lines" >"$acked_file"
    fi
  fi

  failing "$LOGDIR/gate-$sha-$n.log" >"$LOGDIR/gate-$sha-$n.failing"
  started=$(grep -cE '^== (frontend/)?scripts/' "$LOGDIR/gate-$sha-$n.log" || true)
  nfail_run=$(wc -l <"$LOGDIR/gate-$sha-$n.failing")

  # Flakes first: scenarios whose result has differed across this commit's
  # runs so far. Meaningful only from the second run of a commit onward.
  while IFS=$'\t' read -r scen cnt total; do
    [ -z "$scen" ] && continue
    echo "FLAKE $(stamp) $sha: $scen failed $cnt of $total runs"
  done < <(flaky "$sha")

  consistent=$(consistent_failing "$sha")
  nfail=$(printf '%s\n' "$consistent" | sed '/^$/d' | wc -l)

  if [ -n "$prev_sha" ] && find "$LOGDIR" -maxdepth 1 -name "gate-$prev_sha-*.log" -print -quit 2>/dev/null | grep -q .; then
    prev_consistent=$(consistent_failing "$prev_sha")
    new=$(comm -13 <(nz "$prev_consistent" | sort -u) <(nz "$consistent" | sort -u) | tr '\n' ' ')
    fixed=$(comm -23 <(nz "$prev_consistent" | sort -u) <(nz "$consistent" | sort -u) | tr '\n' ' ')
    [ -n "$new" ]   && echo "NEWFAIL $(stamp) $sha run $n/$MAX_REPEATS vs $prev_sha: $new"
    [ -n "$fixed" ] && echo "FIXED $(stamp) $sha run $n/$MAX_REPEATS vs $prev_sha: $fixed"
    [ -z "$new" ] && [ -z "$fixed" ] && echo "SAME $(stamp) $sha run $n/$MAX_REPEATS vs $prev_sha: $nfail failing of $started"
  fi
  # CLEAN keeps the meaning it has always had: *this run* had nothing
  # failing. It deliberately does not mean "nothing fails consistently" --
  # a scenario that fails three runs in twenty is not a green gate, and a
  # CLEAN line printed over the top of a FLAKE line would say it was.
  # Consistency is what NEWFAIL/FIXED/SAME compare across commits; CLEAN
  # is about the run in hand.
  [ "$nfail_run" -eq 0 ] && echo "CLEAN $(stamp) $sha run $n/$MAX_REPEATS: $started scenarios, none failing"
  echo "END $(stamp) $sha run $n/$MAX_REPEATS: $nfail_run failing of $started"
done
