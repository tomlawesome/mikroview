#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# The clear-and-copy step every standalone live-* script uses to get an
# embedded frontend into web/dist before building. Sourced, never run.
#
# This exists because the same line -- rm -rf web/dist, recreate it, copy
# frontend/dist/. in -- was copy-pasted into live-cert-reload.sh,
# live-logspam-check.sh, live-migrate-data.sh and live-tls-log-lines.sh,
# and none of them restored web/dist/.gitkeep afterwards. frontend/dist
# has no .gitkeep of its own, so every one of those scripts left the
# tracked, otherwise-empty web/dist/.gitkeep deleted and the tree dirty
# (#1292).
#
# mv_rebuild_web_dist ends with `touch .gitkeep` for the same reason the
# Makefile's frontend target does (Makefile:9-17): it is the file that
# keeps go:embed compiling in web/embed.go when nothing has been built.
mv_rebuild_web_dist() {
  local web_dist="${1:-web/dist}" frontend_dist="${2:-frontend/dist}"
  rm -rf "$web_dist"
  mkdir -p "$web_dist"
  cp -r "$frontend_dist/." "$web_dist/"
  touch "$web_dist/.gitkeep"
}
