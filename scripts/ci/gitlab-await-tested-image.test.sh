#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Exercises scripts/ci/gitlab-await-tested-image.sh (#1472 step 3, item 2),
# written from the step 3 spec alone. Nothing here touches the network: a stub
# `curl` (and `wget`, `ssh`, `git`) on PATH answers from fixture files and logs
# every call, and a call to anything the stub does not know fails the case.
#
# INTERFACE ASSUMED (the script does not exist yet; this is orbit's
# scripts/ci/gitlab-await-tested-image.sh, read only for its shape, with the
# ORBIT_ prefix of its variables replaced by this repo's MV_ prefix, the one
# gitlab-record-tested-image.sh already uses for MV_EVIDENCE_DIR):
#   environment: GITLAB_API_URL, GITLAB_PROJECT_ID (53), GITLAB_READ_TOKEN,
#     GITLAB_REGISTRY, MV_COMMIT (40 hex), MV_REF (preview or v*),
#     MV_WAIT_MINUTES, MV_POLL_SECONDS, MV_EVIDENCE_DIR, GITHUB_OUTPUT.
#   API calls, all through `curl` with the token in a --header @file:
#     GET <api>/projects/53/pipelines?sha=<commit>&ref=<ref>&source=push...
#         -> JSON array, newest first: [{id, status, web_url}]
#     GET <api>/projects/53/pipelines/<id>/jobs?per_page=100
#         -> JSON array [{id, name, status}]
#     GET <api>/projects/53/jobs/<job>/artifacts/.mv-supply-chain/gitlab-tested-image.json
#         (from publish:image; saved with --output)
#     GET <api>/projects/53/jobs/<job>/artifacts/image.spdx.json
#         (from security:trivy-image; saved with --output)
#   required successful jobs: publish:image, sign:release-digest,
#     publish:channel, security:trivy-image.
#   evidence JSON (as gitlab-record-tested-image.sh writes it): imageReference,
#     imageDigest, commit, ref, pipelineId, pipelineUrl, builtAt.
#   success: exit 0, the digest on stdout (and digest=... in $GITHUB_OUTPUT).
#   refusal: non-zero exit, a reason on stderr.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT="$HERE/gitlab-await-tested-image.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
fail=0

SHA="$(printf 'a%.0s' $(seq 40))"
DIGEST="sha256:$(printf 'c%.0s' $(seq 64))"
TOKEN="sentinel-token-value-must-never-appear"
REGISTRY="registry.example.test"

ok() { echo "ok - $1"; }
bad() { echo "FAIL - $1"; fail=1; }
check() { if [ "$1" = "$2" ]; then ok "$3"; else bad "$3 (got '$1', want '$2')"; fi; }

# Stub tools. The stub serves $FIX/<name> files by URL shape and logs argv.
BIN="$TMP/bin"
mkdir -p "$BIN"
cat > "$BIN/curl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FIX/curl.log"
out=""; url=""
while [ $# -gt 0 ]; do
  case "$1" in
    --output|-o) out="$2"; shift 2 ;;
    --header|-H) hdr="$2"; shift 2 ;;
    --max-time) shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
case "$url" in
  *"/pipelines?"*) src="$FIX/pipelines.json" ;;
  *"/pipelines/"*"/jobs"*) src="$FIX/jobs.json" ;;
  *"/artifacts/"*gitlab-tested-image.json) src="$FIX/evidence.json" ;;
  *"/artifacts/"*image.spdx.json) src="$FIX/sbom.json" ;;
  *) echo "stub curl: unexpected URL $url" >&2; exit 22 ;;
esac
[ -f "$src" ] || exit 22
if [ -n "$out" ]; then cp "$src" "$out"; else cat "$src"; fi
STUB
for t in wget ssh git nc; do
  printf '#!/usr/bin/env bash\necho "%s called: $*" >> "$FIX/forbidden.log"\nexit 99\n' "$t" > "$BIN/$t"
done
chmod +x "$BIN"/*

NODE_DIR="$(dirname "$(command -v node)")"

# fixture NAME [overrides...] builds $TMP/NAME with a green pipeline 77.
fixture() {
  local d="$TMP/$1"; shift
  mkdir -p "$d"
  : > "$d/curl.log"
  printf '[{"id":77,"status":"success","web_url":"https://gitlab.example.test/ai/mikroview/-/pipelines/77"}]' > "$d/pipelines.json"
  printf '[{"id":1,"name":"publish:image","status":"success"},{"id":2,"name":"sign:release-digest","status":"success"},{"id":3,"name":"publish:channel","status":"success"},{"id":4,"name":"security:trivy-image","status":"success"},{"id":5,"name":"lint","status":"success"}]' > "$d/jobs.json"
  printf '{"imageReference":"%s/ai/mikroview@%s","imageDigest":"%s","commit":"%s","ref":"preview","pipelineId":77,"pipelineUrl":"https://gitlab.example.test/ai/mikroview/-/pipelines/77","builtAt":"%s"}' \
    "$REGISTRY" "$DIGEST" "$DIGEST" "$SHA" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$d/evidence.json"
  printf '{"spdxVersion":"SPDX-2.3","SPDXID":"SPDXRef-DOCUMENT"}' > "$d/sbom.json"
  echo "$d"
}

# run DIR REF -> sets rc, OUT (stdout+stderr), and leaves the evidence dir in DIR/ev
run() {
  local d="$1" ref="$2"
  rc=0
  OUT="$(env -i PATH="$BIN:$NODE_DIR:/usr/bin:/bin" FIX="$d" HOME="$d" \
    GITLAB_API_URL="https://gitlab.example.test/api/v4" GITLAB_PROJECT_ID=53 \
    GITLAB_READ_TOKEN="$TOKEN" GITLAB_REGISTRY="$REGISTRY" \
    MV_COMMIT="${MV_COMMIT_OVERRIDE:-$SHA}" MV_REF="$ref" MV_WAIT_MINUTES=0 MV_POLL_SECONDS=1 \
    MV_EVIDENCE_DIR="$d/ev" GITHUB_OUTPUT="$d/gh-output" \
    timeout 60 bash "$SCRIPT" 2>&1)" || rc=$?
}
refused() { # DIR WHAT
  [ "$rc" -ne 0 ] && ok "$2: refuses (exit $rc)" || bad "$2: expected a refusal, exit was 0"
  if grep -q "$DIGEST" "$1/gh-output" 2>/dev/null; then bad "$2: digest was still handed on via GITHUB_OUTPUT"; else ok "$2: no digest handed on"; fi
  [ ! -s "$1/forbidden.log" ] && ok "$2: no other network tool used" || bad "$2: forbidden tool called"
  case "$OUT" in *"$TOKEN"*) bad "$2: output contains the token" ;; *) ok "$2: token not printed" ;; esac
}

[ -f "$SCRIPT" ] || { bad "scripts/ci/gitlab-await-tested-image.sh does not exist"; exit 1; }

# 1. Success path, preview and a tag.
for ref in preview v1.2.3; do
  d="$(fixture "ok-$ref")"
  sed -i "s/\"ref\":\"preview\"/\"ref\":\"$ref\"/" "$d/evidence.json"
  run "$d" "$ref"
  check "$rc" "0" "$ref: success path exits 0"
  case "$OUT" in *"$DIGEST"*) ok "$ref: prints the digest" ;; *) bad "$ref: digest not printed" ;; esac
  case "$OUT" in *"$TOKEN"*) bad "$ref: output contains the token" ;; *) ok "$ref: token not printed" ;; esac
  grep -q "$DIGEST" "$d/gh-output" 2>/dev/null && ok "$ref: digest in GITHUB_OUTPUT" || bad "$ref: digest not in GITHUB_OUTPUT"
  [ -s "$d/ev/gitlab-tested-image.json" ] && [ -s "$d/ev/image.spdx.json" ] && ok "$ref: evidence and SBOM downloaded" || bad "$ref: evidence/SBOM not downloaded"
  grep -q "$TOKEN" "$d/curl.log" && bad "$ref: token on a curl command line" || ok "$ref: token not on any curl command line"
  grep -q "projects/53/" "$d/curl.log" && ok "$ref: queries project 53" || bad "$ref: never queried project 53"
done

# 2. Each required job missing, then each failed.
for name in publish:image sign:release-digest publish:channel security:trivy-image; do
  d="$(fixture "missing-$name")"
  node -e 'const f=process.argv[1],n=process.argv[2];const j=JSON.parse(require("fs").readFileSync(f));require("fs").writeFileSync(f,JSON.stringify(j.filter(x=>x.name!==n)))' "$d/jobs.json" "$name"
  run "$d" preview
  refused "$d" "missing job $name"
  d="$(fixture "failed-$name")"
  node -e 'const f=process.argv[1],n=process.argv[2];const j=JSON.parse(require("fs").readFileSync(f));j.forEach(x=>{if(x.name===n)x.status="failed"});require("fs").writeFileSync(f,JSON.stringify(j))' "$d/jobs.json" "$name"
  run "$d" preview
  refused "$d" "failed job $name"
done

# 3. Pipeline not green, or absent.
d="$(fixture failed-pipeline)"; sed -i 's/"success","web_url"/"failed","web_url"/' "$d/pipelines.json"; run "$d" preview; refused "$d" "failed pipeline"
d="$(fixture canceled-pipeline)"; sed -i 's/"success","web_url"/"canceled","web_url"/' "$d/pipelines.json"; run "$d" preview; refused "$d" "canceled pipeline"
d="$(fixture no-pipeline)"; printf '[]' > "$d/pipelines.json"; run "$d" preview; refused "$d" "no pipeline for the commit"

# 4. Evidence: oversized, bad digest, wrong commit/ref/pipeline.
d="$(fixture oversized)"
node -e 'const f=process.argv[1];const e=JSON.parse(require("fs").readFileSync(f));e.padding="x".repeat(65*1024);require("fs").writeFileSync(f,JSON.stringify(e))' "$d/evidence.json"
run "$d" preview; refused "$d" "evidence over 64 KiB"

for bad_digest in "sha512:$(printf 'c%.0s' $(seq 64))" "sha256:$(printf 'c%.0s' $(seq 63))" "sha256:$(printf 'C%.0s' $(seq 64))" "latest" ""; do
  d="$(fixture "baddigest-${bad_digest:0:12}")"
  node -e 'const f=process.argv[1];const e=JSON.parse(require("fs").readFileSync(f));e.imageDigest=process.argv[2];e.imageReference=process.argv[3]+"@"+process.argv[2];require("fs").writeFileSync(f,JSON.stringify(e))' "$d/evidence.json" "$bad_digest" "$REGISTRY/ai/mikroview"
  run "$d" preview; refused "$d" "non-sha256 digest '${bad_digest:0:14}'"
done

d="$(fixture wrong-commit)"; sed -i "s/\"commit\":\"$SHA\"/\"commit\":\"$(printf 'b%.0s' $(seq 40))\"/" "$d/evidence.json"; run "$d" preview; refused "$d" "evidence for another commit"
d="$(fixture wrong-ref)"; sed -i 's/"ref":"preview"/"ref":"main"/' "$d/evidence.json"; run "$d" preview; refused "$d" "evidence for another ref"
d="$(fixture wrong-pipeline)"; sed -i 's/"pipelineId":77/"pipelineId":78/' "$d/evidence.json"; run "$d" preview; refused "$d" "evidence for another pipeline"
d="$(fixture not-json)"; printf 'not json' > "$d/evidence.json"; run "$d" preview; refused "$d" "evidence that is not JSON"
d="$(fixture other-registry)"; sed -i "s#$REGISTRY#evil.example.test#g" "$d/evidence.json"; run "$d" preview; refused "$d" "image outside the project registry"
d="$(fixture no-sbom)"; rm "$d/sbom.json"; run "$d" preview; refused "$d" "missing SBOM"

# 5. Refs other than preview or v*: refused before any API call.
for ref in dev main feature/x preview2 zz/refs/heads/preview refs/heads/preview mv1.2.3 "v1 2" ""; do
  d="$(fixture "ref-$(printf '%s' "$ref" | tr -c 'A-Za-z0-9' _)")"
  run "$d" "$ref"
  refused "$d" "ref '$ref'"
  [ ! -s "$d/curl.log" ] && ok "ref '$ref': no API call made" || bad "ref '$ref': the API was called"
done

# 6. Token never in output even on a failing API call.
d="$(fixture api-down)"; rm "$d/pipelines.json"; run "$d" preview; refused "$d" "GitLab API failure"

exit "$fail"
