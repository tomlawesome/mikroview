#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# #1550: .private-details-allow.toml must be in the shape the private-details
# hook accepts (only [[allowlists]] tables with description, targetRules and
# paths; SPEC.md section 5 in agent-infra). A file the hook refuses blocks
# every commit once the owner installs it, so this runs the hook's own shape
# check, allow-shape.awk, over the file rather than a copy of its rules here.
#
# Where the check comes from, first match wins:
#   PRIVATE_DETAILS_SHAPE   a path to allow-shape.awk
#   CI (CI_JOB_TOKEN set)   ai/agent-config at AGENT_CONFIG_SHA, fetched with
#                           the job token
#   otherwise               this host's installed hook,
#                           /etc/agent-hooks/current/private-details/allow-shape.awk
# The rule ids come from PRIVATE_DETAILS_RULES (default
# /etc/private-details/rules.toml, the file mounted into every job). Nothing
# here prints that file's contents. A missing check or rules file fails the
# test: it never skips.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ALLOW="$HERE/../.private-details-allow.toml"
# The agent-config commit whose allow-shape.awk is the installed hook's copy.
# Move it when the installed hook's shape check changes.
AGENT_CONFIG_SHA="${AGENT_CONFIG_SHA:-ed96cc95aa887d6abaa86e52c7bdb61fce609544}"
RULES="${PRIVATE_DETAILS_RULES:-/etc/private-details/rules.toml}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail=0
ok() { echo "ok - $1"; }
bad() { echo "FAIL - $1"; fail=1; }

shape=
if [ -n "${PRIVATE_DETAILS_SHAPE:-}" ]; then
  shape="$PRIVATE_DETAILS_SHAPE"
elif [ -n "${CI_JOB_TOKEN:-}" ]; then
  repo="$tmp/agent-config"
  git init --quiet "$repo"
  # The token reaches git through a credential helper that reads the
  # environment, so it is never on a command line.
  # shellcheck disable=SC2016
  if git -C "$repo" -c credential.helper='!f() { echo username=gitlab-ci-token; echo "password=$CI_JOB_TOKEN"; }; f' \
      fetch --quiet --depth 1 "https://${CI_SERVER_HOST:-gitlab.tomlawson.io}/ai/agent-config.git" "$AGENT_CONFIG_SHA" &&
      [ "$(git -C "$repo" rev-parse FETCH_HEAD)" = "$AGENT_CONFIG_SHA" ] &&
      git -C "$repo" show FETCH_HEAD:hooks/private-details/allow-shape.awk > "$tmp/allow-shape.awk"; then
    shape="$tmp/allow-shape.awk"
  else
    bad "could not fetch allow-shape.awk from ai/agent-config at $AGENT_CONFIG_SHA"
  fi
else
  shape=/etc/agent-hooks/current/private-details/allow-shape.awk
fi

if [ -z "$shape" ] || [ ! -r "$shape" ]; then
  bad "the hook's shape check is not readable (${shape:-none}); set PRIVATE_DETAILS_SHAPE"
  exit 1
fi
if [ ! -r "$RULES" ]; then
  bad "rules file not readable: $RULES; set PRIVATE_DETAILS_RULES"
  exit 1
fi

# Rule ids, read the way the hook's scan.sh reads them.
awk '{ sub(/\r$/, "") }
    /^[ \t]*id[ \t]*=[ \t]*["\047]/ {
        v = $0; sub(/^[^=]*=[ \t]*["\047]/, "", v); sub(/["\047].*$/, "", v); print v
    }' "$RULES" > "$tmp/ids"
if [ -s "$tmp/ids" ]; then ok "the rules file names rule ids"; else bad "no rule ids in $RULES"; fi

# shape_check <file>: prints the check's one-line reason; returns its exit code.
shape_check() { LC_ALL=C awk -f "$shape" "$tmp/ids" "$1"; }

if msg="$(shape_check "$ALLOW")"; then
  ok ".private-details-allow.toml passes the hook's shape check"
else
  bad ".private-details-allow.toml is refused by the hook: $msg"
fi

# Controls: the same check must refuse a value-matching key (#1550's cause) and
# a rule id the rules file does not have, so a check that passes everything
# cannot make this test pass.
printf '[[allowlists]]\ndescription = "control"\ntargetRules = ["%s"]\nregexTarget = "match"\npaths = ['"'''^x$'''"']\n' \
  "$(head -n 1 "$tmp/ids")" > "$tmp/regex-target.toml"
if msg="$(shape_check "$tmp/regex-target.toml")"; then
  bad "the shape check accepted a regexTarget key"
else
  case $msg in *regexTarget*) ok "the shape check refuses a regexTarget key" ;;
    *) bad "the shape check refused a regexTarget key for another reason: $msg" ;; esac
fi
printf '[[allowlists]]\ntargetRules = ["no-such-rule-1550"]\npaths = ['"'''^x$'''"']\n' > "$tmp/unknown-rule.toml"
if shape_check "$tmp/unknown-rule.toml" > /dev/null; then
  bad "the shape check accepted an unknown rule id"
else
  ok "the shape check refuses an unknown rule id"
fi

exit "$fail"
