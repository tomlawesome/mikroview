#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Brings up a seeded demo instance, following the demo procedure
# (docs/demos.md) step by step instead of from memory:
#
#   1. cmd/demo-history builds the history envelope (seven nights of
#      streaks plus matching back-dated traffic, ending yesterday).
#   2. scripts/live-env.sh up, with MV_DEMO_DEVICES=1 (so pushed tables
#      meet real device ids, #709) and MV_DEMO_BUILD=1 (the
#      self-destroying service worker, #713).
#   3. Stop the server, restore the envelope into it, start it again.
#      -restore writes the stores to disk, so the server must be down.
#   4. seed-demo.py push, entities, accounts, then feed in the
#      background. Never `all`: it also runs `watchlist`, which would add
#      six blank entries beside the six the envelope already carries.
#
# Usage: scripts/demo-up.sh [--dry-run]
#   --dry-run   print each command prefixed "+ " and run nothing.
#   MV_BIND=<addr> is required for a real run: the address the owner will
#   open the demo on (loopback is only reachable from this host). The same
#   address is the syslog host the seeder streams to.
#
# Healthy result: the steps print as they run, `feed` keeps running in
# the background (log in $MV_DIR/feed.log, pid in $MV_DIR/feed.pid), and
# the LAST line is "Build stamp: <version>" -- the same stamp live-env.sh
# gave the binary and the UI bundle. Quote it when handing the demo over.
# Run `scripts/seed-demo.py mutate` by hand once the feed has produced
# real flags.
# The step texts below are shown, not expanded: $MV_DIR and friends do not
# exist yet in a dry run.
# shellcheck disable=SC2016
set -euo pipefail

usage() {
  echo "usage: $0 [--dry-run]" >&2
}

DRY=0
case "${1:-}" in
  "") ;;
  --dry-run) DRY=1 ;;
  *) usage; exit 2 ;;
esac
[ "$#" -le 1 ] || { usage; exit 2; }

cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"

BACKUP=/tmp/demo-history.backup
if [ -z "${MV_BIND:-}" ]; then
  if [ "$DRY" = 1 ]; then
    MV_BIND='<addr>'
  else
    echo "demo-up: set MV_BIND to the address the owner will open the demo on" >&2
    exit 2
  fi
fi
export MV_BIND

step() { printf '+ %s\n' "$1"; }

stamp="<the stamp live-env.sh builds with; a real run reads it from its 'built' line>"

step "go run ./cmd/demo-history -out $BACKUP -force"
[ "$DRY" = 1 ] || go run ./cmd/demo-history -out "$BACKUP" -force

step "MV_DEMO_DEVICES=1 MV_DEMO_BUILD=1 MV_BIND=$MV_BIND scripts/live-env.sh up"
step 'eval the export lines it prints (MV_URL MV_USER MV_PASS MV_DIR MV_SYSLOG_TLS_PORT ...)'
if [ "$DRY" != 1 ]; then
  errf="$(mktemp)"
  if ! out="$(MV_DEMO_DEVICES=1 MV_DEMO_BUILD=1 MV_BIND="$MV_BIND" scripts/live-env.sh up 2>"$errf")"; then
    cat "$errf" >&2
    rm -f "$errf"
    exit 1
  fi
  cat "$errf" >&2
  eval "$out"
  # build() in live-env.sh logs "live-env: built <stamp>" on stderr; that
  # is the stamp it handed to -ldflags main.version and to the UI bundle.
  stamp="$(sed -n 's/^live-env: built //p' "$errf" | tail -n 1)"
  rm -f "$errf"
  [ -n "$stamp" ] || stamp="unknown (live-env.sh did not build; MV_BINARY prebuilt?)"
fi

step 'kill "$(cat "$MV_DIR/pid")"'
step 'wait for that process to exit'
if [ "$DRY" != 1 ]; then
  old_pid="$(cat "$MV_DIR/pid")"
  kill "$old_pid"
  for _ in $(seq 1 40); do
    kill -0 "$old_pid" 2>/dev/null || break
    sleep 0.25
  done
  if kill -0 "$old_pid" 2>/dev/null; then
    echo "demo-up: server $old_pid did not stop; refusing to restore under a running server" >&2
    exit 1
  fi
fi

step 'MIKROVIEW_CONFIG="$MV_DIR/cfg.yaml" "$MV_DIR/mikroview" -restore '"$BACKUP"' --force'
[ "$DRY" = 1 ] || MIKROVIEW_CONFIG="$MV_DIR/cfg.yaml" "$MV_DIR/mikroview" -restore "$BACKUP" --force

step 'MIKROVIEW_CONFIG="$MV_DIR/cfg.yaml" "$MV_DIR/mikroview" > "$MV_DIR/server.log" 2>&1 &'
step 'echo $! > "$MV_DIR/pid"'
step 'wait for $MV_URL/api/healthz'
if [ "$DRY" != 1 ]; then
  MIKROVIEW_CONFIG="$MV_DIR/cfg.yaml" "$MV_DIR/mikroview" > "$MV_DIR/server.log" 2>&1 &
  echo $! > "$MV_DIR/pid"
  up=0
  for _ in $(seq 1 40); do
    if curl -fsSk "$MV_URL/api/healthz" >/dev/null 2>&1; then up=1; break; fi
    sleep 0.25
  done
  if [ "$up" != 1 ]; then
    echo "demo-up: restarted server did not answer $MV_URL/api/healthz; see $MV_DIR/server.log" >&2
    exit 1
  fi
fi

step "export MV_SYSLOG_HOST=$MV_BIND  # MV_SYSLOG_TLS_PORT comes from the up exports"
[ "$DRY" = 1 ] || export MV_SYSLOG_HOST="$MV_BIND"

step 'scripts/seed-demo.py push'
[ "$DRY" = 1 ] || scripts/seed-demo.py push
step 'scripts/seed-demo.py entities'
[ "$DRY" = 1 ] || scripts/seed-demo.py entities
step 'scripts/seed-demo.py accounts'
[ "$DRY" = 1 ] || scripts/seed-demo.py accounts
step 'scripts/seed-demo.py feed &   # long-running; leave it running'
if [ "$DRY" != 1 ]; then
  nohup scripts/seed-demo.py feed > "$MV_DIR/feed.log" 2>&1 &
  echo $! > "$MV_DIR/feed.pid"
fi

echo "Build stamp: $stamp"
