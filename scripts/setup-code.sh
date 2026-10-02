#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Reads the first-run setup code out of a mikroview server's own log.
# Sourced, never run.
#
# Since #1415 the first admin can only be created with a one-time code
# the server prints once, at Warn, when it starts with no accounts:
#
#   ... WARN  auth │ no account exists yet -- create the first admin with setup code xxxx-xxxx-xxxx-xxxx (valid until ...)
#
# Every harness that registers the first admin reads it from there, the
# way an operator does, rather than from anywhere the server would not
# put it. One pattern in one place, so the scripts cannot drift apart
# on what the line says.
#
# Not named live-*.sh on purpose: run-live-scripts.sh runs every
# scripts/live-*.sh as a check, and a sourced file run as one defines a
# function, exits 0 and reports a pass it never earned (#1337).
#
# Never echo the code into anything that is kept: it is the key to an
# empty instance for as long as that instance runs.

# mv_setup_code_in -- reads server log text on stdin and prints the
# newest setup code in it, or nothing.
mv_setup_code_in() {
  sed -n 's/.*create the first admin with setup code \([A-Za-z0-9][A-Za-z0-9-]*\).*/\1/p' | tail -n 1
}

# mv_wait_setup_code <tries> <command...> -- runs <command> (one that
# prints the server's log, e.g. `cat "$dir/server.log"` or `docker logs
# <name>`) every quarter second until its output carries a setup code,
# then prints the code. Returns 1, printing nothing, if none appeared
# after <tries> attempts.
mv_wait_setup_code() {
  local tries="$1" code="" _
  shift
  for _ in $(seq 1 "$tries"); do
    code="$("$@" 2>&1 | mv_setup_code_in)" || true
    if [ -n "$code" ]; then
      printf '%s' "$code"
      return 0
    fi
    sleep 0.25
  done
  return 1
}
