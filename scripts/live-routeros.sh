#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Boots a real RouterOS router -- MikroTik's own CHR image, under QEMU --
# so changes that depend on what RouterOS actually does can be checked
# against RouterOS rather than against an assumption about it.
#
# This exists because "no RouterOS device is available here" was written
# into #186 as a permanent limitation, and it was wrong. CHR virtualises,
# and booting one immediately produced answers that the guesses had wrong:
# `/tool fetch` refuses a POST body over ~64KiB, `:serialize to=json`
# does not exist before 7.13, and a router user with nothing but `read`
# can print a script's source -- ingest token included.
#
# No root and no host packages: QEMU runs in a container built from
# live-routeros.dockerfile. /dev/kvm is used when the calling user can
# open it and TCG software emulation when they cannot, which costs
# surprisingly little -- CHR reaches its login prompt in about 15s either
# way.
#
# Usage:
#   eval "$(scripts/live-routeros.sh up)"        # exports MVCHR_*
#   scripts/live-routeros.sh run '/system resource print'
#   scripts/live-routeros.sh trust "$MV_URL"     # import mikroview's CA
#   scripts/live-routeros.sh down
set -euo pipefail

CHR_VERSION="${CHR_VERSION:-7.23.3}"
CHR_DIR="${CHR_DIR:-/tmp/mikroview-chr}"
SERIAL_PORT="${MVCHR_SERIAL_PORT:-15901}"
CONTAINER="${MVCHR_CONTAINER:-mikroview-chr}"
QEMU_IMAGE="${MVCHR_QEMU_IMAGE:-mikroview-qemu:local}"
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# CHR's free tier is rate-limited per interface rather than time-limited,
# which is ample for configuration and scripting. Confirmed on 7.23.3:
# `/system license get level` returns "free" on a booted image with no
# licence key applied.
BASE_URL="https://download.mikrotik.com/routeros"

log() { printf '%s\n' "$*" >&2; }

# qemu_image builds the QEMU container once. The build needs
# --network=host: rootless BuildKit's default build network cannot reach
# the Alpine CDN here, while the runtime network can, so the failure
# looks like a broken mirror rather than a sandbox difference.
qemu_image() {
  if ! docker image inspect "$QEMU_IMAGE" >/dev/null 2>&1; then
    log "building $QEMU_IMAGE"
    docker build --network=host -q \
      -f "$REPO/scripts/live-routeros.dockerfile" -t "$QEMU_IMAGE" "$REPO/scripts" >/dev/null
  fi
}

# chr_image downloads and unpacks MikroTik's image into the cache. It is
# never committed: it is 45MiB of someone else's binary, and it is one
# HTTP request to get a fresh one.
chr_image() {
  mkdir -p "$CHR_DIR"
  local zip="$CHR_DIR/chr-$CHR_VERSION.img.zip"
  local img="$CHR_DIR/chr-$CHR_VERSION.img"
  if [ ! -f "$img" ]; then
    log "downloading CHR $CHR_VERSION"
    curl -fsS -o "$zip" "$BASE_URL/$CHR_VERSION/chr-$CHR_VERSION.img.zip"
    unzip -oq "$zip" -d "$CHR_DIR"
  fi
  printf '%s\n' "$img"
}

# accel picks KVM only when this user can genuinely open the device.
# /dev/kvm existing says nothing: it is root:kvm 0660, so on an account
# outside the kvm group it is present and unusable, and reading its
# presence as availability is exactly how #186 came to claim hardware
# virtualisation was on offer here.
accel() {
  if python3 -c "import os,sys; sys.exit(0 if os.access('/dev/kvm', os.R_OK|os.W_OK) else 1)"; then
    printf '%s\n' "kvm"
  else
    printf '%s\n' "tcg"
  fi
}

up() {
  down >/dev/null 2>&1 || true
  qemu_image
  local base scratch mode accel_args=()
  base="$(chr_image)"
  scratch="$CHR_DIR/run-$CHR_VERSION.img"
  # Every run starts from the pristine image. A fixture that inherits the
  # previous run's config is a fixture that passes because of something
  # the change under test did not do.
  cp -f "$base" "$scratch"

  mode="$(accel)"
  if [ "$mode" = kvm ]; then
    accel_args=(--device /dev/kvm)
  fi

  log "booting CHR $CHR_VERSION ($mode)"
  docker run -d --name "$CONTAINER" \
    "${accel_args[@]}" \
    -p "127.0.0.1:$SERIAL_PORT:$SERIAL_PORT" \
    -v "$CHR_DIR:/vm" \
    "$QEMU_IMAGE" \
    -m 512 -smp 2 -accel "$mode" \
    -drive "file=/vm/$(basename "$scratch"),format=raw,if=virtio" \
    -netdev user,id=n0 -device virtio-net-pci,netdev=n0 \
    -display none -serial "tcp:0.0.0.0:$SERIAL_PORT,server=on,wait=off" >/dev/null

  # The port publish is ready slightly after the container is, and
  # connecting into the gap yields a socket that accepts and then says
  # nothing -- which reads as a hung boot.
  sleep 6
  python3 "$REPO/scripts/live-routeros-console.py" --port "$SERIAL_PORT" \
    ':put [/system resource get version]' >/dev/null

  echo "export MVCHR_SERIAL_PORT=$SERIAL_PORT"
  echo "export MVCHR_CONTAINER=$CONTAINER"
  echo "export MVCHR_VERSION=$CHR_VERSION"
}

# run executes console commands and prints what RouterOS printed back.
run() {
  python3 "$REPO/scripts/live-routeros-console.py" --port "$SERIAL_PORT" "$@"
}

# trust imports mikroview's generated CA, which is what makes
# check-certificate=yes work from the router. The download itself runs
# with check-certificate=no because there is nothing to verify against
# yet -- that one bootstrap fetch is the whole reason it is acceptable
# there and nowhere else.
trust() {
  local url="${1:?usage: live-routeros.sh trust <mikroview base url>}"
  run "/tool fetch url=\"$url/ca.crt\" check-certificate=no dst-path=mikroview-ca.crt" \
      '/certificate import file-name=mikroview-ca.crt passphrase=""' \
      '/certificate print detail'
}

# host-addr prints the address the router should use to reach mikroview.
# Not 127.0.0.1: from inside the VM that is the VM, and from inside the
# rootless container that is the container. The host's own LAN address is
# the one address that means the host from all three places.
host_addr() {
  local addr
  addr="$(ip -4 -o addr show scope global | awk '{print $4}' | cut -d/ -f1 | head -1)"
  if [ -z "$addr" ]; then
    log "no global IPv4 address found -- the router has no route to this host"
    exit 1
  fi
  printf '%s\n' "$addr"
}

down() {
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
  rm -f "$CHR_DIR/run-$CHR_VERSION.img"
}

case "${1:-}" in
  up) up ;;
  run) shift; run "$@" ;;
  trust) shift; trust "$@" ;;
  host-addr) host_addr ;;
  down) down ;;
  *) echo "usage: $0 {up|run <command>...|trust <url>|host-addr|down}" >&2; exit 2 ;;
esac
