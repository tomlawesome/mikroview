#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# The store block every live-* script writes into the config it
# generates, in one place.
#
# There is no "most of the stores" option. checkStoresUsable
# (storage_preflight.go, #536) walks backedUpStores and refuses to start
# on the first path it cannot write, so a config naming only some of
# them leaves the rest at their /var/lib/mikroview defaults and the
# server exits before doing anything.
#
# This file exists because that is precisely what happened. The block
# was copy-pasted into four scripts; three of them fell behind, and
# live-cert-reload.sh, live-logspam-check.sh and live-tls-log-lines.sh
# were all unable to start a server at all -- silently, for long enough
# that nobody noticed three real checks had stopped running (#595).
#
# TestLiveScriptsCoverEveryStore (live_scripts_test.go) is the guard:
# it fails the build if a store named by backedUpStores is missing from
# the block below, so adding one to config.Config cannot quietly break
# the harness again.
#
# Usage: `mv_store_block "$DIR" "$SECURE_COOKIE"` prints the YAML, with
# every path under the directory given. secureCookie is a parameter
# rather than part of the block because it is not a store and the
# callers genuinely differ on it -- live-env.sh turns it off when it
# serves plain HTTP.

mv_store_block() {
  local dir="$1" secure_cookie="${2:-true}"
  cat <<YAML
auth:
  storePath: $dir/users.json
  recoveryKeysPath: $dir/recovery.json
  recoveryPepperPath: $dir/pepper
  tokensStorePath: $dir/tokens.json
  secureCookie: $secure_cookie
flags:
  storePath: $dir/flags.json
  ruleUsageStorePath: $dir/rule-usage.json
  detectorSettingsStorePath: $dir/detector-settings.json
entities: {storePath: $dir/entities.json}
coverage: {storePath: $dir/coverage.json}
audit: {storePath: $dir/audit.json}
setup: {storePath: $dir/setup.json}
watchlist:
  storePath: $dir/watchlist.json
  matchLogPath: $dir/matchlog.jsonl
  suggestionsStorePath: $dir/suggestions.json
deviceMac: {storePath: $dir/mac-registry.json}
engine:
  storePath: $dir/engine-state.json
  definitionsStorePath: $dir/definitions.json
YAML
}
