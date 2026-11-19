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
  # A no-op chown: the key helper's script chowns to uid 1000, which the
  # sandbox below (a plain directory, not a volume) cannot do unprivileged.
  cat >"$dir/chown" <<'EOF'
#!/bin/sh
exit 0
EOF
  chmod +x "$dir/chown"
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
  #
  # #1357/#1426: history.key runs act on a fake volume ($STUB_ETC, empty
  # by default, so the default across every test below is a fresh
  # install); see the run) branch for STUB_KEY_RC / STUB_KEY_CHECK_RC.
  cat >"$dir/docker" <<'EOF'
#!/bin/sh
echo "$*" >>"$DOCKER_LOG"
case "$1" in
  info) exit "${STUB_INFO_RC:-0}" ;;
  pull) exit 0 ;;
  inspect) [ -z "${STUB_DIGEST:-}" ] || echo "$STUB_DIGEST"; exit "${STUB_INSPECT_RC:-0}" ;;
  container) [ "$2" = "inspect" ] && { [ "${STUB_EXISTS:-0}" = "1" ] && exit 0 || exit 1; }; exit 0 ;;
  stop|rm) exit 0 ;;
  run)
    # Any run naming history.key is a fake volume: STUB_KEY_RC is a
    # docker-level failure (nothing executed); the old separate
    # `test -e` check answers STUB_KEY_CHECK_RC or the sandbox; any other
    # helper script is run for real against $STUB_ETC standing in for
    # /etc/mikroview.
    case "$*" in
      *history.key*)
        PATH="${0%/*}:/usr/bin:/bin"
        [ -z "${STUB_KEY_RC:-}" ] || exit "$STUB_KEY_RC"
        case "$*" in
          *"test -e /etc/mikroview/keys/history.key"*)
            [ -z "${STUB_KEY_CHECK_RC:-}" ] || exit "$STUB_KEY_CHECK_RC"
            test -e "$STUB_ETC/keys/history.key"; exit $? ;;
        esac
        eval "script=\${$#}"
        script="$(printf '%s' "$script" | sed "s#/etc/mikroview#$STUB_ETC#g")"
        sh -c "$script"
        exit $?
        ;;
    esac
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
  STUB_ETC="$dir/etc"
  mkdir -p "$STUB_ETC"
  set +e
  out="$(env -i PATH="$dir" DOCKER_LOG="$DOCKER_LOG" STUB_ETC="$STUB_ETC" "${ENV_VARS[@]}" "$(command -v sh)" "$INSTALL" "${ARGS[@]}" 2>&1)"
  rc=$?
  set -e
  calls="$(cat "$DOCKER_LOG" 2>/dev/null || true)"
}

# --- default image/version ------------------------------------------------
ARGS=(); ENV_VARS=(); WITH_DOCKER=true
run default
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "default run exits 0 (rc=$rc, out: $out)"
check "$(case "$calls" in *"run -d --name mikroview --restart unless-stopped --read-only --cap-drop ALL --security-opt no-new-privileges --pids-limit 128 -p 6514:6514 -p 443:8080 -p 47022:47022 -v mikroview-data:/var/lib/mikroview -v mikroview-etc:/etc/mikroview:ro ghcr.io/tomlawesome/mikroview:latest"*) echo true;; *) echo false;; esac)" \
  "default run line: latest, mikroview name, both named volumes, 6514/443/47022"
check "$(case "$out" in *"SECURITY.md"*) echo false;; *) echo true;; esac)" \
  "#1282: no resolvable digest (stub inspect prints nothing) means no SECURITY.md line"

# --- #1282: a resolved digest is printed with a pointer to SECURITY.md -----
ARGS=(); ENV_VARS=(STUB_DIGEST="ghcr.io/tomlawesome/mikroview@sha256:deadbeef"); WITH_DOCKER=true
run digest-resolved
check "$(case "$out" in *"ghcr.io/tomlawesome/mikroview@sha256:deadbeef"*"SECURITY.md"*"cosign verify"*) echo true;; *) echo false;; esac)" \
  "the resolved digest is printed, pointing at SECURITY.md's cosign verify"

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

# --- #1361: the router-backup drop box's port is always published,
# and MIKROVIEW_BACKUP_PORT can move it the same way the other two do ---
ARGS=(); ENV_VARS=(); WITH_DOCKER=true
run backup-port-default
check "$(case "$calls" in *"-p 47022:47022"*) echo true;; *) echo false;; esac)" \
  "the router-backup drop box's port (47022) is published by default"
check "$(case "$out" in *"nothing answers"*"Settings"*) echo true;; *) echo false;; esac)" \
  "the final output warns that nothing answers on it until an admin opens the drop box"

ARGS=(); ENV_VARS=(MIKROVIEW_BACKUP_PORT=17022); WITH_DOCKER=true
run backup-port-override
check "$(case "$calls" in *"-p 17022:47022"*) echo true;; *) echo false;; esac)" \
  "MIKROVIEW_BACKUP_PORT lands in the run line"

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
check "$(case "$calls" in *"--name mikroview-ci"*"-v mikroview-ci-data:/var/lib/mikroview -v mikroview-ci-etc:/etc/mikroview:ro"*) echo true;; *) echo false;; esac)" \
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
check "$(case "$out" in *"already gone"*) echo false;; *) echo true;; esac)" \
  "no existing container was removed, so no recovery message about one being gone"

# --- R3 (#1304): a run failure after the old container was already removed -
# says plainly that it is gone, that the data volumes are untouched, and
# that re-running the script recovers.
ARGS=(); ENV_VARS=(STUB_EXISTS=1 STUB_RUN_RC=1 STUB_RUN_ERR="docker: some other daemon error."); WITH_DOCKER=true
run run-fails-after-existing-removed
check "$([ "$rc" -ne 0 ] && echo true || echo false)" "a failing run after removing the old container still exits non-zero (rc=$rc)"
check "$(case "$out" in *"already gone"*"mikroview-data"*"mikroview-etc"*"re-running"*) echo true;; *) echo false;; esac)" \
  "says the previous container is already gone, its data is untouched in both volumes, and re-running recovers"

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

# --- #1357: a fresh install gets a history key before mikroview starts -----
ARGS=(); ENV_VARS=(); WITH_DOCKER=true
run fresh-history-key
KEYF="$TMP/fresh-history-key/etc/keys/history.key"
check "$(case "$calls" in *"run --rm -v mikroview-etc:/etc/mikroview alpine:3.24 sh -c"*"history.key"*"set -C"*) echo true;; *) echo false;; esac)" \
  "one writable helper container checks for and creates the key under noclobber (set -C)"
check "$(case "$calls" in *":ro alpine:3.24 test -e"*) echo false;; *) echo true;; esac)" \
  "no separate :ro key-exists check is made"
check "$([ -f "$KEYF" ] && [ "$(wc -c <"$KEYF")" -eq 45 ] && echo true || echo false)" \
  "a fresh install leaves a 44-char base64 key plus newline at keys/history.key"
# history.key appears in the helper's own logged call; the position check
# must be on the helper call, so look at the first line mentioning it.
first_key_line="$(printf '%s\n' "$calls" | grep -n 'history.key' | head -n1 | cut -d: -f1)"
run_d_line="$(printf '%s\n' "$calls" | grep -n 'run -d --name mikroview' | head -n1 | cut -d: -f1)"
check "$([ -n "$first_key_line" ] && [ -n "$run_d_line" ] && [ "$first_key_line" -lt "$run_d_line" ] && echo true || echo false)" \
  "the key helper runs before mikroview's own run -d line"
check "$(case "$calls" in *"sh -c"*"history.key"*"run -d --name mikroview"*) echo true;; *) echo false;; esac)" \
  "the create helper (sh -c script naming history.key) itself runs before run -d, not just a check"
check "$(case "$out" in *"a key now lives"*"keys/history.key"*"Back it up"*) echo true;; *) echo false;; esac)" \
  "the operator is told a key now lives, where, and to back it up"
check "$(case "$out" in *"will not start without it"*) echo true;; *) echo false;; esac)" \
  "the operator is told mikroview will not start without the key"
check "$(case "$out" in *"locks you out"*"no way to recover a lost key"*) echo true;; *) echo false;; esac)" \
  "the operator is told losing it locks them out and a lost key cannot be recovered"
check "$(case "$out" in *"makes the retained history unreadable"*) echo false;; *) echo true;; esac)" \
  "the old understated 'makes the retained history unreadable' wording is gone"
check "$(case "$out" in *"accounts"*"API tokens"*"recovery keys"*"sign-in history"*"event history"*) echo true;; *) echo false;; esac)" \
  "the message names what the key seals: accounts, API tokens, recovery keys, sign-in history, event history"

# --- #1357: an already-mounted key is left alone and not re-announced ------
mkdir -p "$TMP/existing-history-key/etc/keys"
printf OLDKEY >"$TMP/existing-history-key/etc/keys/history.key"
ARGS=(); ENV_VARS=(); WITH_DOCKER=true
run existing-history-key
check "$([ "$(cat "$TMP/existing-history-key/etc/keys/history.key")" = "OLDKEY" ] && echo true || echo false)" \
  "an existing key is left byte-for-byte unchanged"
check "$(case "$out" in *"Back it up"*|*"locks you out"*|*"a key now lives"*) echo false;; *) echo true;; esac)" \
  "and nothing is printed about it -- it was told on an earlier run"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "an upgrade run over an existing key exits 0 (rc=$rc)"

# --- #1426: the key check failing at docker level never replaces the key ---
mkdir -p "$TMP/key-check-fails/etc/keys"
printf OLDKEY >"$TMP/key-check-fails/etc/keys/history.key"
ARGS=(); ENV_VARS=(STUB_KEY_CHECK_RC=125); WITH_DOCKER=true
run key-check-fails
check "$([ "$(cat "$TMP/key-check-fails/etc/keys/history.key")" = "OLDKEY" ] && echo true || echo false)" \
  "a failing key check (daemon error) does not replace the existing key"
check "$(case "$out" in *"Back it up"*) echo false;; *) echo true;; esac)" \
  "and no fresh-key announcement is printed"
check "$(case "$out" in *"could not"*) echo false;; *) echo true;; esac)" \
  "and no failure is reported, as the key was found"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "and the install exits 0 (rc=$rc)"

# --- #1426: the single key helper failing at docker level is named ---------
mkdir -p "$TMP/key-helper-docker-fails/etc/keys"
printf OLDKEY >"$TMP/key-helper-docker-fails/etc/keys/history.key"
ARGS=(); ENV_VARS=(STUB_KEY_RC=125); WITH_DOCKER=true
run key-helper-docker-fails
check "$([ "$(cat "$TMP/key-helper-docker-fails/etc/keys/history.key")" = "OLDKEY" ] && echo true || echo false)" \
  "a key helper that fails at docker level leaves the existing key alone"
check "$(case "$out" in *"could not check for or create a history encryption key in mikroview-etc"*"exited 125"*"nothing in the volume was changed"*) echo true;; *) echo false;; esac)" \
  "and the failure names the exit code and says nothing in the volume was changed"
check "$(case "$out" in *"Back it up"*) echo false;; *) echo true;; esac)" \
  "and no fresh-key announcement is printed"
check "$([ "$rc" -eq 0 ] && echo true || echo false)" "and the install goes on to the run line (rc=$rc)"

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
