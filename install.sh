#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only
#
# The one-line pastable install (#1242):
#   curl -fsSL https://raw.githubusercontent.com/tomlawesome/mikroview/main/install.sh | sh
#
# It runs one `docker run` for mikroview itself, and shows it first --
# the whole effect fits on one screen, which is the trust story: no
# checksum step, because the script is short enough to read instead. Not
# a Compose writer: docs/install.md's "no-script form" is this same
# line, for anyone who would rather paste it by hand.
#
# Two more, throwaway `docker run`s (#1357) come before it on a fresh
# install, to give on-disk event history -- on by default since #1357 --
# a key to encrypt under. See the comment above that step.
#
# MIKROVIEW_IMAGE and MIKROVIEW_CONTAINER are deliberately undocumented
# for users -- they exist only for scripts/install.test.sh and the CI
# job (test:install-line) that builds a throwaway image and needs a
# throwaway container name so it never touches a real "mikroview".
set -eu

version="${1:-${MIKROVIEW_VERSION:-latest}}"
image="${MIKROVIEW_IMAGE:-ghcr.io/tomlawesome/mikroview:${version}}"
name="${MIKROVIEW_CONTAINER:-mikroview}"
data_vol="${name}-data"
etc_vol="${name}-etc"
https_port="${MIKROVIEW_HTTPS_PORT:-443}"
syslog_port="${MIKROVIEW_SYSLOG_PORT:-6514}"

if ! command -v docker >/dev/null 2>&1; then
  echo "install.sh: docker is not installed. Get it from https://docs.docker.com/engine/install/ and run this again." >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "install.sh: docker is installed but not answering. Is the daemon running, and can this user reach it (docker group, or rootless setup)?" >&2
  exit 1
fi

# MIKROVIEW_IMAGE names an image that is already local (the test-only path).
if [ -z "${MIKROVIEW_IMAGE:-}" ]; then
  echo "install.sh: pulling $image"
  docker pull "$image"
  # #1282: name the exact bytes just pulled, and point at the verification
  # story SECURITY.md already documents for them (cosign verify against
  # this same digest). Never fail the install over this -- an inspect
  # that errors or comes back empty (offline registry mirrors, older
  # Docker) just means no line gets printed.
  # #1309: a release carries two independent signatures and both must
  # verify, so the line says two rather than sending the reader off
  # expecting one.
  digest="$(docker inspect --format '{{index .RepoDigests 0}}' "$image" 2>/dev/null)" || digest=""
  if [ -n "$digest" ]; then
    echo "install.sh: pulled $digest -- before you trust it, see SECURITY.md and cosign verify that digest. A release carries two independent signatures and both must verify."
  fi
fi

# Same line is the upgrade line, and running it twice is harmless
# (#1240 then tells the operator what changed): stop and remove any
# earlier container by this name, re-created below on the same volumes.
removed_existing=0
if docker container inspect "$name" >/dev/null 2>&1; then
  echo "install.sh: removing existing container $name (its volumes come back on the new one)"
  docker stop "$name" >/dev/null
  docker rm "$name" >/dev/null
  removed_existing=1
fi

# On-disk event history is on by default (#1357), and there is no
# unencrypted mode -- it needs a key mounted at keys/history.key in the
# app folder volume, or it simply stays off (docs/configuration.md).
# Give a fresh install one, the same way docs/configuration.md tells an
# operator to make one by hand, but generated inside a throwaway helper
# container rather than on this shell: the key material then never
# exists on the command line, in this script's own output, or on the
# host running it, only inside the volume. Never overwrites one already
# there, so an upgrade (this is also the upgrade line, above) leaves
# whatever key it is already encrypted under alone.
#
# Two docker runs rather than one -- check, then create -- because the
# check's own exit code is the "does it already exist" answer: nothing
# printed by either has to be parsed to find out.
if docker run --rm -v "${etc_vol}:/etc/mikroview:ro" alpine:3.24 \
  test -e /etc/mikroview/keys/history.key >/dev/null 2>&1; then
  : # already there -- told about it on an earlier run
elif docker run --rm -v "${etc_vol}:/etc/mikroview" alpine:3.24 sh -c '
    set -e
    mkdir -p /etc/mikroview/keys
    head -c 32 /dev/urandom | base64 > /etc/mikroview/keys/history.key
    chown 1000:1000 /etc/mikroview/keys/history.key
    chmod 600 /etc/mikroview/keys/history.key
  '; then
  echo "install.sh: on-disk event history is on by default -- a key for it now lives in the ${etc_vol} volume at keys/history.key. Back it up: losing it makes the retained history unreadable."
else
  echo "install.sh: could not create a history encryption key in ${etc_vol} -- mikroview will start with on-disk history off until one is mounted (see docs/configuration.md)" >&2
fi

# Two named volumes: the data store, and the app folder #1243 taught the
# binary to read config, GeoIP and a certificate pair from -- see
# docs/install.md for putting a file there once you want one. Not with
# docker cp: the same folder is mounted :ro below, and Docker refuses a
# copy into it with "mounted volume is marked read-only". A helper
# container writing to the volume, or a bind-mount swap, is the way --
# the history key above just did exactly that.
#
# Hardening (#1286): the same flags deploy/docker-compose.yml's hardening
# block applies, kept in sync by scripts/check-release-surfaces.sh so the
# two can't drift apart again. The app folder is mounted read-only, as
# Compose has always mounted it and as internal/config/appfolder.go
# describes it ("a folder an operator mounts read-only... nothing else
# writes to it"): config, the GeoIP database and the certificate are put
# there by the operator; the history key above is the one exception,
# made by this script rather than by hand, so a fresh install needs no
# separate setup step for it. No memory or CPU cap here on purpose --
# both depend on the host this runs on, a wrong one is a silent outage on
# a small box, and Compose (where an operator already sets the rest of
# their deployment) is the right place to choose one.
set -- run -d --name "$name" --restart unless-stopped \
  --read-only --cap-drop ALL --security-opt no-new-privileges --pids-limit 128 \
  -p "${syslog_port}:6514" -p "${https_port}:8080" \
  -v "${data_vol}:/var/lib/mikroview" -v "${etc_vol}:/etc/mikroview:ro" \
  "$image"
echo "docker $*"

if ! out=$(docker "$@" 2>&1); then
  docker rm -f "$name" >/dev/null 2>&1 || true
  echo "install.sh: $out" >&2
  if [ "$removed_existing" -eq 1 ]; then
    echo "install.sh: the previous $name container is already gone (removed above) -- its data is untouched in the $data_vol and $etc_vol volumes, and re-running this same command recreates it." >&2
  fi
  case "$out" in
    *bind*|*"already allocated"*|*"permission denied"*)
      echo "install.sh: that looks like a port conflict -- ${https_port} may need root (rootless Docker can't bind under 1024), or something else already has it. Try:" >&2
      echo "  curl -fsSL https://raw.githubusercontent.com/tomlawesome/mikroview/main/install.sh | MIKROVIEW_HTTPS_PORT=8443 sh" >&2
      ;;
  esac
  exit 1
fi

if command -v curl >/dev/null 2>&1; then
  up=0
  attempt=1
  while [ "$attempt" -le 30 ]; do
    if curl -fsSk -o /dev/null "https://127.0.0.1:${https_port}/api/healthz" 2>/dev/null; then
      up=1
      break
    fi
    sleep 1
    attempt=$((attempt + 1))
  done
  if [ "$up" -eq 0 ]; then
    echo "install.sh: healthz did not answer within 30s -- check 'docker logs $name'" >&2
  fi
else
  echo "install.sh: curl not found, skipping the healthz wait"
fi

addr="$(hostname -I 2>/dev/null | awk '{print $1}')"
[ -n "$addr" ] || addr="localhost"
port_suffix=""
[ "$https_port" = "443" ] || port_suffix=":${https_port}"
echo "install.sh: open https://${addr}${port_suffix} and create the admin account -- the setup wizard then writes the router commands for you."
echo "install.sh: both ports are open to every network this host is on, and until that first account exists anyone who reaches the page can create it -- do it now, and firewall ${https_port} and ${syslog_port} to the router and your own machines."
echo "install.sh: data lives in the ${data_vol} and ${etc_vol} named volumes."
