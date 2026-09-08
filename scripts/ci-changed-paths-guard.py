#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
"""Refuse a diff that the docs-only lane (#1038) would misread.

.gitlab-ci.yml skips most jobs when every changed path is in
*docs_only_paths and none is in *code_paths. `changes:` cannot say "only
these", so a path in neither list is invisible to it: on its own it runs
the full set (harmless), but paired with a README edit it is read as
documentation and the pipeline skips (not harmless, and silent). This
guard runs on every merge request and dev pipeline, unconditionally, and
turns that case into a red job naming the path.

The lists are read from .gitlab-ci.yml itself, so there is one copy.

Modes:
  --stdin   changed paths, one per line, on stdin (what the tests use)
  --git     resolve the diff base from GitLab's variables and ask git

Exit codes: 0 nothing to report; 1 a changed path is in neither list;
2 the lists could not be read (fail red, never quietly green).

In --git mode a base that is absent, all zeros (first push of a branch)
or not in the clone passes with a note: in exactly those cases GitLab's
own `changes:` evaluates to true and every job runs, so there is
nothing here to guard.
"""
import argparse
import os
import re
import subprocess
import sys

DOCS_KEY = ".docs_only_paths"
CODE_KEY = ".code_paths"


def glob_re(pat):
    out, i = "", 0
    while i < len(pat):
        if pat.startswith("**/", i):
            out += "(?:[^/]+/)*"
            i += 3
        elif pat.startswith("**", i):
            out += ".*"
            i += 2
        elif pat[i] == "*":
            out += "[^/]*"
            i += 1
        elif pat[i] == "?":
            out += "[^/]"
            i += 1
        else:
            out += re.escape(pat[i])
            i += 1
    return re.compile("^" + out + "$")


def load_lists(ci_file):
    try:
        import yaml
    except ImportError:
        print("guard: PyYAML is not installed -- the job image must provide py3-yaml", file=sys.stderr)
        sys.exit(2)
    with open(ci_file, encoding="utf-8") as fh:
        doc = yaml.safe_load(fh)
    lists = []
    for key in (DOCS_KEY, CODE_KEY):
        pats = doc.get(key) if isinstance(doc, dict) else None
        if not isinstance(pats, list) or not pats or not all(isinstance(p, str) for p in pats):
            print(f"guard: {key} in {ci_file} is missing or not a list of strings", file=sys.stderr)
            sys.exit(2)
        lists.append([glob_re(p) for p in pats])
    return lists


def unlisted(paths, docs_rx, code_rx):
    return [p for p in paths if not any(r.match(p) for r in docs_rx + code_rx)]


def changed_from_git():
    src = os.environ.get("CI_PIPELINE_SOURCE", "")
    if src == "merge_request_event":
        base = os.environ.get("CI_MERGE_REQUEST_DIFF_BASE_SHA", "")
        what = "CI_MERGE_REQUEST_DIFF_BASE_SHA"
    else:
        base = os.environ.get("CI_COMMIT_BEFORE_SHA", "")
        what = "CI_COMMIT_BEFORE_SHA"
    if not base or set(base) == {"0"}:
        print(f"guard: {what} is {base or 'unset'} -- no diff base, so GitLab's changes: "
              "runs every job and there is nothing to guard. PASS.")
        return None
    probe = subprocess.run(["git", "cat-file", "-e", f"{base}^{{commit}}"],
                           capture_output=True)
    if probe.returncode != 0:
        print(f"guard: {what}={base} is not in this clone -- GitLab's changes: "
              "runs every job when it cannot diff either. PASS.")
        return None
    out = subprocess.run(["git", "diff", "--name-only", "--no-renames", base, "HEAD"],
                         capture_output=True, text=True, check=True).stdout
    print(f"guard: diffing {base[:12]}..HEAD")
    return [line for line in out.splitlines() if line]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--ci-file", default=os.path.join(
        os.path.dirname(os.path.dirname(os.path.abspath(__file__))), ".gitlab-ci.yml"))
    mode = ap.add_mutually_exclusive_group(required=True)
    mode.add_argument("--stdin", action="store_true")
    mode.add_argument("--git", action="store_true")
    args = ap.parse_args()

    docs_rx, code_rx = load_lists(args.ci_file)
    if args.git:
        paths = changed_from_git()
        if paths is None:
            return 0
    else:
        paths = [line.strip() for line in sys.stdin if line.strip()]

    bad = unlisted(paths, docs_rx, code_rx)
    if not bad:
        print(f"guard: {len(paths)} changed path(s), all in {DOCS_KEY} or {CODE_KEY}. PASS.")
        return 0
    print(f"guard: {len(bad)} changed path(s) match neither {DOCS_KEY} nor {CODE_KEY}:",
          file=sys.stderr)
    for p in bad:
        print(f"  {p}", file=sys.stderr)
    print("Add the tree or extension to *code_paths in .gitlab-ci.yml. Until it is "
          "listed, a diff pairing it with a README edit would be read as "
          "documentation and skip most of the pipeline (#1038).", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
