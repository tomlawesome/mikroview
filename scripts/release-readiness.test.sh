#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Tests for scripts/release-readiness.py (#1504), written from
# docs/releases/readiness-checks.md alone. Every case builds a throwaway git
# repository with a bare remote, and answers the GitLab API and go.dev from a
# local HTTP stub; nothing touches the network.
#
# CONTRACT (what the spec leaves open, chosen here; the script must meet it)
#
#   Pinned by the spec: `--target preview|main [--remote NAME]`, default
#   remote `gitlab`; exit 0 ready, 1 any check failed, 2 usage error with no
#   check run; the header, `ok:`, `FAIL:` and summary lines and the check ids
#   and order; CI_API_V4_URL, CI_PROJECT_ID (default `ai%2Fmikroview`),
#   CI_REUSE_TOKEN then GITLAB_TOKEN sent as PRIVATE-TOKEN;
#   RELEASE_READINESS_GO_DL_URL; the script never fetches.
#
#   Chosen here:
#   1. API paths, all GET, JSON, relative to $CI_API_V4_URL/projects/$PID:
#        issues/<n>                     -> {"iid":n,"state":"closed"|"opened"}
#        milestones?title=v<version>    -> [{"id":..,"title":"v<version>"}],
#                                          [] when there is none
#        issues?milestone=v<version>&state=opened
#                                       -> [{"iid":n,...}] open issues of it
#        pipelines?sha=<full sha>&ref=<source branch>&source=push
#                                       -> [{"id","status","web_url"}] newest
#                                          first, [] when none
#      The stub ignores query strings (it only logs them), so a script may
#      add parameters, but must send those above.
#   2. Go list: GET $RELEASE_READINESS_GO_DL_URL -> JSON list of
#      {"version":"go1.27.4","stable":true}; unstable entries are ignored.
#   3. A usage error prints to stderr a first line starting
#      `release-readiness: ` (so a missing script, which python also exits 2
#      on, is not mistaken for one), and prints no `ok:`/`FAIL:` line.
#   4. Problem text is the script's own where the spec says `{problem}` or
#      `{problems}`. These keywords must appear in it: changelog `heading`
#      (no heading), `date` (bad date), `empty` (no entries), `Unreleased`
#      (entries still there); release-audit/review-issue/milestone/
#      source-pipeline/go-toolchain/release-record only the fixed prefix and
#      suffix the spec gives are pinned, and the problem part is non-empty.
#   5. check 6 repeats each FAIL line of check-release-surfaces.sh (the text
#      after its `FAIL:` marker is what is tested, in order, joined by '; ').
#   6. With no token no API request is sent at all (fail closed).
#
# Not covered here: the .gitlab-ci.yml wiring test (spec, last section); it
# belongs with the CI change that adds the release_readiness job.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/release-readiness.py"
TMP="$(mktemp -d)"
STUB="$TMP/stub"
mkdir -p "$STUB"
STUB_PID=""
cleanup() { [ -n "$STUB_PID" ] && kill "$STUB_PID" 2>/dev/null; rm -rf "$TMP"; }
trap cleanup EXIT

export GIT_CONFIG_GLOBAL="$TMP/gitconfig" GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0
git config --global user.name "Release Agent"
git config --global user.email "ra@example.org"
git config --global commit.gpgsign false
git config --global init.defaultBranch main
git config --global advice.detachedHead false

fail=0
COUNT=0
chk() { local n="$1"; shift; COUNT=$((COUNT + 1))
  if "$@"; then echo "ok - $n"; else echo "FAIL - $n"; fail=1; fi; }
eq() { [ "$1" = "$2" ]; }
glob() { [[ "$1" == $2 ]]; }       # glob <value> <pattern>
has() { grep -qF -- "$2" <<<"$1"; }
hasnt() { ! grep -qF -- "$2" <<<"$1"; }

# ---------------------------------------------------------------- stub server
cat >"$TMP/stub.py" <<'PY'
import http.server, json, sys
D = sys.argv[1]
class H(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def do_GET(self):
        path, _, q = self.path.partition("?")
        with open(D + "/log", "a") as f:
            f.write("%s?%s\t%s\n" % (path, q, self.headers.get("PRIVATE-TOKEN", "")))
        routes = json.load(open(D + "/routes.json"))
        r = routes.get(path)
        status, body = (404, {"message": "404 Not found"}) if r is None else (r["status"], r["body"])
        data = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
s = http.server.ThreadingHTTPServer(("127.0.0.1", 0), H)
open(D + "/port", "w").write(str(s.server_address[1]))
s.serve_forever()
PY
echo '{}' >"$STUB/routes.json"
python3 "$TMP/stub.py" "$STUB" &
STUB_PID=$!
for _ in $(seq 1 50); do [ -s "$STUB/port" ] && break; sleep 0.1; done
PORT="$(cat "$STUB/port")"
API="http://127.0.0.1:$PORT/api/v4"

PID=42
reset_stub() {
  PID=42
  ISSUE_STATUS=200; ISSUE_BODY='{"iid":1426,"state":"closed"}'
  MS_BODY='[{"id":7,"title":"v'"$V"'"}]'
  ISSUES_BODY='[{"iid":1500,"state":"opened"}]'
  PIPE_STATUS=200; PIPE_BODY='[{"id":9,"status":"success","web_url":"http://gl.example.org/p/9"}]'
  GO_STATUS=200; GO_BODY='[{"version":"go1.27.4","stable":true},{"version":"go1.26.9","stable":true}]'
}
write_routes() {
  : >"$STUB/log"
  cat >"$STUB/routes.json" <<EOF
{"/api/v4/projects/$PID/issues/1426":{"status":$ISSUE_STATUS,"body":$ISSUE_BODY},
 "/api/v4/projects/$PID/milestones":{"status":200,"body":$MS_BODY},
 "/api/v4/projects/$PID/issues":{"status":200,"body":$ISSUES_BODY},
 "/api/v4/projects/$PID/pipelines":{"status":$PIPE_STATUS,"body":$PIPE_BODY},
 "/dl/":{"status":$GO_STATUS,"body":$GO_BODY}}
EOF
}

# -------------------------------------------------------------------- fixture
N=0
set_file() { mkdir -p "$(dirname "$WORK/$1")"; cat >"$WORK/$1"; }
head12() { git -C "$WORK" rev-parse HEAD | cut -c1-12; }
tip12() { git -C "$WORK" rev-parse "$REMOTE/$1" | cut -c1-12; }
push_tip() {
  git -C "$WORK" push -q "$REMOTE" HEAD:dev
  [ "$LEG" = main ] && git -C "$WORK" push -q "$REMOTE" HEAD:preview
  return 0
}
commit_push() {  # commit_push <message>   (after editing files)
  git -C "$WORK" add -A && git -C "$WORK" commit -q -m "$1" && push_tip
}
remote_extra() {  # remote_extra <branch>: a commit on the remote only, then fetch
  local c="$TMP/extra$N"; rm -rf "$c"
  git clone -q "$REMOTE_DIR" "$c" && git -C "$c" checkout -q "$1" \
    && echo x >"$c/extra.txt" && git -C "$c" add -A && git -C "$c" commit -q -m extra \
    && git -C "$c" push -q origin "$1"
  git -C "$WORK" fetch -q "$REMOTE"
}
add_tag() { git -C "$WORK" tag "$1" "${2:-HEAD}" && git -C "$WORK" push -q "$REMOTE" "$1"; }

GUIDE=$'# Guide\n\n## What you do, in short\n\n1. Close the milestone.\n   A continuation line.\n2. Bump VERSION.\n3. Audit.\n4. Cut the changelog.\n\n## Later section\n\n5. Not a step of the short list.\n'
guide_n() {  # guide_n <count>: a guide whose short list has <count> steps
  { printf '# Guide\n\n## What you do, in short\n\n'
    for i in $(seq 1 "$1"); do printf '%s. Step text.\n' "$i"; done
    printf '\n## Later section\n\n99. Not a step.\n'; } | set_file docs/releasing.md
}

good_record() {  # good_record [steps]
  local n="${1:-4}" blob gc
  blob="$(git -C "$WORK" rev-parse HEAD:docs/releasing.md)"
  gc="$(git -C "$WORK" log -1 --format=%H -- docs/releasing.md)"
  printf 'version: v%s\nguide-blob: %s\nguide-commit: %s\npromote-issue: #1500\n\n' "$V" "$blob" "$gc"
  for i in $(seq 1 "$n"); do
    if [ "$i" -le 3 ]; then printf 'step %s: done: https://gitlab.example.org/ai/mikroview/-/issues/1500#note_%s\n' "$i" "$i"
    else printf 'step %s: after-merge\n' "$i"; fi
  done
  printf '\nSigned-off-by: Release Agent\n'
}
RC_TRAILER="Release Agent"; RC_AUTHOR=""
rec_commit() {  # rec_commit <sed script> [steps]: rewrite and commit the record
  local f="docs/releases/v$V.md"
  mkdir -p "$WORK/docs/releases"
  good_record "${2:-4}" | sed -E "$1" >"$WORK/$f"
  git -C "$WORK" add -A
  local args=(-q -m "release record")
  [ -n "$RC_TRAILER" ] && args+=(--trailer "Signed-off-by: $RC_TRAILER")
  [ -n "$RC_AUTHOR" ] && args+=(--author "$RC_AUTHOR <x@example.org>")
  git -C "$WORK" commit "${args[@]}" && push_tip
  RC_TRAILER="Release Agent"; RC_AUTHOR=""
}

new() {  # new <preview|main> ; knobs: VER, TAGS, REMOTE_NAME
  LEG="$1"; N=$((N + 1))
  V="${VER:-1.2.3}"; REMOTE="${REMOTE_NAME:-origin}"
  case "$LEG" in preview) T=preview; SRC=dev; TGT=preview;; main) T=main; SRC=preview; TGT=main;; esac
  local W="$TMP/case$N"; REMOTE_DIR="$W/remote.git"; WORK="$W/work"
  mkdir -p "$W"
  git init -q --bare "$REMOTE_DIR"
  git init -q "$WORK"
  git -C "$WORK" remote add "$REMOTE" "$REMOTE_DIR"
  printf '1.2.2\n' | set_file VERSION
  printf '# Changelog\n\n## [Unreleased]\n\n## [1.2.2] - 2026-10-01\n\n- Older thing.\n' | set_file CHANGELOG.md
  printf 'FROM golang:1.27.4-alpine AS build\nRUN true\n' | set_file Dockerfile
  printf '%s' "$GUIDE" | set_file docs/releasing.md
  printf '#!/bin/sh\n[ -f VERSION ] || { echo "FAIL: not run from the repository root"; exit 1; }\nexit 0\n' | set_file scripts/check-release-surfaces.sh
  echo readme | set_file README.md
  git -C "$WORK" add -A && git -C "$WORK" commit -q -m base
  for t in ${TAGS-v1.2.2}; do git -C "$WORK" tag "$t"; done
  git -C "$WORK" push -q "$REMOTE" HEAD:main HEAD:preview
  [ -n "${TAGS-v1.2.2}" ] && git -C "$WORK" push -q "$REMOTE" --tags
  git -C "$WORK" checkout -q -b dev
  # the batch
  printf '%s\n' "$V" | set_file VERSION
  printf '# Changelog\n\n## [Unreleased]\n\n## [%s] - 2026-11-12\n\n### Added\n- A thing.\n\n## [1.2.2] - 2026-10-01\n\n- Older thing.\n' "$V" | set_file CHANGELOG.md
  git -C "$WORK" add -A && git -C "$WORK" commit -q -m batch
  local audited; audited="$(git -C "$WORK" rev-parse HEAD)"
  printf '# v%s audit record, #1426\n\nAudited-through: %s\n' "$V" "$audited" | set_file ".agents/reviews/2026-11-10-v$V-record.md"
  git -C "$WORK" add -A && git -C "$WORK" commit -q -m audit
  RC_TRAILER="Release Agent"; RC_AUTHOR=""
  # the record, last: push dev first (preview stays at main on the preview leg)
  mkdir -p "$WORK/docs/releases"
  good_record 4 >"$WORK/docs/releases/v$V.md"
  git -C "$WORK" add -A && git -C "$WORK" commit -q -m "release record" --trailer "Signed-off-by: Release Agent"
  git -C "$WORK" push -q "$REMOTE" HEAD:dev
  [ "$LEG" = main ] && git -C "$WORK" push -q "$REMOTE" HEAD:preview
  reset_stub
  TOKEN_ENV=(GITLAB_TOKEN=tok-gl-SECRET1234)
  PROJ_ENV=(CI_PROJECT_ID=42)
}

run_rr() {  # run_rr [args...]; default args come from the leg
  local args=("$@")
  [ $# -eq 0 ] && args=(--target "$T" --remote "$REMOTE")
  write_routes
  OUT="$(cd "$WORK" && env -u CI_REUSE_TOKEN -u GITLAB_TOKEN -u CI_PROJECT_ID \
    ${TOKEN_ENV[@]+"${TOKEN_ENV[@]}"} ${PROJ_ENV[@]+"${PROJ_ENV[@]}"} \
    CI_API_V4_URL="${API_URL:-$API}" RELEASE_READINESS_GO_DL_URL="http://127.0.0.1:$PORT/dl/?mode=json" \
    python3 "$SCRIPT" "${args[@]}" 2>"$TMP/err")"
  RC=$?
  ERR="$(cat "$TMP/err")"
}

IDS="source-tip contains-target version tag-free changelog release-surfaces release-audit review-issue milestone source-pipeline go-toolchain release-record"
failline() { grep -m1 "^FAIL: $1: " <<<"$OUT"; }
okcount() { grep -c '^ok: ' <<<"$OUT"; }
has_fail() { [ -n "$(failline "$1")" ]; }
has_ok() { grep -q "^ok: $1: " <<<"$OUT"; }
fails_in_block() { grep -c '^  FAIL: ' <<<"$OUT"; }
only_fail() {  # only_fail <id>: the one check that failed, 11 others ok, exit 1
  [ "$RC" = 1 ] && [ "$(okcount)" = 11 ] && has_fail "$1" \
    && has "$OUT" "release-readiness: 1 of 12 checks failed:" \
    && has "$OUT" "  $(failline "$1")" && ! has "$OUT" "Ready ("
}

# ----------------------------------------------------------- the script exists
chk "script scripts/release-readiness.py exists" test -f "$SCRIPT"

# ------------------------------------------------------- all pass, both legs
for leg in preview main; do
  new $leg; run_rr
  chk "$leg leg: all pass, exit 0" eq "$RC" 0
  chk "$leg leg: header line" has "$OUT" "release-readiness: target $T, source $SRC at $(head12), version 1.2.3"
  chk "$leg leg: Ready summary" has "$OUT" "release-readiness: Ready (12 of 12 checks passed)"
  chk "$leg leg: twelve ok lines in spec order" eq "$(sed -nE 's/^ok: ([a-z0-9-]+):.*/\1/p' <<<"$OUT" | tr '\n' ' ')" "$IDS "
  chk "$leg leg: no FAIL line" hasnt "$OUT" "FAIL:"
  for id in $IDS; do chk "$leg leg: ok line for $id" has_ok "$id"; done
done

# ---------------------------------------------------------------- usage errors
new preview
run_rr; : # default args fine; now the usage cases
for args in "" "--target bogus" "--target" "--remote origin"; do
  run_rr $args
  chk "usage '$args': exit 2" eq "$RC" 2
  chk "usage '$args': stderr says release-readiness:" glob "$ERR" "release-readiness: *"
  chk "usage '$args': no check run" eq "$(grep -cE '^(ok|FAIL): ' <<<"$OUT")" 0
done
mkdir -p "$TMP/nogit"
OUT="$(cd "$TMP/nogit" && env CI_API_V4_URL="$API" GITLAB_TOKEN=x python3 "$SCRIPT" --target preview 2>"$TMP/err")"; RC=$?; ERR="$(cat "$TMP/err")"
chk "outside a git work tree: exit 2" eq "$RC" 2
chk "outside a git work tree: stderr says release-readiness:" glob "$ERR" "release-readiness: *"
chk "outside a git work tree: no check run" eq "$(grep -cE '^(ok|FAIL): ' <<<"$OUT")" 0

# default remote is gitlab
REMOTE_NAME=gitlab new preview; run_rr --target preview
chk "default remote is gitlab: exit 0" eq "$RC" 0
# default project and CI_PROJECT_ID absent
new preview; PROJ_ENV=(); PID='ai%2Fmikroview'; run_rr
PID=42
write_routes_pid() { :; }
chk "project defaults to ai%2Fmikroview" eq "$RC" 0

# the script never fetches: a remote move that was not fetched is not seen
new preview; git -C "$WORK" fetch -q "$REMOTE"; before="$(git -C "$WORK" rev-parse "$REMOTE/dev")"
c="$TMP/mv"; git clone -q "$REMOTE_DIR" "$c" && git -C "$c" checkout -q dev && echo y >"$c/y" && git -C "$c" add -A && git -C "$c" commit -q -m y && git -C "$c" push -q origin dev
run_rr
chk "no fetch: tracking ref unchanged" eq "$(git -C "$WORK" rev-parse "$REMOTE/dev")" "$before"
chk "no fetch: source-tip still ok on the stale ref" has_ok source-tip

# ---------------------------------------------------------------- 1 source-tip
new preview; echo more >"$WORK/note.txt"; git -C "$WORK" add -A; git -C "$WORK" commit -q -m local-only
run_rr
chk "1 source-tip: HEAD ahead of remote fails" eq "$(failline source-tip)" "FAIL: source-tip: HEAD $(head12) is not origin/dev ($(tip12 dev)), or tracked files are modified -- fetch, then check out origin/dev cleanly and run again"
chk "1 source-tip: only that fails" only_fail source-tip
new main; echo more >"$WORK/note.txt"; git -C "$WORK" add -A; git -C "$WORK" commit -q -m local-only
run_rr
chk "1 source-tip: main leg names origin/preview" eq "$(failline source-tip)" "FAIL: source-tip: HEAD $(head12) is not origin/preview ($(tip12 preview)), or tracked files are modified -- fetch, then check out origin/preview cleanly and run again"
new preview; echo changed >>"$WORK/README.md"; run_rr
chk "1 source-tip: modified tracked file fails" eq "$(failline source-tip)" "FAIL: source-tip: HEAD $(head12) is not origin/dev ($(tip12 dev)), or tracked files are modified -- fetch, then check out origin/dev cleanly and run again"
chk "1 source-tip: dirty tree, only that fails" only_fail source-tip
new preview; echo junk >"$WORK/untracked.txt"; run_rr
chk "1 source-tip: an untracked file does not fail" eq "$RC" 0

# ------------------------------------------------------------ 2 contains-target
new preview; remote_extra preview; run_rr
chk "2 contains-target preview leg fails with its message" eq "$(failline contains-target)" "FAIL: contains-target: dev does not contain preview's tip $(tip12 preview) -- the main -> dev back-merge of the last release is missing; merge it first (docs/releasing.md step 12)"
chk "2 contains-target preview leg: only that fails" only_fail contains-target
new main; remote_extra main; run_rr
chk "2 contains-target main leg fails with its message" eq "$(failline contains-target)" "FAIL: contains-target: preview does not contain main's tip $(tip12 main) -- main has a commit preview lacks; back-merge main -> dev and promote dev -> preview again"
chk "2 contains-target main leg: only that fails" only_fail contains-target

# ------------------------------------------------------------------- 3 version
new preview; add_tag v1.2.4; run_rr
chk "3 version: not above newest tag" eq "$(failline version)" "FAIL: version: VERSION is '1.2.3', not above the newest tag v1.2.4 -- bump VERSION on the batch before the audit (docs/releasing.md step 2)"
chk "3 version: only that fails" only_fail version
VER=1.2.10 TAGS="v1.2.9 v1.2.2" new preview; run_rr
chk "3 version: numeric not string comparison (1.2.10 > 1.2.9)" eq "$RC" 0
TAGS="v1.2.2 nightly v1.2.3-rc1" new preview; run_rr
chk "3 version: tags not shaped vX.Y.Z are ignored" eq "$RC" 0
for bad in '1.2' 'v1.2.3' '1.2.3-rc1' '1.2.3 1.2.4'; do
  new preview; printf '%s\n' "$bad" | set_file VERSION; commit_push "bad version"; run_rr
  chk "3 version: '$bad' is the wrong shape" glob "$(failline version)" "FAIL: version: VERSION is '$bad'*"
  chk "3 version '$bad': exit 1" eq "$RC" 1
  for id in tag-free changelog release-audit release-record; do
    chk "3 version '$bad': $id says could not run" glob "$(failline $id)" "FAIL: $id: could not run: VERSION is not X.Y.Z*"
  done
done

# ------------------------------------------------------------------ 4 tag-free
new preview; add_tag v1.2.3; run_rr
chk "4 tag-free: existing tag fails" eq "$(failline tag-free)" "FAIL: tag-free: tag v1.2.3 already exists -- this version is released; bump VERSION for a new release"
chk "4 tag-free: exit 1 (version fails too: not above its own tag)" eq "$RC" 1
chk "4 tag-free: version also fails" has_fail version
chk "4 tag-free: two checks fail" has "$OUT" "release-readiness: 2 of 12 checks failed:"

# ------------------------------------------------------------------ 5 changelog
FIX5=" -- rename '## [Unreleased]' to '## [1.2.3] - YYYY-MM-DD' and add an empty '## [Unreleased]' above it, on the release branch (docs/releasing.md step 4)"
cl() { printf '%s' "$1" | set_file CHANGELOG.md; commit_push changelog; run_rr; }
new preview; cl $'# Changelog\n\n## [Unreleased]\n\n- Not yet released.\n\n## [1.2.3] - 2026-11-12\n\n- Thing.\n'
chk "5 changelog: entries under Unreleased fail" glob "$(failline changelog)" "FAIL: changelog: ?*Unreleased*$FIX5"
chk "5 changelog: only that fails" only_fail changelog
new preview; cl $'# Changelog\n\n## [Unreleased]\n\n### Added\n\n## [1.2.3] - 2026-11-12\n\n- Thing.\n'
chk "5 changelog: a ### under Unreleased fails" has_fail changelog
new preview; cl $'# Changelog\n\n## [1.2.2] - 2026-10-01\n\n- Older.\n'
chk "5 changelog: no heading for the version" glob "$(failline changelog)" "FAIL: changelog: ?*heading*$FIX5"
new preview; cl $'# Changelog\n\n## [Unreleased]\n\n## [1.2.3] - 2026-02-30\n\n- Thing.\n'
chk "5 changelog: impossible calendar date" glob "$(failline changelog)" "FAIL: changelog: ?*date*$FIX5"
new preview; cl $'# Changelog\n\n## [Unreleased]\n\n## [1.2.3] - 12 Nov 2026\n\n- Thing.\n'
chk "5 changelog: wrong date format" glob "$(failline changelog)" "FAIL: changelog: ?*date*$FIX5"
new preview; cl $'# Changelog\n\n## [Unreleased]\n\n## [1.2.3] - 2026-11-12\n\nJust prose, no entry.\n\n## [1.2.2] - 2026-10-01\n\n- Older.\n'
chk "5 changelog: section with no '- ' line is empty" glob "$(failline changelog)" "FAIL: changelog: ?*empty*$FIX5"
new preview; cl $'# Changelog\n\n## [Unreleased]\n\n## [1.2.3] - 2026-11-12\n\n## [1.2.2] - 2026-10-01\n\n- Older entry must not count.\n'
chk "5 changelog: an entry in the next section does not count" has_fail changelog
new preview; cl $'# Changelog\n\n## [Unreleased]\n\n- Left over.\n\n## [1.2.3] - 2026-13-45\n\n- Thing.\n'
chk "5 changelog: several problems, one FAIL line naming both" glob "$(failline changelog)" "FAIL: changelog: *Unreleased*date*$FIX5"
chk "5 changelog: several problems, still one FAIL line" eq "$(grep -c '^FAIL: changelog: ' <<<"$OUT")" 1
new preview; cl $'# Changelog\n\n## [1.2.3] - 2026-11-12\n\n- Thing.\n'
chk "5 changelog: no Unreleased section at all passes" has_ok changelog
new preview; cl $'# Changelog\n\n## [Unreleased]\n\nSome intro prose, no entries.\n\n## [1.2.3] - 2026-11-12\n\n- Thing.\n'
chk "5 changelog: prose without '- ' under Unreleased passes" has_ok changelog

# -------------------------------------------------------- 6 release-surfaces
FIX6=" -- fix each as it says, on the batch; make release-surfaces runs it alone"
new preview
printf '#!/bin/sh\necho "checking"\necho "FAIL: docs/screenshots/stream-dark.png is stale"\necho "FAIL: README.md Features is stale"\nexit 1\n' | set_file scripts/check-release-surfaces.sh
commit_push surfaces; run_rr
chk "6 release-surfaces: two FAIL lines repeated, in order, with the count" glob "$(failline release-surfaces)" "FAIL: release-surfaces: 2 surface check(s) failed: *stream-dark.png is stale*; *README.md Features is stale$FIX6"
chk "6 release-surfaces: only that fails" only_fail release-surfaces
chk "6 release-surfaces: non-FAIL output is not repeated" hasnt "$(failline release-surfaces)" "checking"
new preview
printf '#!/bin/sh\necho "FAIL: one"\nexit 1\n' | set_file scripts/check-release-surfaces.sh
commit_push surfaces; run_rr
chk "6 release-surfaces: one failure" glob "$(failline release-surfaces)" "FAIL: release-surfaces: 1 surface check(s) failed: *one$FIX6"
new preview; git -C "$WORK" rm -q scripts/check-release-surfaces.sh; commit_push "no surfaces script"; run_rr
chk "6 release-surfaces: missing script is could not run" glob "$(failline release-surfaces)" "FAIL: release-surfaces: could not run:*"
chk "6 release-surfaces: missing script does not stop the run (11 others ran)" eq "$(( $(okcount) + $(grep -c '^FAIL: ' <<<"$OUT") ))" 12

# ------------------------------------------------------------- 7 release-audit
FIX7=" -- the pre-release audit for v1.2.3 must land on dev with its record before promoting (docs/releasing.md step 3)"
new preview; git -C "$WORK" rm -q .agents/reviews/2026-11-10-v1.2.3-record.md; commit_push "no audit"; run_rr
chk "7 release-audit: no record file" glob "$(failline release-audit)" "FAIL: release-audit: ?*$FIX7"
new preview; cp "$WORK/.agents/reviews/2026-11-10-v1.2.3-record.md" "$WORK/.agents/reviews/2026-11-11-v1.2.3-record.md"; commit_push "two audits"; run_rr
chk "7 release-audit: two record files" glob "$(failline release-audit)" "FAIL: release-audit: ?*$FIX7"
new preview; printf '# v1.2.3 audit record, #1426\n\nNo marker here.\n' | set_file .agents/reviews/2026-11-10-v1.2.3-record.md; commit_push "no marker"; run_rr
chk "7 release-audit: no Audited-through line" glob "$(failline release-audit)" "FAIL: release-audit: ?*$FIX7"
new preview; printf '# v1.2.3 audit record, #1426\n\nAudited-through: deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n' | set_file .agents/reviews/2026-11-10-v1.2.3-record.md; commit_push "bad marker"; run_rr
chk "7 release-audit: marker not in history" glob "$(failline release-audit)" "FAIL: release-audit: ?*$FIX7"
chk "7 release-audit: only that fails (milestone/issue unaffected)" only_fail release-audit
new preview; printf '# v1.2.3 audit record, #1426\n\nAudited-through: %s\n' "$(git -C "$WORK" rev-parse --short HEAD)" | set_file .agents/reviews/2026-11-10-v1.2.3-record.md; commit_push "short marker"; run_rr
chk "7 release-audit: a short sha is not a 40-hex marker" has_fail release-audit
new preview; git -C "$WORK" checkout -q -b side main; echo s >"$WORK/side.txt"; git -C "$WORK" add -A; git -C "$WORK" commit -q -m side
side="$(git -C "$WORK" rev-parse HEAD)"; git -C "$WORK" checkout -q dev
printf '# v1.2.3 audit record, #1426\n\nAudited-through: %s\n' "$side" | set_file .agents/reviews/2026-11-10-v1.2.3-record.md; commit_push "marker on another branch"; run_rr
chk "7 release-audit: a real commit that is not an ancestor fails" has_fail release-audit

# ------------------------------------------------------------- 8 review-issue
new preview; ISSUE_BODY='{"iid":1426,"state":"opened"}'; run_rr
chk "8 review-issue: open issue message" glob "$(failline review-issue)" "FAIL: review-issue: ?* -- close the review issue #1426 with its evidence before promoting (docs/releasing.md step 3)"
chk "8 review-issue: only that fails" only_fail review-issue
chk "8 review-issue: the API was asked for issue 1426" has "$(cat "$STUB/log")" "/api/v4/projects/42/issues/1426"
new preview; printf '# v1.2.3 audit record\n\nAudited-through: %s\n' "$(git -C "$WORK" rev-parse HEAD~2)" | set_file .agents/reviews/2026-11-10-v1.2.3-record.md; commit_push "no issue number"; run_rr
chk "8 review-issue: no issue number on the first line" glob "$(failline review-issue)" "FAIL: review-issue: ?* -- close the review issue*"
new preview; printf '# v1.2.3 audit #1426 and then #1300\n\nAudited-through: %s\n' "$(git -C "$WORK" rev-parse HEAD~2)" | set_file .agents/reviews/2026-11-10-v1.2.3-record.md; commit_push "two numbers"; run_rr
chk "8 review-issue: first #number on the line is used" has "$(cat "$STUB/log")" "/issues/1426"
chk "8 review-issue: second number is not asked for" hasnt "$(cat "$STUB/log")" "/issues/1300"
new preview; TOKEN_ENV=(); run_rr
chk "8 review-issue: no token, exact message" eq "$(failline review-issue)" "FAIL: review-issue: could not run: no GitLab token -- set GITLAB_TOKEN to a read_api token (CI: CI_REUSE_TOKEN)"
new preview; ISSUE_STATUS=500; ISSUE_BODY='{"message":"boom"}'; run_rr
chk "8 review-issue: API error is could not run" glob "$(failline review-issue)" "FAIL: review-issue: could not run: *"
chk "8 review-issue: API error, other 11 checks still ran" eq "$(( $(okcount) + $(grep -c '^FAIL: ' <<<"$OUT") ))" 12

# ------------------------------------------------------- missing token, tokens
new preview; TOKEN_ENV=(); run_rr
chk "token: missing token exits 1" eq "$RC" 1
for id in review-issue milestone source-pipeline; do chk "token: missing token fails $id as could not run" glob "$(failline $id)" "FAIL: $id: could not run:*"; done
chk "token: only the three API checks fail" has "$OUT" "release-readiness: 3 of 12 checks failed:"
chk "token: no API request is sent without a token" eq "$(wc -l <"$STUB/log" | tr -d ' ')" 0
new preview; TOKEN_ENV=(GITLAB_TOKEN=tok-gl-AAAA CI_REUSE_TOKEN=tok-reuse-BBBB); run_rr
chk "token: CI_REUSE_TOKEN wins over GITLAB_TOKEN" eq "$(cut -f2 "$STUB/log" | sort -u | tr '\n' ' ')" "tok-reuse-BBBB "
chk "token: passes with it" eq "$RC" 0
new preview; TOKEN_ENV=(CI_REUSE_TOKEN=tok-reuse-CCCC); run_rr
chk "token: CI_REUSE_TOKEN alone works" eq "$RC" 0
new preview; TOKEN_ENV=(GITLAB_TOKEN=tok-gl-DDDD); run_rr
chk "token: GITLAB_TOKEN alone is sent as the PRIVATE-TOKEN header" eq "$(cut -f2 "$STUB/log" | sort -u | tr '\n' ' ')" "tok-gl-DDDD "
new preview; TOKEN_ENV=(GITLAB_TOKEN=tok-gl-LEAKTEST); ISSUE_BODY='{"iid":1426,"state":"opened"}'; PIPE_BODY='[{"id":9,"status":"failed","web_url":"http://gl.example.org/p/9"}]'; ISSUE_STATUS=200; run_rr
chk "token: never printed on failure (stdout)" hasnt "$OUT" "tok-gl-LEAKTEST"
chk "token: never printed on failure (stderr)" hasnt "$ERR" "tok-gl-LEAKTEST"
new preview; TOKEN_ENV=(GITLAB_TOKEN=tok-gl-LEAKTEST2); ISSUE_STATUS=500; ISSUE_BODY='{"message":"x"}'; run_rr
chk "token: never printed on API error" hasnt "$OUT$ERR" "tok-gl-LEAKTEST2"

# ----------------------------------------------------------------- 9 milestone
FIX9=" -- close them, or have the owner move them to the next milestone (docs/releasing.md step 1)"
new preview; ISSUES_BODY='[{"iid":1500},{"iid":1501},{"iid":1502}]'; run_rr
chk "9 milestone: two other open issues" glob "$(failline milestone)" "FAIL: milestone: v1.2.3 has 2 open issue(s) besides the promote issue: *1501*1502*$FIX9"
chk "9 milestone: the promote issue is not listed" hasnt "$(failline milestone)" "1500"
chk "9 milestone: only that fails" only_fail milestone
new preview; ISSUES_BODY='[]'; run_rr
chk "9 milestone: no open issues passes" eq "$RC" 0
new preview; MS_BODY='[]'; run_rr
chk "9 milestone: missing milestone fails" glob "$(failline milestone)" "FAIL: milestone: ?*v1.2.3*"
new preview; ISSUES_BODY="[$(seq -s, 2001 2012 | sed -E 's/([0-9]+)/{"iid":\1}/g')]"; run_rr
chk "9 milestone: counts all twelve" glob "$(failline milestone)" "FAIL: milestone: v1.2.3 has 12 open issue(s) besides the promote issue: *"
chk "9 milestone: lists at most ten numbers" eq "$(failline milestone | grep -oE '20[0-9][0-9]' | wc -l | tr -d ' ')" 10
new preview; rec_commit '/^promote-issue:/d'; run_rr
chk "9 milestone: no promote-issue line, every open issue counts" glob "$(failline milestone)" "FAIL: milestone: v1.2.3 has 1 open issue(s) besides the promote issue: *1500*"
new preview; git -C "$WORK" rm -q docs/releases/v1.2.3.md; commit_push "no record"; run_rr
chk "9 milestone: no record, every open issue counts" glob "$(failline milestone)" "FAIL: milestone: v1.2.3 has 1 open issue(s)*"
chk "9 milestone: the record's promote issue is asked about the milestone" has "$(cat "$STUB/log")" "milestone"
new preview; ISSUES_BODY='[{"iid":1500}]'; run_rr
chk "9 milestone: milestone title queried as v1.2.3" has "$(cat "$STUB/log")" "title=v1.2.3"
chk "9 milestone: open issues queried for the milestone" has "$(cat "$STUB/log")" "milestone=v1.2.3"

# ------------------------------------------------------------ 10 source-pipeline
FIX10=" -- wait for it, or fix it and push again, before promoting"
new preview; PIPE_BODY='[{"id":9,"status":"failed","web_url":"http://gl.example.org/p/9"}]'; run_rr
chk "10 source-pipeline: failed" eq "$(failline source-pipeline)" "FAIL: source-pipeline: the dev push pipeline for $(head12) is failed (http://gl.example.org/p/9)$FIX10"
chk "10 source-pipeline: only that fails" only_fail source-pipeline
new preview; PIPE_BODY='[{"id":9,"status":"running","web_url":"http://gl.example.org/p/9"}]'; run_rr
chk "10 source-pipeline: running counts as a fail with its own message" glob "$(failline source-pipeline)" "FAIL: source-pipeline: *running*$FIX10"
new preview; PIPE_BODY='[]'; run_rr
chk "10 source-pipeline: none found" glob "$(failline source-pipeline)" "FAIL: source-pipeline: ?*$FIX10"
new preview; PIPE_BODY='[{"id":9,"status":"failed","web_url":"http://x/9"},{"id":8,"status":"success","web_url":"http://x/8"}]'; run_rr
chk "10 source-pipeline: newest (first) failed, an older success does not rescue it" has_fail source-pipeline
new preview; PIPE_BODY='[{"id":9,"status":"success","web_url":"http://x/9"},{"id":8,"status":"failed","web_url":"http://x/8"}]'; run_rr
chk "10 source-pipeline: newest success passes over an older failure" eq "$RC" 0
chk "10 source-pipeline: queried by sha, ref and push source" has "$(cat "$STUB/log")" "sha=$(git -C "$WORK" rev-parse HEAD)"
chk "10 source-pipeline: ref=dev on the preview leg" has "$(cat "$STUB/log")" "ref=dev"
chk "10 source-pipeline: source=push" has "$(cat "$STUB/log")" "source=push"
new main; run_rr
chk "10 source-pipeline: ref=preview on the main leg" has "$(cat "$STUB/log")" "ref=preview"
new main; PIPE_BODY='[{"id":9,"status":"failed","web_url":"http://gl.example.org/p/9"}]'; run_rr
chk "10 source-pipeline: main leg names preview" eq "$(failline source-pipeline)" "FAIL: source-pipeline: the preview push pipeline for $(head12) is failed (http://gl.example.org/p/9)$FIX10"

# ------------------------------------------------------------ 11 go-toolchain
new preview; printf 'FROM golang:1.27.3-alpine AS build\n' | set_file Dockerfile; commit_push "old go"
GO_BODY='[{"version":"go1.27.5","stable":true},{"version":"go1.27.4","stable":true},{"version":"go1.26.9","stable":true}]'; run_rr
chk "11 go-toolchain: newer patch exists" eq "$(failline go-toolchain)" "FAIL: go-toolchain: the image builds with go1.27.3, newest is go1.27.5 -- bump Go on the batch everywhere lint:supply-chain-pins checks (Dockerfile, .gitlab-ci.yml, supply-chain/pins-policy.json)"
chk "11 go-toolchain: only that fails" only_fail go-toolchain
new preview; GO_BODY='[{"version":"go1.28.1","stable":true},{"version":"go1.26.9","stable":true}]'; run_rr
chk "11 go-toolchain: X.Y no longer listed fails" glob "$(failline go-toolchain)" "FAIL: go-toolchain: *go1.27.4*"
new preview; GO_STATUS=500; GO_BODY='{"message":"x"}'; run_rr
chk "11 go-toolchain: page cannot be read fails" has_fail go-toolchain
new preview; GO_BODY='[{"version":"go1.27.5rc1","stable":false},{"version":"go1.27.4","stable":true}]'; run_rr
chk "11 go-toolchain: an unstable newer release is ignored" has_ok go-toolchain
new preview; printf 'FROM golang:1.27.9-alpine AS build\n' | set_file Dockerfile; commit_push "go 1.27.9"
GO_BODY='[{"version":"go1.27.10","stable":true},{"version":"go1.27.9","stable":true}]'; run_rr
chk "11 go-toolchain: numeric patch comparison (1.27.10 newer than 1.27.9)" glob "$(failline go-toolchain)" "FAIL: go-toolchain: the image builds with go1.27.9, newest is go1.27.10 --*"
new preview; GO_BODY='[{"version":"go1.27.4","stable":true}]'; run_rr
chk "11 go-toolchain: current patch passes" has_ok go-toolchain
new preview; printf 'FROM alpine:3.24\n' | set_file Dockerfile; commit_push "no go"; run_rr
chk "11 go-toolchain: Dockerfile without a golang FROM fails" has_fail go-toolchain

# ------------------------------------------------------------ 12 release-record
FIX12_MISSING="copy docs/releases/TEMPLATE.md to docs/releases/v1.2.3.md and fill it in (docs/releasing.md step 4)"
FIX12_BLOB="guide changed; re-read docs/releasing.md and write a new record"
FIX12_SIGN='commit the record with git commit --trailer "Signed-off-by: <your name>", the name only, and do not squash its merge request'
FIX12_OTHER='fix the record to match docs/releasing.md "The signed release record"'
rr12() { glob "$(failline release-record)" "FAIL: release-record: ?* -- $1"; }
new preview; git -C "$WORK" rm -q docs/releases/v1.2.3.md; commit_push "no record"; run_rr
chk "12 release-record: missing file, fix names the template" rr12 "$FIX12_MISSING"
new preview; rec_commit 's/^version: .*/version: v1.2.4/'; run_rr
chk "12 release-record: wrong version" rr12 "$FIX12_OTHER"
chk "12 release-record: wrong version, only that fails" only_fail release-record
new preview; echo "A new paragraph." >>"$WORK/docs/releasing.md"; commit_push "guide edited after the record"; run_rr
chk "12 release-record: older guide blob" rr12 "$FIX12_BLOB"
chk "12 release-record: older guide blob, only that fails" only_fail release-record
new preview; rec_commit 's/^guide-blob: .*/guide-blob: 0000000000000000000000000000000000000000/'; run_rr
chk "12 release-record: guide-blob that is not the guide's" rr12 "$FIX12_BLOB"
new preview; echo "later edit" >>"$WORK/docs/releasing.md"; commit_push "guide edit"; RC_TRAILER=""; rec_commit 's/^(guide-blob: ).*/\10000/'; run_rr
chk "12 release-record: blob fix wins over the unsigned fix" rr12 "$FIX12_BLOB"
new preview; rec_commit '/^guide-commit:/d'; run_rr
chk "12 release-record: guide-commit missing" rr12 "$FIX12_OTHER"
new preview; rec_commit '/^promote-issue:/d'; run_rr
chk "12 release-record: promote-issue missing" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^promote-issue: .*/promote-issue: 1500/'; run_rr
chk "12 release-record: promote-issue without # fails" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^promote-issue: .*/promote-issue: <#N>/'; run_rr
chk "12 release-record: placeholder promote-issue fails" rr12 "$FIX12_OTHER"
new preview; rec_commit '/^step 3:/d'; run_rr
chk "12 release-record: a skipped step line (3 of 4)" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^step 3: .*/step 3: skipped/'; run_rr
chk "12 release-record: a step value that is not done/not-needed/after-merge" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^step 2: .*/step 2: done: <the merge request that bumped VERSION>/'; run_rr
chk "12 release-record: a placeholder value" rr12 "$FIX12_OTHER"
chk "12 release-record: placeholder, only that fails" only_fail release-record
new preview; rec_commit 's/^step 2: .*/step 2: done:/'; run_rr
chk "12 release-record: done with empty text" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^step 2: .*/step 2: not-needed:   /'; run_rr
chk "12 release-record: not-needed with empty text" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^step 2: .*/step 2: done/'; run_rr
chk "12 release-record: bare done" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^step 2: .*/step 2: done: link>/'; run_rr
chk "12 release-record: a > in the text" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^step 2: .*/step 2: not-needed: the owner said so in #1500/'; run_rr
chk "12 release-record: not-needed with text passes" eq "$RC" 0
new preview; rec_commit 's/^step 2:/step X:/;s/^step 3:/step 2:/;s/^step X:/step 3:/'; run_rr
chk "12 release-record: steps out of order" rr12 "$FIX12_OTHER"
new preview; rec_commit 's/^step 3: .*/step 2: done: again\n&/'; run_rr
chk "12 release-record: a duplicated step number" rr12 "$FIX12_OTHER"
new preview; rec_commit '$a step 5: after-merge'; run_rr
chk "12 release-record: more step lines than the guide has" rr12 "$FIX12_OTHER"
new preview; guide_n 6; commit_push "guide with six steps"; run_rr
chk "12 release-record: the guide grew (stale blob) fails" rr12 "$FIX12_BLOB"
new preview; guide_n 6; git -C "$WORK" add -A; git -C "$WORK" commit -q -m "guide six"; rec_commit 's/^(step [56]: ).*/\1after-merge/' 6; run_rr
chk "12 release-record: N comes from the guide (6 steps, 6 lines) passes" has_ok release-record
new preview; guide_n 6; git -C "$WORK" add -A; git -C "$WORK" commit -q -m "guide six"; rec_commit '' 4; run_rr
chk "12 release-record: 6-step guide, 4-step record fails" rr12 "$FIX12_OTHER"
new preview; RC_TRAILER=""; rec_commit ''; run_rr
chk "12 release-record: record line present but commit has no trailer" rr12 "$FIX12_SIGN"
chk "12 release-record: unsigned, only that fails" only_fail release-record
new preview; rec_commit '/^Signed-off-by:/d'; run_rr
chk "12 release-record: no Signed-off-by line in the record" rr12 "$FIX12_SIGN"
new preview; rec_commit 's/^Signed-off-by: .*/Signed-off-by: Release Agent <ra@example.org>/'; RC_TRAILER=""; run_rr
chk "12 release-record: name with an email in it" has_fail release-record
new preview; rec_commit 's/^Signed-off-by: .*/Signed-off-by: <your name, as git config user.name>/'; run_rr
chk "12 release-record: placeholder name" has_fail release-record
new preview; RC_TRAILER="Someone Else"; rec_commit 's/^Signed-off-by: .*/Signed-off-by: Someone Else/'; run_rr
chk "12 release-record: signed name differs from the author name" rr12 "$FIX12_SIGN"
new preview; RC_TRAILER="Other Person"; rec_commit ''; run_rr
chk "12 release-record: record's name is not one of the commit's trailers" rr12 "$FIX12_SIGN"
new preview; RC_AUTHOR="Other Author"; RC_TRAILER="Release Agent"; rec_commit ''; run_rr
chk "12 release-record: author name differs from the signed name" rr12 "$FIX12_SIGN"
new preview; RC_AUTHOR="Other Author"; RC_TRAILER="Other Author"; rec_commit 's/^Signed-off-by: .*/Signed-off-by: Other Author/'; run_rr
chk "12 release-record: a different author who signs as themselves passes" has_ok release-record
new preview; echo unrelated >"$WORK/unrelated.txt"; commit_push "unrelated, no trailer"; run_rr
chk "12 release-record: a later commit not touching the record may be unsigned" has_ok release-record
new preview; RC_TRAILER=""; rec_commit 's/$/ /'; run_rr
chk "12 release-record: the last commit that touched the record decides" rr12 "$FIX12_SIGN"
new main; run_rr
chk "12 release-record: main leg reads it from preview and passes" has_ok release-record

# ---------------------------------------------------- several fail, all reported
new preview
add_tag v1.2.4
printf '# Changelog\n\n## [Unreleased]\n\n- Left.\n\n## [1.2.3] - 2026-11-12\n\n- Thing.\n' | set_file CHANGELOG.md; commit_push "dirty changelog"
ISSUE_BODY='{"iid":1426,"state":"opened"}'
PIPE_BODY='[{"id":9,"status":"failed","web_url":"http://gl.example.org/p/9"}]'
GO_BODY='[{"version":"go1.27.5","stable":true}]'
run_rr
chk "several: exit 1" eq "$RC" 1
chk "several: five of twelve failed" has "$OUT" "release-readiness: 5 of 12 checks failed:"
for id in version changelog review-issue source-pipeline go-toolchain; do
  chk "several: $id reported in the closing block" has "$OUT" "  $(failline $id)"
done
chk "several: closing block has exactly five lines" eq "$(fails_in_block)" 5
chk "several: seven others still ran and passed" eq "$(okcount)" 7
chk "several: closing block is in check order" eq "$(grep '^  FAIL: ' <<<"$OUT" | sed -E 's/^  FAIL: ([a-z0-9-]+):.*/\1/' | tr '\n' ' ')" "version changelog review-issue source-pipeline go-toolchain "
chk "several: no Ready line" hasnt "$OUT" "Ready ("
chk "several: header line present" has "$OUT" "release-readiness: target preview, source dev at $(head12), version 1.2.3"
chk "several: the block comes after the per-check lines" eq "$(grep -n 'checks failed:' <<<"$OUT" | cut -d: -f1 | head -1 | awk -v t="$(grep -n '^ok: release-record' <<<"$OUT" | cut -d: -f1)" '{print ($1 > t)}')" 1

# --------------------------------------------- a check that cannot run, run goes on
new preview; API_URL="http://127.0.0.1:1/api/v4"; run_rr
chk "cannot run: API unreachable, exit 1" eq "$RC" 1
for id in review-issue milestone source-pipeline; do chk "cannot run: $id says could not run" glob "$(failline $id)" "FAIL: $id: could not run: *"; done
chk "cannot run: the nine other checks still ran and passed" eq "$(okcount)" 9
chk "cannot run: three failed" has "$OUT" "release-readiness: 3 of 12 checks failed:"
API_URL=""

echo "# $COUNT checks"
exit "$fail"
