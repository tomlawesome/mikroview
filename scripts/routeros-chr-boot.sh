#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Boots MikroTik's CHR image straight under software-emulated QEMU
# (-accel tcg) -- no Docker, no /dev/kvm, no privilege of any kind.
#
# This is the CI counterpart of scripts/live-routeros.sh, not a
# replacement for it. That script containerises QEMU so a local,
# unprivileged developer doesn't need root to install it; a GitLab CI
# job container can just have qemu-system-x86 installed into it
# directly (see .gitlab-ci.yml's chr-exercise stage), which makes
# Docker-in-Docker pure overhead there -- and DinD needs a privileged
# runner that this project doesn't have tagged for it. #894 only ever
# exercises RouterOS command syntax, never throughput, so TCG's
# software emulation costs nothing this job needs.
#
# Deliberately narrower than live-routeros.sh: only the up/run/down
# vocabulary scripts/routeros-chr-exercise.sh actually calls. No
# hostfwd ports, because the exercise never sends traffic through the
# router -- only console commands, including one /tool fetch that is
# *expected* to fail at runtime against an unreachable placeholder
# address (see routeros-chr-exercise.sh's header).
#
# Usage:
#   eval "$(scripts/routeros-chr-boot.sh up)"   # exports MVCHR_*
#   scripts/routeros-chr-boot.sh run '/system resource print'
#   scripts/routeros-chr-boot.sh down
set -euo pipefail

CHR_VERSION="${CHR_VERSION:-7.23.3}"
CHR_DIR="${CHR_DIR:-/tmp/mikroview-chr}"
SERIAL_PORT="${MVCHR_SERIAL_PORT:-15901}"
PID_FILE="${MVCHR_PID_FILE:-$CHR_DIR/qemu-$CHR_VERSION.pid}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Same CDN, same override, same "never committed" reasoning as
# live-routeros.sh's chr_image -- see that script for why.
BASE_URL="${MVCHR_BASE_URL:-https://download.mikrotik.com/routeros}"

log() { printf '%s\n' "$*" >&2; }

require_qemu() {
  if ! command -v qemu-system-x86_64 >/dev/null 2>&1; then
    log "routeros-chr-boot: qemu-system-x86_64 not on PATH -- install qemu-system-x86 (and qemu-utils) first"
    exit 2
  fi
}

# chr_image downloads and unpacks MikroTik's image into the cache.
# Mirrors live-routeros.sh's chr_image exactly, including the
# failure-cleanup reasoning in its comments: a truncated download must
# not survive to look like a valid cached image on the next run.
chr_image() {
  mkdir -p "$CHR_DIR"
  local zip="$CHR_DIR/chr-$CHR_VERSION.img.zip"
  local img="$CHR_DIR/chr-$CHR_VERSION.img"
  if [ ! -f "$img" ]; then
    log "downloading CHR $CHR_VERSION"
    # -C - and --retry-all-errors, not just --retry, because of how this
    # actually fails. download.mikrotik.com resets the connection partway
    # through the ~43 MB image rather than refusing it: curl exit 56,
    # around 40 MB in, on the first real run of this job (pipeline 153)
    # and reproducibly on a workstation too. Plain --retry does not cover
    # exit 56 -- it retries timeouts and 5xx -- so the download failed
    # once and stopped, and a job that has to fetch 43 MB every time it
    # runs would keep meeting this.
    #
    # The server sends accept-ranges: bytes and answers a Range request
    # with 206, so resuming is the fix rather than simply trying again
    # from zero: -C - picks up at the byte it stopped at. Confirmed by
    # resuming a download killed at 39.9 MB and getting a zip that
    # unzip -t passes.
    #
    # Even with that, the reset has been reproduced landing inside
    # curl's own --retry budget often enough (#929, two hosts) that one
    # curl invocation still isn't a sure thing. So there is a second,
    # outer retry loop here: up to three whole attempts, a short pause
    # between them, each one resuming (-C -) from whatever the last
    # attempt left in $zip rather than starting over. Three failures in
    # a row is no longer "the network blipped" -- it is a real failure,
    # and the caller (scripts/routeros-chr-exercise.sh, and the
    # chr-exercise:run CI job on top of it) reports it as one rather
    # than as an exercise failure, since nothing about RouterOS itself
    # was even reached.
    #
    # The zip is still removed once all attempts are exhausted, so a
    # partial file never survives the run that produced it.
    local ok=0
    local attempt=1
    while [ "$attempt" -le 3 ]; do
      if curl -fsS -C - --retry 10 --retry-delay 3 --retry-all-errors \
        -o "$zip" "$BASE_URL/$CHR_VERSION/chr-$CHR_VERSION.img.zip"; then
        ok=1
        break
      fi
      log "downloading CHR $CHR_VERSION from $BASE_URL failed (attempt $attempt/3)"
      attempt=$((attempt + 1))
      [ "$attempt" -le 3 ] && sleep 10
    done
    if [ "$ok" -ne 1 ]; then
      rm -f "$zip"
      log "downloading CHR $CHR_VERSION from $BASE_URL failed after 3 attempts -- this fixture needs to reach download.mikrotik.com"
      return 1
    fi
    if ! unzip -oq "$zip" -d "$CHR_DIR"; then
      rm -f "$zip"
      log "the downloaded CHR archive is not a readable zip -- removed, so the next run fetches it again"
      return 1
    fi
    if [ ! -f "$img" ]; then
      log "the CHR archive unpacked without producing $img"
      return 1
    fi
  fi
  printf '%s\n' "$img"
}

up() {
  down >/dev/null 2>&1 || true
  require_qemu
  local base scratch
  if ! base="$(chr_image)"; then
    return 1
  fi
  scratch="$CHR_DIR/run-$CHR_VERSION.img"
  # Every run starts from the pristine image, same reasoning as
  # live-routeros.sh: a fixture that inherits the previous run's
  # config is a fixture that passes because of something the change
  # under test did not do.
  cp -f "$base" "$scratch"

  log "booting CHR $CHR_VERSION (tcg, no docker)"
  qemu-system-x86_64 \
    -m 512 -smp 2 -accel tcg \
    -drive "file=$scratch,format=raw,if=virtio" \
    -netdev user,id=n0 \
    -device virtio-net-pci,netdev=n0 \
    -display none -serial "tcp:127.0.0.1:$SERIAL_PORT,server=on,wait=off" \
    >"$CHR_DIR/qemu-$CHR_VERSION.log" 2>&1 &
  echo $! >"$PID_FILE"

  # Same gap as live-routeros.sh's up(): the serial listener is ready
  # slightly after the process starts, and connecting into that gap
  # yields a socket that accepts and then says nothing.
  sleep 6
  python3 "$REPO/scripts/live-routeros-console.py" --port "$SERIAL_PORT" \
    ':put [/system resource get version]' >/dev/null

  echo "export MVCHR_SERIAL_PORT=$SERIAL_PORT"
  echo "export MVCHR_VERSION=$CHR_VERSION"
}

# run executes console commands and prints what RouterOS printed back.
run() {
  python3 "$REPO/scripts/live-routeros-console.py" --port "$SERIAL_PORT" "$@"
}

down() {
  if [ -f "$PID_FILE" ]; then
    kill "$(cat "$PID_FILE")" >/dev/null 2>&1 || true
    rm -f "$PID_FILE"
  fi
  rm -f "$CHR_DIR/run-$CHR_VERSION.img"
}

case "${1:-}" in
  up) up ;;
  run) shift; run "$@" ;;
  down) down ;;
  *) echo "usage: $0 {up|run <command>...|down}" >&2; exit 2 ;;
esac
