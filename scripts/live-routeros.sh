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

# Two ports forwarded from this host into the router, so traffic can be
# sent *at* the router rather than only originated by it.
#
# Without them the only firewall traffic this fixture can produce is the
# `output` chain -- the router's own /tool fetch and pings -- because
# QEMU user-mode networking forwards nothing inbound by default. That
# leaves untested the two chains an operator actually watches: `input`
# (traffic aimed at the router) and `forward` (traffic transiting it).
# `forward` matters most: internal/routeros/parser.go states it is the
# only chain that reliably carries src-mac, and #243's MAC-preferred
# watchlist identity is built on that claim.
#
# Two ports rather than one because a dstnat rule is what turns inbound
# traffic into transit traffic, and it fires before the input/forward
# decision -- so a single port can produce one chain or the other, never
# both. INPUT_PORT is left alone and lands in `input`; FORWARD_PORT is
# dst-natted onward and lands in `forward`.
#
# GUEST_ADDR is QEMU's user-mode DHCP address for the first guest, fixed
# by SLIRP rather than chosen here, and confirmed against the booted
# router (`/ip address print` shows 10.0.2.15/24 on ether1).
INPUT_PORT="${MVCHR_INPUT_PORT:-15902}"
FORWARD_PORT="${MVCHR_FORWARD_PORT:-15903}"
GUEST_ADDR="10.0.2.15"

# CHR's free tier is rate-limited per interface rather than time-limited,
# which is ample for configuration and scripting. Confirmed on 7.23.3:
# `/system license get level` returns "free" on a booted image with no
# licence key applied.
# Overridable so the download-failure path above can actually be
# exercised. It could not be, which is part of why that path was wrong.
BASE_URL="${MVCHR_BASE_URL:-https://download.mikrotik.com/routeros}"

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
    # Each step checked, and a failed download deleted rather than left
    # in the cache.
    #
    # Without this the failure does not surface here at all: a reset
    # connection leaves a truncated zip, unzip fails, no .img appears,
    # the qemu container starts with no disk, and the first thing that
    # actually stops is the console driver -- reporting "connection
    # refused" on the serial port. Observed exactly that, and the real
    # cause (MikroTik's CDN resetting) was four errors up the log with
    # nothing tying them together.
    if ! curl -fsS --retry 3 --retry-delay 2 -o "$zip" "$BASE_URL/$CHR_VERSION/chr-$CHR_VERSION.img.zip"; then
      rm -f "$zip"
      log "downloading CHR $CHR_VERSION from $BASE_URL failed -- this fixture needs to reach download.mikrotik.com"
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
  # Checked explicitly. `set -e` does not reliably abort on a failing
  # command substitution in an assignment, which is how the truncated
  # download above got all the way to a serial-port error.
  if ! base="$(chr_image)"; then
    return 1
  fi
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
    -p "127.0.0.1:$INPUT_PORT:$INPUT_PORT" \
    -p "127.0.0.1:$FORWARD_PORT:$FORWARD_PORT" \
    -v "$CHR_DIR:/vm" \
    "$QEMU_IMAGE" \
    -m 512 -smp 2 -accel "$mode" \
    -drive "file=/vm/$(basename "$scratch"),format=raw,if=virtio" \
    -netdev "user,id=n0,hostfwd=tcp::$INPUT_PORT-$GUEST_ADDR:$INPUT_PORT,hostfwd=tcp::$FORWARD_PORT-$GUEST_ADDR:$FORWARD_PORT" \
    -device virtio-net-pci,netdev=n0 \
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
  echo "export MVCHR_INPUT_PORT=$INPUT_PORT"
  echo "export MVCHR_FORWARD_PORT=$FORWARD_PORT"
  echo "export MVCHR_ADDR=$GUEST_ADDR"
}

# run executes console commands and prints what RouterOS printed back.
run() {
  python3 "$REPO/scripts/live-routeros-console.py" --port "$SERIAL_PORT" "$@"
}

# run-as is run under a different router account, which is how questions
# about what a low-privilege user can see get answered by asking one
# rather than by reading the policy table.
run_as() {
  local user="${1:?usage: live-routeros.sh run-as <user> <password> <command>...}"
  local password="${2?usage: live-routeros.sh run-as <user> <password> <command>...}"
  shift 2
  python3 "$REPO/scripts/live-routeros-console.py" --port "$SERIAL_PORT" \
    --login "$user" --password "$password" "$@"
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

# setup performs docs/routeros-setup.md steps 1-3 against the booted
# router, so what gets tested is the documented procedure rather than a
# fixture-only shortcut, and then builds a small LAN behind the router so
# there is real router state for step 4 to push.
#
# The LAN half is scaffolding with a purpose. A CHR under QEMU has one
# interface and nothing behind it, so /ip dhcp-server lease and /ip arp
# would both be empty -- and those two tables are the entire input to
# #243's suggestions feature. bridge1 plus a DHCP server gives the router
# something to have leases *of*.
#
# The self-DHCP-client line is the part worth explaining. A lease typed
# in by hand has no host-name at all -- confirmed here, `set host-name=`
# is refused outright with "bad parameter host-name", since RouterOS
# learns it from the client and will not let you assert it -- and a lease
# with no host-name produces no device suggestion. Pointing the router's
# own DHCP client at its own DHCP server makes it a real client of
# itself: it completes a real DHCP handshake and appears as a real
# dynamic lease carrying host-name=CHR. That is a genuinely learned
# lease, which is what this needs to exercise, rather than a record
# hand-shaped to look like one.
setup() {
  local url="${1:?usage: live-routeros.sh setup <mikroview url> <syslog host> <syslog port>}"
  local syslog_host="${2:?}" syslog_port="${3:?}"

  trust "$url" >/dev/null

  run \
    "/system logging action add name=mikroview target=remote remote=$syslog_host remote-port=$syslog_port remote-protocol=tls check-certificate=yes" \
    '/system logging add topics=firewall,info action=mikroview' \
    "/ip firewall filter add chain=input protocol=tcp dst-port=$INPUT_PORT action=accept log=yes log-prefix=\"A|live-in|\"" \
    '/ip firewall filter add chain=forward action=accept log=yes log-prefix="A|lan-wan|"' \
    '/ip firewall filter add chain=output protocol=icmp action=accept log=yes log-prefix="A|live-out|"' \
    "/ip firewall nat add chain=dstnat protocol=tcp dst-port=$FORWARD_PORT action=dst-nat to-addresses=203.0.113.9 to-ports=9999" \
    '/interface bridge add name=bridge1' \
    '/ip address add address=192.168.88.1/24 interface=bridge1' \
    '/ip pool add name=live-pool ranges=192.168.88.10-192.168.88.100' \
    '/ip dhcp-server add name=live-dhcp interface=bridge1 address-pool=live-pool disabled=no' \
    '/ip dhcp-server network add address=192.168.88.0/24 gateway=192.168.88.1' \
    '/ip dhcp-server lease add address=192.168.88.20 mac-address=AA:BB:CC:11:22:33 server=live-dhcp comment="live fixture camera"' \
    '/ip dns static add name=live-camera.lan address=192.168.88.20' \
    '/ip dhcp-client add interface=bridge1 disabled=no add-default-route=no use-peer-dns=no' \
    >/dev/null

  # The self-lease needs a moment to complete its handshake before
  # anything queries for it.
  sleep 8
}

# traffic makes the router log real firewall events, in every chain the
# fixture's topology can reach and in both protocol shapes the parser
# distinguishes.
#
# TCP to INPUT_PORT lands in `input`; TCP to FORWARD_PORT is dst-natted
# onward by the rule setup added and so lands in `forward` -- the chain
# that carries src-mac and the NAT annotation. The ping is `output` and
# ICMP, which has no ports at all; internal/routeros/parser.go names that
# as one of the shapes it has to tolerate, and this is where that stops
# being an assumption.
traffic() {
  local n="${1:-5}" i
  for i in $(seq 1 "$n"); do
    timeout 2 bash -c "exec 3<>/dev/tcp/127.0.0.1/$INPUT_PORT && printf 'x' >&3" 2>/dev/null || true
    timeout 2 bash -c "exec 3<>/dev/tcp/127.0.0.1/$FORWARD_PORT && printf 'x' >&3" 2>/dev/null || true
  done
  run "/ping 203.0.113.9 count=$n" >/dev/null
}

# push runs docs/routeros-setup.md step 4's three blocks -- filter rules,
# DHCP leases, ARP -- from the router itself, over TLS it verifies with
# the CA it imported in step 1.
#
# Each documented block is one console line here rather than the several
# the docs lay it out across. RouterOS treats a semicolon-separated line
# as one scope, so the :local names survive across the statements exactly
# as they do inside a saved script, which is what makes this the same
# code path an operator schedules rather than a rewrite of it.
push() {
  local url="${1:?usage: live-routeros.sh push <mikroview url> <ingest token>}"
  local token="${2:?}"
  local hdr="http-header-field=(\"Content-Type: application/json,Authorization: Bearer $token\")"
  local dest="url=\"$url/api/ingest/routeros\" http-method=post"

  run \
    ":local recs [:toarray \"\"]; :foreach i,v in=[/ip/firewall/filter print as-value] do={ :local rec {\"ordinal\"=\$i; \"comment\"=(\$v->\"comment\"); \"chain\"=(\$v->\"chain\"); \"action\"=(\$v->\"action\"); \"srcAddressList\"=(\$v->\"src-address-list\"); \"logPrefix\"=(\$v->\"log-prefix\"); \"dstPort\"=(\$v->\"dst-port\"); \"protocol\"=(\$v->\"protocol\"); \"log\"=(\$v->\"log\"); \"dstAddress\"=(\$v->\"dst-address\"); \"srcAddress\"=(\$v->\"src-address\"); \"connectionState\"=(\$v->\"connection-state\"); \"inInterface\"=(\$v->\"in-interface\"); \"outInterface\"=(\$v->\"out-interface\")}; :set recs (\$recs, {\$rec}) }; :local payload [:serialize to=json value={\"kind\"=\"filter-rule\"; \"page\"=1; \"pages\"=1; \"routerosVersion\"=[/system/resource get version]; \"records\"=\$recs}]; /tool fetch $dest http-data=\$payload $hdr check-certificate=yes output=none" \
    ":local leaseRecs [:toarray \"\"]; :foreach i,v in=[/ip/dhcp-server/lease print as-value] do={ :local rec {\"hostname\"=(\$v->\"host-name\"); \"mac\"=(\$v->\"mac-address\"); \"address\"=(\$v->\"address\")}; :set leaseRecs (\$leaseRecs, {\$rec}) }; :local leasePayload [:serialize to=json value={\"kind\"=\"dhcp-lease\"; \"page\"=1; \"pages\"=1; \"records\"=\$leaseRecs}]; /tool fetch $dest http-data=\$leasePayload $hdr check-certificate=yes output=none" \
    ":local arpRecs [:toarray \"\"]; :foreach i,v in=[/ip/arp print as-value] do={ :local rec {\"address\"=(\$v->\"address\"); \"mac\"=(\$v->\"mac-address\")}; :set arpRecs (\$arpRecs, {\$rec}) }; :local arpPayload [:serialize to=json value={\"kind\"=\"arp\"; \"page\"=1; \"pages\"=1; \"records\"=\$arpRecs}]; /tool fetch $dest http-data=\$arpPayload $hdr check-certificate=yes output=none"
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
  run-as) shift; run_as "$@" ;;
  trust) shift; trust "$@" ;;
  setup) shift; setup "$@" ;;
  traffic) shift; traffic "$@" ;;
  push) shift; push "$@" ;;
  host-addr) host_addr ;;
  down) down ;;
  *) echo "usage: $0 {up|run <command>...|run-as <user> <pass> <command>...|trust <url>|setup <url> <syslog host> <syslog port>|traffic [n]|push <url> <token>|host-addr|down}" >&2; exit 2 ;;
esac
