#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
#
# #1537 (item 2): what a merge-request pipeline into batch-a / batch-b runs,
# read from .gitlab-ci.yml itself.
#   1. test:build-checks runs (cheap, light runner);
#   2. the heavy jobs do not;
#   3. every job that does run names `needs:` explicitly, so it starts at
#      once or waits only on the jobs whose output it uses.
# Needs python3 with PyYAML (lint:script-tests installs py3-yaml).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CI_FILE="${CI_FILE:-$HERE/../.gitlab-ci.yml}"
exec python3 - "$CI_FILE" <<'PY'
import os, re, subprocess, sys
import yaml

ci_path = sys.argv[1]

class Loader(yaml.SafeLoader):
    pass
Loader.add_constructor("!reference", lambda l, n: {"__ref__": l.construct_sequence(n)})
doc = yaml.load(open(ci_path), Loader)

# The included agent-infra templates cannot be fetched in CI, so their rules
# are copied here, pinned by the include refs checked below. When a local
# agent-infra clone exists the copies are checked against the real files.
PINS = {
    "scripts/batch-intake/ci-template.yml": "063b5b95a96b139b0a858bb009466456a19e5674",
    "scripts/catchup-check/ci-template.yml": "03499313d89b421212af02db3735662397eebfb3",
    "scripts/dev-landing/ci-template.yml": "1dca70057bd93f68ded22c6b159f315c987c1663",
}
TEMPLATE_RULES = yaml.safe_load("""
.batch-intake:
  workflow_rules:
    - if: >-
        $CI_PIPELINE_SOURCE == "merge_request_event" &&
        ($CI_MERGE_REQUEST_TARGET_BRANCH_NAME == "batch-a" || $CI_MERGE_REQUEST_TARGET_BRANCH_NAME == "batch-b")
      variables: {BATCH_INTAKE: "true"}
  job_rule:
    - {if: '$BATCH_INTAKE == "true"', when: never}
.catchup-check:
  workflow_rules:
    - if: >-
        $CI_PIPELINE_SOURCE == "merge_request_event" &&
        $CI_MERGE_REQUEST_SOURCE_BRANCH_NAME =~ /^chore\\/catchup-/ &&
        ($CI_MERGE_REQUEST_TARGET_BRANCH_NAME == "batch-a" || $CI_MERGE_REQUEST_TARGET_BRANCH_NAME == "batch-b")
      variables: {CATCHUP_CHECK: "true"}
  job_rule:
    - {if: '$CATCHUP_CHECK == "true"', when: never}
.dev-landing-check:
  timeout: 5m
  rules:
    - {if: '$DEV_LANDING == "true"'}
.dev-landing:
  workflow_rules:
    - if: >-
        $CI_PIPELINE_SOURCE == "push" &&
        ($CI_COMMIT_BRANCH == "dev" || $CI_COMMIT_BRANCH == "primary")
      variables: {DEV_LANDING: "true"}
  job_rule:
    - {if: '$DEV_LANDING == "true"', when: never}
""")

fails = 0
def check(ok, label, detail=""):
    global fails
    if ok:
        print("ok - " + label)
    else:
        fails += 1
        print("FAIL - " + label + (": " + detail if detail else ""))

# The include refs must still be the ones the copies above stand for.
inc = {i["file"]: i["ref"] for i in doc.get("include", []) if isinstance(i, dict) and i.get("project") == "ai/agent-infra"}
for f, ref in PINS.items():
    check(inc.get(f) == ref, "include of %s is pinned at the recorded ref" % f,
          "got %s, update the copy in this test with the new template" % inc.get(f))
    clone = os.path.expanduser("~/projects/agent-infra")
    if os.path.isdir(clone):
        r = subprocess.run(["git", "-C", clone, "show", "%s:%s" % (inc.get(f), f)], capture_output=True, text=True)
        if r.returncode == 0:
            real = yaml.safe_load(r.stdout)
            for k, v in real.items():
                if k.startswith(".") and k in TEMPLATE_RULES:
                    check(v == TEMPLATE_RULES[k], "copied rules of %s match the real template" % k)

# ---- resolving !reference and extends --------------------------------------
def top(name):
    return doc[name] if name in doc else TEMPLATE_RULES[name]

def resolve(v):
    if isinstance(v, dict) and "__ref__" in v:
        name, *path = v["__ref__"]
        t = top(name)
        for p in path:
            t = t[p]
        return resolve(t)
    if isinstance(v, list):
        out = []
        for x in v:
            r = resolve(x)
            if isinstance(x, dict) and "__ref__" in x and isinstance(r, list):
                out.extend(r)
            else:
                out.append(r)
        return out
    if isinstance(v, dict):
        return {k: resolve(x) for k, x in v.items()}
    return v

def job(name):
    j = dict(doc[name]) if name in doc else dict(TEMPLATE_RULES[name])
    ext = j.get("extends")
    if ext:
        merged = {}
        for e in ([ext] if isinstance(ext, str) else ext):
            merged.update(job(e))
        merged.update(j)
        j = merged
    return j

# ---- evaluating rules -------------------------------------------------------
def make_eval(vars_):
    def V(n):
        return vars_.get(n)
    def M(n, rx, neg):
        v = vars_.get(n)
        hit = v is not None and re.search(rx.replace("\\/", "/"), v) is not None
        return (not hit) if neg else hit
    def ev(expr):
        e = re.sub(r"\$(\w+)\s*(=~|!~)\s*/((?:\\/|[^/])*)/", lambda m: "M(%r,%r,%s)" % (m[1], m[3], m[2] == "!~"), expr)
        e = re.sub(r"\$(\w+)", lambda m: "V(%r)" % m[1], e)
        e = e.replace("&&", " and ").replace("||", " or ").replace("null", "None")
        return bool(eval(e, {"V": V, "M": M, "None": None}))
    return ev

def pipeline_vars(target):
    base = {"CI_PIPELINE_SOURCE": "merge_request_event",
            "CI_MERGE_REQUEST_TARGET_BRANCH_NAME": target,
            "CI_MERGE_REQUEST_SOURCE_BRANCH_NAME": "feature/x"}
    ev = make_eval(base)
    for r in resolve(doc["workflow"]["rules"]):
        if "if" in r and not ev(r["if"]):
            continue
        base.update(r.get("variables", {}))
        break
    return base

def runs(name, vars_, changes):
    """First matching rule decides. `changes:` cannot be known offline, so it
    is tried both ways; `exists:` is read from the checkout."""
    ev = make_eval(vars_)
    root = os.path.dirname(os.path.abspath(ci_path))
    for r in resolve(job(name).get("rules", [{"when": "on_success"}])):
        if "if" in r and not ev(r["if"]):
            continue
        if "changes" in r and not changes:
            continue
        if "exists" in r and not any(os.path.exists(os.path.join(root, p)) for p in r["exists"]):
            continue
        return r.get("when", "on_success") != "never"
    return False

jobs = [k for k, v in doc.items() if isinstance(v, dict) and not k.startswith(".")
        and k not in ("default", "workflow", "variables") and ("script" in job(k) or "trigger" in job(k) or "extends" in job(k))]

CHEAP = ["test:build-checks"]
HEAVY = ["test:go", "test:postgres", "test:frontend", "security:fuzz",
         "gate:scripts", "gate:scenarios", "gate:scenarios:firefox",
         "gate:scenarios:webkit", "gate:scenarios:quarantined",
         # skipped on purpose: they need the built image (plan, 2026-11-20)
         "build:image", "gate:image", "test:install-line", "security:trivy-image", "test:container"]

for target in ("batch-a", "batch-b"):
    vars_ = pipeline_vars(target)
    check(vars_.get("BATCH_INTAKE") == "true", "[%s] MR pipeline is a batch-intake pipeline" % target)
    for changes in (True, False):
        tag = "[%s, %s]" % (target, "code changed" if changes else "no path matched")
        for n in CHEAP:
            check(n in doc and runs(n, vars_, changes), "%s %s runs on a batch merge" % (tag, n))
        for n in HEAVY:
            check(n in doc, "%s %s still exists" % (tag, n))
            if n in doc:
                check(not runs(n, vars_, changes), "%s %s does not run on a batch merge" % (tag, n))
        running = [n for n in jobs if runs(n, vars_, changes)]
        check(bool(running), "%s some job runs" % tag)
        for n in running:
            check("needs" in job(n), "%s %s has explicit needs:" % (tag, n),
                  "no needs: key, so it waits for every earlier stage")

sys.exit(1 if fails else 0)
PY
