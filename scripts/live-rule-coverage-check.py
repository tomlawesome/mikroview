#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
#
# Reads GET /api/routeros/{device}/rules on stdin and checks that the
# three fields #274 needs actually survived the round trip from a real
# router: `log`, `dstAddress` and `srcAddress`.
#
# Separate from live-rule-coverage-probe.sh rather than embedded in it.
# The first version of this was python inside bash inside a subshell, and
# its quoting broke -- which the surrounding pipeline then swallowed, so
# it reported success having checked nothing.

import json
import sys


def main() -> int:
    doc = json.load(sys.stdin)
    if not doc.get("available"):
        print("FAIL: the device reports no pushed filter table")
        return 1

    rules = doc.get("rules", [])
    print(f"{len(rules)} rules pushed by the router\n")
    for r in rules:
        print(
            f"  ordinal={r['ordinal']:<2} "
            f"log={str(r.get('log', False)):<5} "
            f"dstAddress={r.get('dstAddress', '') or '-':<20} "
            f"srcAddress={r.get('srcAddress', '') or '-':<18} "
            f"prefix={r.get('logPrefix', '') or '-'}"
        )
    print()

    failures = []

    # Every address shape has to arrive intact, negation included: "not
    # this range" is the opposite of "this range", so a coverage check
    # that loses the "!" answers backwards.
    addresses = {r.get("dstAddress") for r in rules}
    for want in ("203.0.113.9", "!10.0.0.0/8", "10.0.0.1-10.0.0.5", "198.51.100.0/24"):
        if want not in addresses:
            failures.append(f"dstAddress {want!r} did not survive the push")

    if not any(r.get("srcAddress") == "192.168.88.0/24" for r in rules):
        failures.append("srcAddress did not survive the push")

    # The field the whole feature rests on. A rule with log=no feeds
    # mikroview nothing whatever else it matches, and there must be
    # exactly one such rule here.
    silent = [r for r in rules if not r.get("log", False)]
    if len(silent) != 1:
        failures.append(
            f"expected exactly one non-logging rule, got {len(silent)} "
            "-- either log is not arriving, or it is arriving as true for everything"
        )
    elif silent[0].get("dstAddress") != "198.51.100.0/24":
        failures.append(f"the non-logging rule is not the one added as silent: {silent[0]}")

    logging_rules = [r for r in rules if r.get("log", False)]
    if not logging_rules:
        failures.append("no rule arrived with log=true, so nothing distinguishes them")

    if failures:
        for f in failures:
            print(f"FAIL: {f}")
        return 1

    print("PASS: log, dstAddress and srcAddress all survive a real router's push,")
    print("      including the negated form and the non-logging rule.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
