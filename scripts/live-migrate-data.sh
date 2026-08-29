#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Runs `-migrate-data` for real: a live instance writes real state, the
# data is migrated to a new directory, and a fresh server is started
# against the destination to prove the state survived (#537).
#
# A browser scenario is the wrong shape for this. The claim being tested
# is that a deployment can be moved without losing anything, and the
# only way to test that is to move one and start it again -- the same
# reason live-cert-reload.sh drives SIGHUP against a running server
# rather than unit-testing the reloader.
#
# The unit tests cover the copy, the refusals and the verification in
# isolation. What they cannot show is the part that actually costs an
# operator: that mikroview boots from the destination, with its accounts,
# its definitions and its flags intact, and that storage_preflight is
# satisfied by what the migration produced.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO"
. "$REPO/scripts/live-stores.sh"

DIR="$(mktemp -d)"
PORT=19821
SRC="$DIR/src"
DST="$DIR/dst"
PID=""
failed=0

cleanup() {
  [ -n "$PID" ] && kill "$PID" 2>/dev/null || true
  rm -rf "$DIR"
}
trap cleanup EXIT

ok()   { echo "  ok    $1"; }
fail() { echo "  FAIL  $1"; failed=1; }
check() { if [ "$1" = "0" ]; then ok "$2"; else fail "$2"; fi; }

cfg() {
  cat <<YAML
listen: {http: "127.0.0.1:$PORT", httpRedirect: "", syslogTls: ""}
tls: {enabled: false, storePath: $1/tls}
$(mv_store_block "$1" false)
YAML
}

start() {
  MIKROVIEW_CONFIG="$1/cfg.yaml" "$DIR/mikroview" > "$1/server.log" 2>&1 &
  PID=$!
  for _ in $(seq 1 40); do
    if curl -fsS "http://127.0.0.1:$PORT/api/healthz" >/dev/null 2>&1; then return 0; fi
    sleep 0.25
  done
  return 1
}

stop() {
  [ -n "$PID" ] && kill "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
  PID=""
}

echo "== migrate-data, against a real instance"

( cd frontend && npm run build >/dev/null 2>&1 )
rm -rf web/dist && mkdir -p web/dist && cp -r frontend/dist/. web/dist/
go build -buildvcs=false -o "$DIR/mikroview" .

mkdir -p "$SRC"
cfg "$SRC" > "$SRC/cfg.yaml"

start "$SRC" || { fail "the source instance never became healthy -- see $SRC/server.log"; exit 1; }
ok "a real instance started against the source directory"

# Real state, written through the real API rather than by planting files:
# an account is the thing an operator cannot afford to lose, and it also
# exercises the recovery pepper and the accounts store together.
create="$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:$PORT/api/auth/register" \
  -H 'Content-Type: application/json' -H 'X-Requested-With: mikroview' \
  -d '{"username":"migrate-test","password":"correct horse battery staple 42"}' 2>/dev/null || true)"
check "$([ "$create" = "200" ] || [ "$create" = "201" ] && echo 0 || echo 1)" \
  "an admin account was created through the real API (got $create)"

stop
ok "the source instance stopped cleanly"

# The migration itself.
if MIKROVIEW_CONFIG="$SRC/cfg.yaml" "$DIR/mikroview" -migrate-data "$DST" > "$DIR/migrate.log" 2>&1; then
  ok "-migrate-data reported success"
else
  fail "-migrate-data failed: $(tail -3 "$DIR/migrate.log")"
fi

check "$([ -f "$DST/users.json" ] && echo 0 || echo 1)" "the accounts store arrived at the destination"
check "$([ -f "$SRC/users.json" ] && echo 0 || echo 1)" "the source was left whole, not moved"

# Everything, not a named sample. Listing what the source actually holds
# and requiring the destination to hold the same means a store this
# script has never heard of still has to travel -- which is the whole
# promise, and the one a hand-picked list of filenames cannot check.
# cfg.yaml and server.log are this script's own, not the deployment's.
( cd "$SRC" && find . -type f ! -name cfg.yaml ! -name server.log | sort ) > "$DIR/src.list"
( cd "$DST" && find . -type f ! -name cfg.yaml ! -name server.log | sort ) > "$DIR/dst.list"
if diff -q "$DIR/src.list" "$DIR/dst.list" >/dev/null; then
  ok "every file in the source arrived at the destination ($(wc -l < "$DIR/src.list") files)"
else
  fail "the destination does not match the source:$(diff "$DIR/src.list" "$DIR/dst.list" | head -6)"
fi

# The recovery pepper is the interesting one: -backup deliberately
# excludes it, because a backup travels to another host. A migration does
# not, and losing it silently invalidates every recovery key.
if [ -f "$SRC/pepper" ]; then
  check "$([ -f "$DST/pepper" ] && echo 0 || echo 1)" "the recovery pepper travelled -- backup excludes it, a migration must not"
else
  ok "no recovery pepper was written in this run, so there was none to carry"
fi

# The real question: does a server come up on the destination, and is the
# account still there? Anything less is checking that files were copied.
cfg "$DST" > "$DST/cfg.yaml"
if start "$DST"; then
  ok "a fresh server started against the migrated directory"
else
  fail "the migrated instance never became healthy -- see $DST/server.log"
fi

code="$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:$PORT/api/auth/login" \
  -H 'Content-Type: application/json' -H 'X-Requested-With: mikroview' \
  -d '{"username":"migrate-test","password":"correct horse battery staple 42"}' 2>/dev/null || true)"
check "$([ "$code" = "200" ] && echo 0 || echo 1)" "the account created before the migration can still sign in (got $code)"

# Registration is closed on the destination: an account already exists,
# so the migrated instance is not offering to create a second admin over
# the top of the first. 409 is auth.ErrRegistrationClosed.
second="$(curl -s -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:$PORT/api/auth/register" \
  -H 'Content-Type: application/json' -H 'X-Requested-With: mikroview' \
  -d '{"username":"second-admin","password":"correct horse battery staple 42"}' 2>/dev/null || true)"
check "$([ "$second" = "409" ] && echo 0 || echo 1)" \
  "the migrated instance refuses a second first-account, so it knows the migrated one exists (got $second)"

stop

if [ "$failed" -ne 0 ]; then
  echo
  echo "RESULT: FAIL"
  exit 1
fi
echo
echo "PASS: a deployment survives -migrate-data and starts from its new home."
