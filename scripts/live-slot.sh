#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# The one port allocator every live check uses. Sourced, never run.
#
# This exists because there used to be two (#660). live-env.sh derived
# its ports from a per-checkout slot, while the standalone scripts
# hardcoded values -- 19821, 19822, 19811, 19812, 19831 -- that sat
# *inside* the band live-env.sh hands out. So slot 21's HTTP port was
# also live-logspam-check.sh's and live-migrate-data.sh's, and slot 22's
# was live-logspam-check.sh's syslog-TLS port. Not a risk of collision: a
# guarantee of one, for whichever checkout happened to hash there.
#
# What it looked like was "FAIL: server never came up", in a script that
# names no port, in a phase that then leaves state for the next script to
# trip over. One collision presented as five failures across two scripts.
#
# Keeping the derivation here rather than in each caller is the point.
# Two allocators drifted apart precisely because nothing forced them to
# agree.

# The slot is a hash of the checkout path, so each worktree gets its own
# ports, stable across repeated runs in the same tree. 64 slots is not a
# guarantee -- two checkouts can hash to the same one -- which is what
# mv_require_free_port below is for.
#
# BASH_SOURCE, not $0: this file is sourced, so $0 is the caller and
# would give the wrong directory for a caller outside scripts/.
MV_SLOT="$(printf '%s' "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)" | cksum | awk '{print $1 % 64}')"

# Six bands, none overlapping, one port per slot in each:
#
#   16800-16927  the shared instance's syslog and syslog-TLS, interleaved
#   17000-17063  a standalone script's syslog-TLS
#   17100-17163  the RouterOS fixture's event sink
#   17200-17263  the RouterOS fixture's SFTP probe
#   19800-19863  the shared instance's HTTP
#   19900-19963  a standalone script's HTTP
#
# The four standalone scripts share one pair between them rather than
# taking a band each, because run-live-scripts.sh runs them strictly in
# sequence: only one is ever up. Handing out bands per script would put
# the ranges back to competing for room, which is how this began.
#
# Nothing here may be widened into a neighbour. Anything added takes a
# fresh band with a gap, and gets a line above -- an undocumented band is
# how 19822 came to be two different things.
MV_SLOT_HTTP_PORT=$((19800 + MV_SLOT))
MV_SLOT_SYSLOG_PORT=$((16800 + MV_SLOT * 2))
MV_SLOT_SYSLOG_TLS_PORT=$((16801 + MV_SLOT * 2))
MV_STANDALONE_HTTP_PORT=$((19900 + MV_SLOT))
MV_STANDALONE_SYSLOG_TLS_PORT=$((17000 + MV_SLOT))

# The RouterOS fixture (live-routeros-step0.sh) is opt-in rather than
# part of live-check, but it ran on the same two-allocator mistake: its
# SFTP probe defaulted to 19822, which is slot 22's HTTP port, and its
# sink to 19899, which rootless Docker has been seen publishing on this
# host. Two more bands rather than sharing the standalone pair, because
# this fixture runs *alongside* a live instance, not instead of one.
MV_ROUTEROS_SINK_PORT=$((17100 + MV_SLOT))
MV_ROUTEROS_SFTP_PORT=$((17200 + MV_SLOT))

# mv_port_in_use PORT -- true if anything is listening on it locally.
mv_port_in_use() {
  if command -v ss >/dev/null 2>&1; then
    ss -ltnH "sport = :$1" 2>/dev/null | grep -q .
  else
    (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null && exec 3>&- && return 0
    return 1
  fi
}

# mv_port_holder PORT -- best-effort description of what holds it, for an
# error message. Empty when nothing does, or when ss cannot say (it needs
# no privilege for this account's own processes, which is the case that
# matters -- every live-check instance belongs to the same account).
mv_port_holder() {
  command -v ss >/dev/null 2>&1 || return 0
  ss -ltnpH "sport = :$1" 2>/dev/null | sed -n 's/.*users:((\(.*\))).*/\1/p' | head -1
}

# mv_require_free_port PORT WHAT -- exit 1 with a message naming the port
# and its holder, rather than letting the caller start a server that
# cannot bind and report "server never came up" three seconds later.
#
# The old message said nothing about which port or what held it, so
# attributing one of these cost a full investigation. Anything added here
# is read by whoever hits this at their least patient.
mv_require_free_port() {
  local port="$1" what="$2" holder
  mv_port_in_use "$port" || return 0
  holder="$(mv_port_holder "$port")"
  echo "live-check: cannot start $what -- 127.0.0.1:$port is already in use." >&2
  if [ -n "$holder" ]; then
    echo "live-check: held by $holder" >&2
  fi
  echo "live-check: this checkout is slot $MV_SLOT. Another checkout hashing to the same" >&2
  echo "live-check: slot, or a live-check instance left behind by an interrupted run," >&2
  echo "live-check: will hold this port. 'scripts/live-env.sh down' in the checkout that" >&2
  echo "live-check: owns it stops the shared instance; a leftover standalone server has" >&2
  echo "live-check: no owner and can be killed directly." >&2
  exit 1
}
