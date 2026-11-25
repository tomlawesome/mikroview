#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/govulncheck-gate.py (#1558): the govulncheck JSON stream
# is gated against a reviewed not-affected list. Reachable ids must be
# listed; listed ids must still be reachable (no stale entries); every entry
# needs id, reason and issue.
set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/govulncheck-gate.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail=0
ok() { echo "ok - $1"; }
bad() { echo "FAIL - $1"; fail=1; }

# Pretty-printed objects, concatenated, with noise objects around them.
header='{
  "config": {"protocol_version": "v1.0.0", "scanner_name": "govulncheck"}
}
{
  "progress": {"message": "Scanning your code"}
}
{
  "osv": {"id": "GO-2026-6278", "summary": "x"}
}
{
  "SBOM": {"modules": []}
}
'
reach() {  # reach <osv> -- a reachable finding
  printf '{\n  "finding": {\n    "osv": "%s",\n    "fixed_version": "v1.5.4",\n    "trace": [\n      {"module": "github.com/gorilla/websocket", "version": "v1.5.3", "package": "github.com/gorilla/websocket", "function": "WriteMessage", "receiver": "*Conn"},\n      {"module": "github.com/tomlawesome/mikroview", "package": "github.com/tomlawesome/mikroview/internal/web", "function": "handleWS"}\n    ]\n  }\n}\n' "$1"
}
modonly() {  # modonly <osv> -- module-level finding, not reachable
  printf '{\n  "finding": {\n    "osv": "%s",\n    "fixed_version": "v1.5.4",\n    "trace": [\n      {"module": "github.com/gorilla/websocket", "version": "v1.5.3"}\n    ]\n  }\n}\n' "$1"
}
pkgonly() {
  printf '{\n  "finding": {\n    "osv": "%s",\n    "trace": [\n      {"module": "example.com/m", "version": "v1.0.0", "package": "example.com/m/p"}\n    ]\n  }\n}\n' "$1"
}
entry() { printf '[{"id":"%s","reason":"%s","issue":"#1"}]' "$1" "$2"; }

run() {  # run <findings-file> <list-file>; sets rc and out
  out="$(python3 "$SCRIPT" "$1" "$2" 2>&1)"; rc=$?
}
expect_rc() {  # expect_rc <want> <name>
  if [ "$rc" = "$1" ]; then ok "$2 exits $1"; else bad "$2: expected exit $1 got $rc: $out"; fi
}
expect_out() {  # expect_out <fixed-string> <name>
  if grep -qF -- "$1" <<< "$out"; then ok "$2"; else bad "$2: output lacks [$1]: $out"; fi
}
expect_no_out() {
  if grep -qF -- "$1" <<< "$out"; then bad "$2: output unexpectedly has [$1]: $out"; else ok "$2"; fi
}

# 1. Reachable and listed: pass, reason printed.
{ printf '%s' "$header"; reach GO-2026-6278; } > "$TMP/f1.json"
entry GO-2026-6278 "frame never reaches the vulnerable path" > "$TMP/l1.json"
run "$TMP/f1.json" "$TMP/l1.json"
expect_rc 0 "reachable listed"
expect_out "GO-2026-6278" "reachable listed prints id"
expect_out "frame never reaches the vulnerable path" "reachable listed prints reason"

# 2. Reachable and unlisted: fail, names id.
echo '[]' > "$TMP/empty.json"
run "$TMP/f1.json" "$TMP/empty.json"
expect_rc 1 "reachable unlisted"
expect_out "GO-2026-6278" "reachable unlisted names id"

# 3. Module-level-only (and package-only) findings unlisted: pass.
{ printf '%s' "$header"; modonly GO-2026-1111; pkgonly GO-2026-2222; } > "$TMP/f3.json"
run "$TMP/f3.json" "$TMP/empty.json"
expect_rc 0 "non-reachable findings unlisted"
expect_no_out "GO-2026-1111" "module-only id not reported"

# 4. Stale listed id with no findings: fail, names id.
{ printf '%s' "$header"; } > "$TMP/f4.json"
entry GO-2026-9999 "was reviewed once" > "$TMP/l4.json"
run "$TMP/f4.json" "$TMP/l4.json"
expect_rc 1 "stale entry"
expect_out "GO-2026-9999" "stale entry names id"

# 4b. Listed id only found module-level: still stale.
run "$TMP/f3.json" <(entry GO-2026-1111 "r")
expect_rc 1 "listed id only module-level is stale"
expect_out "GO-2026-1111" "listed module-level id named as stale"

# 5. Entry missing reason (also id, issue, non-list): fail.
printf '[{"id":"GO-2026-6278","issue":"#1"}]' > "$TMP/l5.json"
run "$TMP/f1.json" "$TMP/l5.json"
expect_rc 1 "entry missing reason"
printf '[{"id":"GO-2026-6278","reason":"r"}]' > "$TMP/l5b.json"
run "$TMP/f1.json" "$TMP/l5b.json"
expect_rc 1 "entry missing issue"
printf '[{"reason":"r","issue":"#1"}]' > "$TMP/l5c.json"
run "$TMP/f1.json" "$TMP/l5c.json"
expect_rc 1 "entry missing id"
printf '[{"id":"GO-2026-6278","reason":"","issue":"#1"}]' > "$TMP/l5d.json"
run "$TMP/f1.json" "$TMP/l5d.json"
expect_rc 1 "entry with empty reason"
printf '{"id":"GO-2026-6278","reason":"r","issue":"#1"}' > "$TMP/l5e.json"
run "$TMP/f1.json" "$TMP/l5e.json"
expect_rc 1 "list file not a list"

# 6. One reachable listed + another reachable unlisted: fail, names unlisted.
{ printf '%s' "$header"; reach GO-2026-6278; reach GO-2026-7777; } > "$TMP/f6.json"
run "$TMP/f6.json" "$TMP/l1.json"
expect_rc 1 "mixed listed and unlisted"
expect_out "GO-2026-7777" "mixed names the unlisted id"

# 7. Several findings with the same listed id: pass.
{ printf '%s' "$header"; reach GO-2026-6278; reach GO-2026-6278; reach GO-2026-6278; } > "$TMP/f7.json"
run "$TMP/f7.json" "$TMP/l1.json"
expect_rc 0 "repeated findings for one listed id"

# 8. Empty list and no findings: pass.
run "$TMP/f4.json" "$TMP/empty.json"
expect_rc 0 "empty list, no findings"

exit "$fail"
