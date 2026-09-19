#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises install.sh's argument/env handling against a stub `docker`
# placed first on PATH, one case per behaviour #1242's ruling names:
# default image/version, version from the first argument, version from
# MIKROVIEW_VERSION, MIKROVIEW_IMAGE skipping the pull, the port env
# overrides landing in the run line, an existing container being
# stopped and removed before re-creation, and a missing docker refusing
# plainly. Same reasoning as assert-ui-built.test.sh: a script nobody
# has watched run is not tested.
#
# The stub also fakes curl (always "answers" healthz on the first try)
# so the suite does not sit through install.sh's real 30s wait loop --
# real docker and real curl are both on this host, but nothing in here
# is actually listening on the ports install.sh prints.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL="$HERE/../install.sh"

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

# stub_bin <dir> <with-docker>: a PATH entry carrying real hostname/awk/
# curl/sleep (via symlink, so install.sh's non-docker calls behave
# normally) plus, unless told otherwise, a fake docker that logs every
# invocation to $DOCKER_LOG and answers just enough to let the script
# run to completion.
stub_bin() {
  local dir="$1" with_docker="$2"
  mkdir -p "$dir"
  for real in hostname awk; do
    ln -s "$(command -v "$real")" "$dir/$real"
  done
  # A no-op sleep, not the real one: nothing here waits on real time, and
  # the only caller (the healthz retry loop) would otherwise cost up to
  # 30 real seconds in the STUB_CURL_RC test below.
  cat >"$dir/sleep" <<'EOF'
#!/bin/sh
exit 0
EOF
  chmod +x "$dir/sleep"
  # A fake curl that answers healthz on the first try unless
  # STUB_CURL_RC says otherwise -- nothing in this suite ever really
  # listens on the ports install.sh prints, so the real curl would sit
  # through all 30 of install.sh's retries. The fake sleep above is what
  # keeps that case (STUB_CURL_RC=1, below) from costing 30 real seconds.
  cat >"$dir/curl" <<'EOF'
#!/bin/sh
exit "${STUB_CURL_RC:-0}"
EOF
  chmod +x "$dir/curl"
  [ "$with_docker" = "true" ] || return 0
  # STUB_INFO_RC/STUB_RUN_RC fake a failing `docker info`/`docker run`;
  # STUB_RUN_ERR is what `docker run` prints to stderr when it fails, so
  # a test can exercise install.sh's port-conflict-hint pattern match
  # against real-looking daemon text instead of an empty message.
  cat >"$dir/docker" <<'EOF'
#!/bin/sh
echo "$*" >>"$DOCKER_LOG"
case "$1" in
  info) exit "${STUB_INFO_RC:-0}" ;;
  pull) exit 0 ;;
  container) [ "$2" = "inspect" ] && { [ "${STUB_EXISTS:-0}" = "1" ] && exit 0 || exit 1; }; exit 0 ;;
  stop|rm) exit 0 ;;
  run)
    rc="${STUB_RUN_RC:-0}"
    [ "$rc" = "0" ] || { [ -z "${STUB_RUN_ERR:-}" ] || echo "${STUB_RUN_ERR}" >&2; }
    exit "$rc"
    ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$dir/docker"
}

# run <label>; runs install.sh with $ARGS under $ENV_VARS (both arrays,
# set by the caller before calling), against a fresh stub PATH. Sets
# rc, out and DOCKER_LOG's contents in $calls.
run() {
  local dir="$TMP/$1"
  stub_bin "$dir" "${WITH_DOCKER:-true}"
  DOCKER_LOG="$dir/docker-calls.log"
  : >"$DOCKER_LOG"
  set +e
  out="$(env -i PATH="$dir" DOCKER_LOG="$DOCKER_LOG" "${ENV_VARS[@]}" "$(command -v sh)" "$INSTALL" "${ARGS[@]}" 2>&1)"
  rc=$?
  set -e
  calls="$(cat "$DOCKER_LOG" 2>/dev/null || true)"
}

# --- default image/version ------------------------------------------------
ARGS=(); ENV_VARS=(); WITH_DOCKER=true
run default
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "default run exits 0 (rc=$rc, out: $out)"
check "$(case "$calls" in *"run -d --name mikroview --restart unless-stopped --read-only --cap-drop ALL --security-opt no-new-privileges --pids-limit 128 -p 6514:6514 -p 443:8080 -v mikroview-data:/var/lib/mikroview -v mikroview-etc:/etc/mikroview ghcr.io/tomlawesome/mikroview:latest"*) echo true;; *) echo false;; esac)" \
  "default run line: latest, mikroview name, both named volumes, 6514/443"

# --- hardening flags (#1286): each must appear on the run line -------------
for flag in "--read-only" "--cap-drop ALL" "--security-opt no-new-privileges" "--pids-limit 128"; do
  check "$(case "$calls" in *"$flag"*) echo true;; *) echo false;; esac)" \
    "hardening flag on the run line: $flag"
done

# --- version from the first argument --------------------------------------
ARGS=(v0.6.0); ENV_VARS=(); WITH_DOCKER=true
run arg-version
check "$(case "$calls" in *"ghcr.io/tomlawesome/mikroview:v0.6.0"*) echo true;; *) echo false;; esac)" \
  "a first argument sets the version (v0.6.0)"

# --- version from MIKROVIEW_VERSION ----------------------------------------
ARGS=(); ENV_VARS=(MIKROVIEW_VERSION=v0.7.0); WITH_DOCKER=true
run env-version
check "$(case "$calls" in *"ghcr.io/tomlawesome/mikroview:v0.7.0"*) echo true;; *) echo false;; esac)" \
  "MIKROVIEW_VERSION sets the version when no argument is given"

# --- MIKROVIEW_IMAGE skips the pull ----------------------------------------
ARGS=(); ENV_VARS=(MIKROVIEW_IMAGE=registry.example/mikroview:custom); WITH_DOCKER=true
run image-override
check "$(case "$calls" in *"pull "*) echo false;; *) echo true;; esac)" \
  "MIKROVIEW_IMAGE skips the pull entirely (no pull line in the docker log)"
check "$(case "$calls" in *"registry.example/mikroview:custom"*) echo true;; *) echo false;; esac)" \
  "MIKROVIEW_IMAGE's exact reference reaches the run line"

# --- port env overrides appear in the run line ------------------------------
ARGS=(); ENV_VARS=(MIKROVIEW_HTTPS_PORT=8443 MIKROVIEW_SYSLOG_PORT=16514); WITH_DOCKER=true
run port-override
check "$(case "$calls" in *"-p 16514:6514 -p 8443:8080"*) echo true;; *) echo false;; esac)" \
  "MIKROVIEW_HTTPS_PORT/MIKROVIEW_SYSLOG_PORT land in the run line"

# --- an existing container is stopped/removed before re-creation -----------
ARGS=(); ENV_VARS=(STUB_EXISTS=1); WITH_DOCKER=true
run existing-container
check "$(case "$calls" in *$'\n'"stop mikroview"$'\n'*) echo true;; *) echo false;; esac)" \
  "an existing container is stopped"
check "$(case "$calls" in *$'\n'"rm mikroview"$'\n'*) echo true;; *) echo false;; esac)" \
  "and removed, before the run line (both appear ahead of it in the log)"
check "$(case "$calls" in *"rm mikroview"$'\n'*"run -d"*) echo true;; *) echo false;; esac)" \
  "stop/rm happen before the new run, not after"

# --- container name drives both volume names --------------------------------
ARGS=(); ENV_VARS=(MIKROVIEW_CONTAINER=mikroview-ci); WITH_DOCKER=true
run container-name
check "$(case "$calls" in *"--name mikroview-ci"*"-v mikroview-ci-data:/var/lib/mikroview -v mikroview-ci-etc:/etc/mikroview"*) echo true;; *) echo false;; esac)" \
  "MIKROVIEW_CONTAINER renames the container and both named volumes"

# --- no existing container: nothing is stopped or removed ------------------
ARGS=(); ENV_VARS=(); WITH_DOCKER=true
run no-existing-container
check "$(case "$calls" in *$'\n'"stop mikroview"$'\n'*) echo false;; *) echo true;; esac)" \
  "no existing container: nothing is stopped"
check "$(case "$calls" in *$'\n'"rm mikroview"$'\n'*) echo false;; *) echo true;; esac)" \
  "no existing container: nothing is removed"

# --- docker installed but daemon unreachable --------------------------------
ARGS=(); ENV_VARS=(STUB_INFO_RC=1); WITH_DOCKER=true
run daemon-unreachable
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "docker info failing exits non-zero (rc=$rc)"
check "$(case "$out" in *"not answering"*) echo true;; *) echo false;; esac)" \
  "and says plainly that docker is installed but not answering"
check "$(case "$calls" in *"pull"*) echo false;; *) echo true;; esac)" \
  "and never reaches the pull (info is checked first)"

# --- docker run fails, no recognizable pattern ------------------------------
ARGS=(); ENV_VARS=(STUB_RUN_RC=1 STUB_RUN_ERR="docker: some other daemon error."); WITH_DOCKER=true
run run-fails-generic
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a failing docker run exits non-zero (rc=$rc)"
check "$(case "$out" in *"some other daemon error"*) echo true;; *) echo false;; esac)" \
  "docker run's own stderr reaches the operator"
check "$(case "$out" in *"port conflict"*) echo false;; *) echo true;; esac)" \
  "an unrecognized failure gets no port-conflict hint"
check "$(case "$calls" in *$'\n'"rm -f mikroview"*) echo true;; *) echo false;; esac)" \
  "the half-created container is cleaned up with rm -f after a failed run"

# --- docker run fails on a port conflict: the operator gets the hint -------
ARGS=(); ENV_VARS=(STUB_RUN_RC=125 STUB_RUN_ERR="docker: Error response from daemon: driver failed programming external connectivity: Bind for 0.0.0.0:443 failed: port is already allocated."); WITH_DOCKER=true
run run-fails-port-conflict
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a port-conflict run failure exits non-zero (rc=$rc)"
check "$(case "$out" in *"port conflict"*"MIKROVIEW_HTTPS_PORT=8443"*) echo true;; *) echo false;; esac)" \
  "a bind/already-allocated failure gets the port-conflict hint with the retry command"
check "$(case "$calls" in *$'\n'"rm -f mikroview"*) echo true;; *) echo false;; esac)" \
  "the half-created container is cleaned up here too"

# --- healthz never answers: install.sh warns but still exits 0 -------------
ARGS=(); ENV_VARS=(STUB_CURL_RC=1); WITH_DOCKER=true
run healthz-never-answers
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "a healthz that never answers is a warning, not a failure (rc=$rc)"
check "$(case "$out" in *"healthz did not answer within 30s"*) echo true;; *) echo false;; esac)" \
  "and says so, pointing at docker logs"

# --- missing docker refuses plainly with a non-zero exit -------------------
ARGS=(); ENV_VARS=(); WITH_DOCKER=false
run missing-docker
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "no docker on PATH exits non-zero (rc=$rc)"
check "$(case "$out" in *"docker is not installed"*) echo true;; *) echo false;; esac)" \
  "and says plainly that docker is not installed"

echo
if [ "$fails" -ne 0 ]; then
  echo "install.test.sh: $fails check(s) failed"
  exit 1
fi
echo "install.test.sh: all checks passed"
