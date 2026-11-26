#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Static checks for #1563: GitHub's runners cannot reach the owner's
# GitLab, so the three workflows that used to pull from it read only GHCR,
# and .gitlab-ci.yml gains the publish:ghcr job that pushes there.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

fail=0
check() {
  if [ "$1" = "$2" ]; then echo "ok - $3"; else echo "FAIL - $3: expected [$2] got [$1]"; fail=1; fi
}
absent() {  # absent <file> <fixed string>
  if [ -f "$1" ] && ! grep -qF -- "$2" "$1"; then echo yes; else echo no; fi
}

WF=.github/workflows
for f in publish-from-gitlab.yml release-on-tag.yml countersign.yml; do
  for needle in gitlab.tomlawson.io registry.tomlawson.io GITLAB_READ_TOKEN gitlab-await-tested-image; do
    check "$(absent "$WF/$f" "$needle")" "yes" "$f does not mention $needle"
  done
done

for f in publish-from-gitlab.yml release-on-tag.yml; do
  ok=no
  if [ -f "$WF/$f" ] && grep -E 'verify-release-signature\.sh' "$WF/$f" | grep -qF 'ghcr.io/'; then ok=yes
  elif [ -f "$WF/$f" ] && grep -qF 'verify-release-signature.sh' "$WF/$f" &&
       python3 -I - "$WF/$f" <<'PY'
import re, sys
text = open(sys.argv[1]).read()
# a verify-release-signature.sh call whose argument list (continuations
# included) names a ghcr.io/ reference
for m in re.finditer(r"verify-release-signature\.sh((?:[^\n]*\\\n)*[^\n]*)", text):
    if "ghcr.io/" in m.group(1):
        sys.exit(0)
sys.exit(1)
PY
  then ok=yes; fi
  check "$ok" "yes" "$f runs verify-release-signature.sh against a ghcr.io/ reference"
done

# --- .gitlab-ci.yml: the publish:ghcr job ---
if python3 -c 'import yaml' 2>/dev/null; then
  py_out="$(python3 -I - <<'PY'
import yaml, sys

class L(yaml.SafeLoader):
    pass
L.add_constructor("!reference", lambda loader, node: {"__reference__": loader.construct_sequence(node)})
doc = yaml.load(open(".gitlab-ci.yml"), Loader=L)

def lines(job):
    out = []
    for key in ("before_script", "script", "after_script"):
        v = job.get(key) or []
        out += [str(x) for x in (v if isinstance(v, list) else [v])]
    return "\n".join(out)

def flat(x):
    return yaml.safe_dump(x) if x is not None else ""

job = doc.get("publish:ghcr") if isinstance(doc, dict) else None
print("exists=" + ("yes" if isinstance(job, dict) else "no"))
if not isinstance(job, dict):
    sys.exit(0)
needs = job.get("needs") or []
names = [n if isinstance(n, str) else n.get("job") for n in needs]
print("needs_channel=" + ("yes" if "publish:channel" in names else "no"))
rules = flat(job.get("rules"))
# rules either spell the refs out or reuse .publish_ref (preview push or v* tag)
pub = flat(doc.get(".publish_ref"))
protected = ("publish_ref" in rules) or ('"preview"' in rules or "preview" in rules) and "v[0-9" in rules
print("protected_refs=" + ("yes" if protected and "publish_ref" in rules + pub or protected else "no"))
sign_tags = doc["sign:release-digest"].get("tags")
print("tags=" + ("yes" if sign_tags and job.get("tags") == sign_tags else "no"))
# CI_REPOSITORY_URL carries the job token: never on a command line
print("repourl=" + ("no" if "CI_REPOSITORY_URL" in lines(job) else "yes"))
print("script=" + ("yes" if "scripts/publish-ghcr.sh" in lines(job) else "no"))
PY
)"
  get() { printf '%s\n' "$py_out" | sed -n "s/^$1=//p"; }
  check "$(get exists)" "yes" ".gitlab-ci.yml defines job publish:ghcr"
  check "$(get needs_channel)" "yes" "publish:ghcr needs publish:channel"
  check "$(get protected_refs)" "yes" "publish:ghcr runs only on protected refs (preview push or v* tag)"
  check "$(get tags)" "yes" "publish:ghcr uses the signing runner's tags (as sign:release-digest)"
  check "$(get repourl)" "yes" "publish:ghcr script does not use CI_REPOSITORY_URL (it carries the job token)"
  check "$(get script)" "yes" "publish:ghcr script calls scripts/publish-ghcr.sh"
else
  echo "FAIL - PyYAML is not available; cannot check .gitlab-ci.yml structure"; fail=1
fi

exit "$fail"
