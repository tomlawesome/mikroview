#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# Static checks for #1563: GitHub's runners cannot reach the owner's
# GitLab, so the three workflows that used to pull from it read only GHCR,
# and .gitlab-ci.yml gains the publish:ghcr job that pushes there.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

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
# Standard library only (the gate image has no PyYAML): each job's block is
# cut as text, from its `name:` line to the next line starting with a
# character that is neither a space nor `#`.
py_out="$(python3 -I - <<'PY'
import re

lines = open(".gitlab-ci.yml").read().split("\n")

def block(name):
    for i, line in enumerate(lines):
        if line == name + ":":
            out = []
            for nxt in lines[i + 1:]:
                if nxt and nxt[0] not in " #":
                    break
                out.append(nxt)
            return "\n".join(out)
    return None

def code(text):
    return "\n".join(l for l in text.split("\n") if not l.lstrip().startswith("#"))

job = block("publish:ghcr")
sign = block("sign:release-digest")
print("exists=" + ("yes" if job is not None else "no"))
if job is None:
    raise SystemExit
job = code(job)
needs = re.search(r"^  needs:\n((?:    .*\n?|  #.*\n?)*)", job + "\n", re.M)
print("needs_channel=" + ("yes" if needs and re.search(r"^\s+- (job: )?publish:channel\b|job: publish:channel\b", needs.group(1), re.M) else "no"))
rules = re.search(r"^  rules:\n((?:    .*\n?)*)", job + "\n", re.M)
r = rules.group(1) if rules else ""
protected = "publish_ref" in r or ("preview" in r and "v[0-9" in r)
print("protected_refs=" + ("yes" if protected else "no"))
tag_re = r"^  tags:.*$"
jt = re.search(tag_re, job, re.M)
st = re.search(tag_re, code(sign or ""), re.M)
print("tags=" + ("yes" if jt and st and jt.group(0) == st.group(0) else "no"))
# CI_REPOSITORY_URL carries the job token: never in the job
print("repourl=" + ("no" if "CI_REPOSITORY_URL" in job else "yes"))
print("script=" + ("yes" if "scripts/publish-ghcr.sh" in job else "no"))
PY
)"
get() { printf '%s\n' "$py_out" | sed -n "s/^$1=//p"; }
check "$(get exists)" "yes" ".gitlab-ci.yml defines job publish:ghcr"
check "$(get needs_channel)" "yes" "publish:ghcr needs publish:channel"
check "$(get protected_refs)" "yes" "publish:ghcr runs only on protected refs (preview push or v* tag)"
check "$(get tags)" "yes" "publish:ghcr uses the signing runner's tags (as sign:release-digest)"
check "$(get repourl)" "yes" "publish:ghcr script does not use CI_REPOSITORY_URL (it carries the job token)"
check "$(get script)" "yes" "publish:ghcr script calls scripts/publish-ghcr.sh"

exit "$fail"
