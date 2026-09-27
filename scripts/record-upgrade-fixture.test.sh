#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/record-upgrade-fixture.sh (#1310) against a stub
# `docker` -- no daemon, no real containers, no network -- that records
# every invocation and fakes just enough output for the script to run
# to completion. Same reasoning as check-upgrade-fixtures-recorded.
# test.sh: a check nobody has watched fail is not a check.
#
# What this catches, and what it cannot:
#
#   - It proves the script never asks a `docker run`/`docker create` to
#     bind-mount a host path (a `-v` whose source starts with `/`) --
#     only named volumes. That is the actual defect (#1310): in CI the
#     job container and the daemon it talks to are different
#     filesystems, so a host-path `-v` names a path the daemon cannot
#     see and gets an empty directory instead. This assertion is
#     environment-independent -- it holds locally exactly because it
#     would also hold against the CI daemon -- so it does not require
#     reproducing the remote-daemon split itself, which a local stub
#     genuinely cannot do (this docker and the one running it are the
#     same daemon).
#   - It proves that for a version enforcing #1281's connection gate
#     (v0.6.0 on), a config.yaml declaring the fixture router is
#     delivered to /etc/mikroview before the container starts. It
#     cannot prove the *server* then accepts the syslog line -- that
#     needs the real image, exercised by the real runs recorded in
#     #1310's report, not a stub.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fails=0
check() {
  if [ "$1" = "true" ]; then
    echo "  ok   $2"
  else
    echo "  FAIL $2"
    fails=$((fails + 1))
  fi
}

# stub_docker <dir> <log-file> <capture-dir> -- writes a fake `docker`
# that appends one tab-separated line per invocation to <log-file> and:
#   - on `docker cp ... <container>:/etc/mikroview`, copies the source
#     directory's contents into <capture-dir>, so the test can inspect
#     what was staged after the real one is gone (the script's own
#     cleanup trap removes it on exit).
#   - on any `docker run`, consumes stdin (the real script pipes
#     session.py in over it) and prints a fixed manifest, standing in
#     for upgrade-fixture-session.py's real output.
#   - anything else (rm, volume, network, create, start, stop, logs)
#     just succeeds, the way an idle throwaway container would.
stub_docker() {
  local dir="$1" log="$2" capture="$3"
  mkdir -p "$dir"
  cat > "$dir/docker" <<EOF
#!/bin/sh
LOG="$log"
CAPTURE="$capture"
{ for a in "\$@"; do printf '%s\t' "\$a"; done; printf '\n'; } >> "\$LOG"
case "\$1" in
  cp)
    case "\$3" in
      *:/etc/mikroview)
        mkdir -p "\$CAPTURE"
        cp -a "\$2" "\$CAPTURE/" 2>/dev/null || true
        ;;
    esac
    exit 0
    ;;
  run)
    cat >/dev/null
    cat <<'JSON'
{"version":"v0.6.0","recordedAt":"2026-01-01T00:00:00Z","adminUsername":"upgrade-fixture-admin","viewerUsername":"upgrade-fixture-viewer","apiTokenName":"upgrade-fixture-token","entity":{"type":"host","key":"172.20.30.40","label":"upgrade-fixture-host"},"watchlist":{"name":"upgrade-fixture-watch","ports":[443]},"declaredRouter":{"id":"upgrade-fixture-router","sourceIp":"127.0.0.1"},"flag":{"type":"new_device","target":"aa:bb:cc:00:ff:10"},"coverage":{"key":"upgrade-fixture-boundary","reason":"upgrade-fixture-coverage-reason"},"hostMark":{"key":"ether1|172.20.30.40","kind":"intended","reason":"upgrade-fixture-host-reason"}}
JSON
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
EOF
  chmod +x "$dir/docker"
}

# stub_docker_failing_session <dir> <log-file> -- same as stub_docker
# above except `docker run` (the scripted session) exits 1 with no
# stdout, standing in for a broken session -- e.g. #1358's "this account
# has no second factor" -- so --check's job is to fail the pipeline, not
# print a manifest.
stub_docker_failing_session() {
  local dir="$1" log="$2"
  mkdir -p "$dir"
  cat > "$dir/docker" <<EOF
#!/bin/sh
LOG="$log"
{ for a in "\$@"; do printf '%s\t' "\$a"; done; printf '\n'; } >> "\$LOG"
case "\$1" in
  run)
    cat >/dev/null
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
EOF
  chmod +x "$dir/docker"
}

# A scratch copy, not the real repo: record-upgrade-fixture.sh writes
# testdata/upgrade/<version>/manifest.json and .upgrade-fixtures/ next
# to itself (ROOT is derived from $0), and this run's manifest is fake
# -- it must never land next to the real, committed one.
SCRATCH="$TMP/scratch"
mkdir -p "$SCRATCH/scripts"
cp "$HERE/record-upgrade-fixture.sh" "$SCRATCH/scripts/"
cp "$HERE/upgrade-fixture-session.py" "$SCRATCH/scripts/"
chmod +x "$SCRATCH/scripts/record-upgrade-fixture.sh"
# --check reads this instead of taking a version argument (#1358 item 3).
echo "0.6.1" > "$SCRATCH/VERSION"

BIN="$TMP/bin"
LOG="$TMP/docker.log"
CAPTURE="$TMP/etc-capture"
stub_docker "$BIN" "$LOG" "$CAPTURE"

run_recorder() { # run_recorder <version>
  local version="$1"
  : > "$LOG"
  rm -rf "$CAPTURE"
  set +e
  out="$(cd "$SCRATCH" && env -u CI_JOB_TOKEN -u CI_API_V4_URL -u CI_PROJECT_ID \
    PATH="$BIN:$PATH" MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD=1 \
    bash scripts/record-upgrade-fixture.sh "$version" < /dev/null 2>&1)"
  rc=$?
  set -e
}

# no_host_path_v -- prints one line per `docker run`/`docker create`
# invocation (from $LOG) whose -v source is a host path, empty if none.
no_host_path_v() {
  awk -F'\t' '
    $1 == "run" || $1 == "create" {
      for (i = 1; i <= NF; i++) {
        if ($i == "-v") {
          split($(i + 1), parts, ":")
          if (substr(parts[1], 1, 1) == "/") print
        }
      }
    }
  ' "$LOG"
}

# --- v0.6.0 (generation C, needs both the history key and the
# declared router) records successfully against the stub ------------
run_recorder v0.6.0
if [ "$rc" -ne 0 ]; then
  printf '    %s\n' "${out//$'\n'/$'\n    '}"
fi
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "records v0.6.0 against the stub docker (rc=$rc)"

bad_mounts="$(no_host_path_v)"
if [ -n "$bad_mounts" ]; then
  printf '    %s\n' "${bad_mounts//$'\n'/$'\n    '}"
fi
check "$([ -z "$bad_mounts" ] && echo true || echo false)" \
  "no docker run/create names a host path (starts with /) as a -v source -- only named volumes"

declared_ok=false
if [ -f "$CAPTURE/config.yaml" ] \
  && grep -q 'id: upgrade-fixture-router' "$CAPTURE/config.yaml" \
  && grep -q 'sourceIp: 127.0.0.1' "$CAPTURE/config.yaml"; then
  declared_ok=true
fi
check "$declared_ok" \
  "a config.yaml declaring the fixture router was copied to /etc/mikroview for a generation-C version"

check "$(grep -q 'tls yes' "$LOG" && echo true || echo false)" \
  "the scripted session was told the router was declared (argv ends ... tls yes)"

check "$([ -s "$SCRATCH/testdata/upgrade/v0.6.0/manifest.json" ] && echo true || echo false)" \
  "wrote a manifest for the recorded version"

# --- --check (#1358 item 3): runs against a local image reference
# instead of pulling ghcr.io/tomlawesome/mikroview:<version>, derives its
# feature-generation version from the tree's own VERSION file, and never
# uploads -- exercised here without $MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD
# set at all, so a passing run proves --check enforces that itself
# rather than relying on the caller to remember the env var. ------------
run_check() { # run_check <docker-bin-dir>
  local bindir="$1"
  : > "$LOG"
  rm -rf "$CAPTURE"
  set +e
  out="$(cd "$SCRATCH" && env -u CI_JOB_TOKEN -u CI_API_V4_URL -u CI_PROJECT_ID -u MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD \
    PATH="$bindir:$PATH" \
    bash scripts/record-upgrade-fixture.sh --check local-test-image:rc < /dev/null 2>&1)"
  rc=$?
  set -e
}

rm -rf "$SCRATCH/testdata"
run_check "$BIN"
if [ "$rc" -ne 0 ]; then
  printf '    %s\n' "${out//$'\n'/$'\n    '}"
fi
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "--check records against a local image reference (rc=$rc)"

check "$(echo "$out" | grep -q 'PUT\|packages/generic' && echo false || echo true)" \
  "--check never mentions an upload, even with MIKROVIEW_UPGRADE_FIXTURE_SKIP_UPLOAD unset"

check "$([ -s "$SCRATCH/testdata/upgrade/v0.6.1-check/manifest.json" ] && echo true || echo false)" \
  "--check writes its manifest under a -check-suffixed directory, never the real version's"

check "$([ ! -e "$SCRATCH/testdata/upgrade/v0.6.1/manifest.json" ] && echo true || echo false)" \
  "--check does not touch the real v0.6.1 manifest directory"

# --check refuses --postgres and refuses an explicit version -- both are
# usage mistakes, not something to silently reinterpret.
set +e
bad_out="$(cd "$SCRATCH" && bash scripts/record-upgrade-fixture.sh --check local-test-image:rc --postgres 2>&1)"
bad_rc=$?
set -e
if [ "$bad_rc" -eq 0 ]; then printf '    %s\n' "${bad_out//$'\n'/$'\n    '}"; fi
check "$([ "$bad_rc" -ne 0 ] && echo true || echo false)" "--check --postgres is refused"

set +e
bad_out="$(cd "$SCRATCH" && bash scripts/record-upgrade-fixture.sh --check local-test-image:rc v0.6.1 2>&1)"
bad_rc=$?
set -e
if [ "$bad_rc" -eq 0 ]; then printf '    %s\n' "${bad_out//$'\n'/$'\n    '}"; fi
check "$([ "$bad_rc" -ne 0 ] && echo true || echo false)" "--check with an explicit version argument is refused"

# --- --check fails the pipeline on a broken session, the same as a real
# tag-time recording would (#1358's actual incident) -- and writes no
# manifest, so a red job cannot be mistaken for a recorded check. -------
FAILBIN="$TMP/bin-failing"
FAILLOG="$TMP/docker-failing.log"
stub_docker_failing_session "$FAILBIN" "$FAILLOG"

rm -rf "$SCRATCH/testdata"
run_check "$FAILBIN"
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "--check fails the job when the scripted session fails (rc=$rc)"
check "$([ ! -e "$SCRATCH/testdata/upgrade/v0.6.1-check/manifest.json" ] && echo true || echo false)" \
  "--check writes no manifest when the session failed"

echo
if [ "$fails" -ne 0 ]; then
  echo "record-upgrade-fixture.test.sh: $fails check(s) failed"
  exit 1
fi
echo "record-upgrade-fixture.test.sh: all checks passed"
