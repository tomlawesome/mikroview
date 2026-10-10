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
  signInsStorePath: $dir/signins.json
  secureCookie: $secure_cookie
  adminPasskey: optional
flags:
  storePath: $dir/flags.json
  ruleUsageStorePath: $dir/rule-usage.json
store: {settingsStorePath: $dir/settings.json}
entities: {storePath: $dir/entities.json}
coverage: {storePath: $dir/coverage.json}
hosts: {storePath: $dir/hosts.json}
seen: {storePath: $dir/seen-values.json}
baseline: {storePath: $dir/baseline.json}
audit: {storePath: $dir/audit.json}
setup: {storePath: $dir/setup.json}
droplist: {storePath: $dir/droplist.json}
prefs: {storePath: $dir/preferences.json}
watchlist:
  matchLogPath: $dir/matchlog.jsonl
  suggestionsStorePath: $dir/suggestions.json
deviceMac: {storePath: $dir/mac-registry.json}
deviceRegistry: {storePath: $dir/device-registry.json}
engine:
  storePath: $dir/engine-state.json
  definitionsStorePath: $dir/definitions.json
  decommissionStorePath: $dir/decommission.json
YAML
}

# Since #1202 MikroView refuses to start without a key (CFG-0083): every
# account, token, recovery-key and sign-in store is sealed under it.
# `mv_history_key_block DIR` makes a fresh random key at DIR/history.key
# the first time it is called for DIR (never committed, never reused
# across runs -- DIR is a throwaway) and prints the config line that
# names it, for the scripts that write their own config. A second call
# for the same DIR keeps the key, so a source and the copy migrated from
# it open under the same one. live-env.sh makes its own beside the data
# directory, because it also turns retention on.
mv_history_key_block() {
  local key="$1/history.key"
  if [ ! -s "$key" ]; then
    mkdir -p "$1"
    head -c 32 /dev/urandom | base64 > "$key"
    chmod 600 "$key"
  fi
  echo "history: {keyFile: \"$key\"}"
}

# adminPasskey is "optional" in mv_store_block's auth section because the rule's
# default turns to "required" on an https publicUrl a passkey can be
# made for, and the harness's TLS instances are exactly that
# (https://localhost:PORT): every admin request would answer 403
# must-enrol-passkey, and the harness has no passkey to register.
#
# The common-password list fetch (#1202) is pointed at a closed local
# port so a live run makes no outbound request and leaves no downloaded
# list in the data directory. It is honoured only with MV_TEST_HOOKS=1
# (gauntlet_wiring.go), which live-env.sh and live-migrate-data.sh set.
MV_PASSWORDLIST_OFFLINE_URL="http://127.0.0.1:9/password-list-offline"
