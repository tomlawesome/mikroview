#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# .gitignore ignores *.log everywhere, which is right for the stray logs a
# local run drops -- and was wrong for one file. The weekly CHR exercise
# commits its console transcript beside the dialect row that transcript
# proves (scripts/routeros-chr-open-mr.sh), so `git add` refused the path
# and the nightly job failed after every part of its real work had
# succeeded: RouterOS 7.24.4 was exercised and still had no row anywhere
# (#1318). A negation fixed it, and this is the recurrence guard -- the
# next broad ignore rule that swallows those transcripts fails here rather
# than in a month of silent nightly failures.
#
# The directory is read out of the script that writes it, not spelled
# again here: renaming it there has to fail here. Same reasoning as
# dockerignore.test.sh -- a rule said in one place drifts.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fails=0
check() {
  if [ "$1" = "true" ]; then
    echo "  ok   $2"
  else
    echo "  FAIL $2"
    fails=$((fails + 1))
  fi
}

# The log_dest= line in the exercise's companion script, with the version
# placeholder resolved: docs/routeros-verification-logs/<version>.log.
log_dest="$(sed -n 's/^log_dest="\(.*\)"$/\1/p' "$ROOT/scripts/routeros-chr-open-mr.sh")"
if [ -z "$log_dest" ]; then
  echo "gitignore.test.sh: no log_dest= line in scripts/routeros-chr-open-mr.sh -- did it move?" >&2
  exit 1
fi
verification_log="${log_dest/\$\{version\}/7.24.4}"
check "$([ "$verification_log" != "$log_dest" ] && echo true || echo false)" \
  "resolved the version placeholder in $log_dest"

# Not ignored: the transcript the CHR job commits. `git check-ignore -q`
# without -v exits 1 when no rule ignores the path, negations included.
ok=false
git -C "$ROOT" check-ignore -q "$verification_log" || ok=true
check "$ok" "$verification_log is not ignored"

# Still ignored: the strays the *.log rule is actually for. The negation
# is anchored and one level deep, so neither a root log, nor a log
# somewhere else under docs/, nor a subdirectory of the transcripts
# themselves comes back.
for stray in gate-run.log frontend/build.log docs/some-other.log \
  "$(dirname "$verification_log")/nested/7.24.4.log"; do
  ok=false
  git -C "$ROOT" check-ignore -q "$stray" && ok=true
  check "$ok" "$stray is still ignored"
done

# End-to-end, because the exit code above is not what broke: the job runs
# `git add`, and that is what refused the path. A throwaway repo carrying
# only this .gitignore, so the real index is never touched.
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
git -C "$scratch" init -q
cp "$ROOT/.gitignore" "$scratch/.gitignore"
mkdir -p "$scratch/$(dirname "$verification_log")"
printf 'console transcript\n' > "$scratch/$verification_log"
ok=false
git -C "$scratch" add "$verification_log" 2>/dev/null && ok=true
check "$ok" "git add accepts $verification_log (#1318)"

if [ "$fails" -gt 0 ]; then
  echo "gitignore.test.sh: $fails failure(s)" >&2
  exit 1
fi
echo "gitignore.test.sh: all passed"
