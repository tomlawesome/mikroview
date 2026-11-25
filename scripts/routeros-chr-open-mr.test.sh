#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# #1495: the console transcript routeros-chr-open-mr.sh commits under
# docs/routeros-verification-logs/ must open with a header naming the
# RouterOS version the CHR booted and the run date (UTC, YYYY-MM-DD),
# before the console output, so two runs never produce identical files.
#
# No CHR, no network, no real git remote: the script is copied into a
# scratch tree (it derives its repo root from its own path) and run with
# stub go, git, curl and date on PATH. Only the file it writes under
# docs/routeros-verification-logs/ is inspected.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
check() {
  if [ "$1" = "true" ]; then echo "  ok   $2"; else echo "  FAIL $2"; fails=$((fails + 1)); fi
}

mkdir -p "$TMP/bin"
cat >"$TMP/bin/go" <<'STUB'
#!/usr/bin/env bash
echo "stub row"
STUB
cat >"$TMP/bin/git" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat >"$TMP/bin/curl" <<'STUB'
#!/usr/bin/env bash
echo "[]"
STUB
cat >"$TMP/bin/date" <<'STUB'
#!/usr/bin/env bash
case "$*" in
  *%Y-%m-%d*) echo "$FAKE_DATE" ;;
  *) exec /usr/bin/date "$@" ;;
esac
STUB
chmod +x "$TMP/bin/"*

printf '== /system resource print\nversion: console-line-one\n== next\nconsole-line-two\n' >"$TMP/console.log"

# run_exercise <version> <date>: prints the committed transcript's path.
run_exercise() {
  local ver="$1" day="$2" root="$TMP/repo-$1-$2"
  mkdir -p "$root/scripts"
  cp "$HERE/routeros-chr-open-mr.sh" "$root/scripts/"
  (
    cd "$root"
    PATH="$TMP/bin:$PATH" FAKE_DATE="$day" GITLAB_MR_TOKEN=x CI_SERVER_HOST=h \
      CI_PROJECT_PATH=p/q CI_API_V4_URL=http://h/api CI_PROJECT_ID=1 \
      bash scripts/routeros-chr-open-mr.sh "$ver" "$TMP/console.log" >/dev/null 2>&1
  ) || true
  echo "$root/docs/routeros-verification-logs/$ver.log"
}

a="$(run_exercise 7.30 2026-11-23)"
b="$(run_exercise 7.31 2026-11-30)"
c="$(run_exercise 7.30 2026-12-07)"

[ -f "$a" ] || { echo "  FAIL transcript not written at $a"; exit 1; }

head_a="$(head -n 5 "$a")"
check "$(grep -q '7\.30' <<<"$(head -n 5 "$a")" && echo true || echo false)" "header names the RouterOS version"
check "$(grep -q '2026-11-23' <<<"$head_a" && echo true || echo false)" "header names the run date"
first_console="$(grep -n 'console-line-one' "$a" | head -n1 | cut -d: -f1 || true)"
first_ver="$(grep -n '7\.30' "$a" | head -n1 | cut -d: -f1 || true)"
first_date="$(grep -n '2026-11-23' "$a" | head -n1 | cut -d: -f1 || true)"
check "$([ -n "$first_ver" ] && [ -n "$first_date" ] && [ "$first_ver" -lt "$first_console" ] && [ "$first_date" -lt "$first_console" ] && echo true || echo false)" "header comes before the console output"
check "$(cmp -s "$a" "$b" && echo false || echo true)" "different versions give different files"
check "$(cmp -s "$a" "$c" && echo false || echo true)" "same version on a different date gives a different file"

if [ "$fails" -ne 0 ]; then echo "$fails check(s) failed"; exit 1; fi
echo "all checks passed"
