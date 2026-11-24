#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
"""Fail on a reachable Go vulnerability unless it is recorded as not affected.

Usage: govulncheck-gate.py <govulncheck.json> <not-affected.json>

govulncheck has no way to accept a finding, so security:govulncheck runs it
with -format=json and gates here instead (#1558). The not-affected file is
a reviewed JSON list of {"id", "reason", "issue"}: the reason says why the
vulnerable code is not in MikroView's execute path (the VEX
"vulnerable_code_not_in_execute_path" status) and the issue tracks moving
off the vulnerable version. An entry no reachable finding names any more is
stale and fails the gate, so the list cannot outlive the reason for it.
"""
import json
import sys


def read_stream(text):
    """govulncheck -format=json writes concatenated JSON objects."""
    dec = json.JSONDecoder()
    i, n = 0, len(text)
    while True:
        while i < n and text[i].isspace():
            i += 1
        if i >= n:
            return
        obj, i = dec.raw_decode(text, i)
        yield obj


def reachable_ids(path):
    ids = set()
    with open(path, encoding="utf-8") as f:
        for obj in read_stream(f.read()):
            finding = obj.get("finding") if isinstance(obj, dict) else None
            if not isinstance(finding, dict):
                continue
            trace = finding.get("trace") or []
            if trace and isinstance(trace[0], dict) and trace[0].get("function"):
                ids.add(finding.get("osv"))
    return ids


def main(argv):
    if len(argv) != 3:
        print(__doc__.strip().splitlines()[2], file=sys.stderr)
        return 2
    found = reachable_ids(argv[1])
    with open(argv[2], encoding="utf-8") as f:
        entries = json.load(f)
    problems = []
    if not isinstance(entries, list):
        problems.append(f"{argv[2]}: must be a JSON list")
        entries = []
    accepted = {}
    for e in entries:
        if not (isinstance(e, dict) and all(isinstance(e.get(k), str) and e[k].strip()
                                            for k in ("id", "reason", "issue"))):
            problems.append(f"{argv[2]}: entry {e!r} needs a non-empty id, reason and issue")
            continue
        accepted[e["id"]] = e
    for osv in sorted(found - set(accepted)):
        problems.append(f"{osv}: reachable vulnerability, not recorded as not affected")
    for osv in sorted(set(accepted) - found):
        problems.append(f"{osv}: listed as not affected but no longer found -- remove the entry")
    for osv in sorted(found & set(accepted)):
        e = accepted[osv]
        print(f"not affected: {osv} ({e['issue']}): {e['reason']}")
    for p in problems:
        print(f"FAIL: {p}")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
