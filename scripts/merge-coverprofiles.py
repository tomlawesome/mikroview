#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-only
"""Merge Go text coverage profiles into one (#1451).

test:go and test:postgres measure coverage twice: `go test -coverprofile`
for code the tests call directly, and `go tool covdata textfmt` for code
run by the real program the tests start (built with -cover, see
internal/authe2e's coverDir). The same block appears in both, and
coverage-floor.py sums every line it reads, so concatenating the files
would count a block's statements twice. This writes each block once, with
its counts added: covered if either run covered it.

Usage: merge-coverprofiles.py <out> <profile> [<profile>...]

Every input must carry the same mode: line (atomic under -race). A block
listed with two different statement counts means the profiles came from
different sources; both fail red (exit 2) rather than merge into a number
nobody measured.
"""
import sys


def fail(message):
    print(f"merge-coverprofiles: {message}", file=sys.stderr)
    raise SystemExit(2)


def merge(paths):
    mode = None
    counts = {}  # "file:start,end numstmt" -> count, in first-seen order
    numstmts = {}  # "file:start,end" -> numstmt, to catch a mismatch
    for path in paths:
        try:
            with open(path) as f:
                lines = f.read().splitlines()
        except OSError as err:
            fail(f"cannot read {path}: {err}")
        if not lines or not lines[0].startswith("mode: "):
            fail(f"{path} has no 'mode:' header line")
        if mode is None:
            mode = lines[0]
        elif lines[0] != mode:
            fail(f"{path} has '{lines[0]}', the first profile '{mode}'")
        for lineno, line in enumerate(lines[1:], start=2):
            if not line.strip():
                continue
            try:
                block, numstmt, count = line.rsplit(" ", 2)
                int(numstmt)
                count = int(count)
            except ValueError:
                fail(f"{path}:{lineno}: cannot parse {line!r}")
            if numstmts.setdefault(block, numstmt) != numstmt:
                fail(f"{path}:{lineno}: {block} has {numstmt} statements, "
                     f"{numstmts[block]} in an earlier profile")
            key = f"{block} {numstmt}"
            if mode == "mode: set":
                counts[key] = max(counts.get(key, 0), count)
            else:
                counts[key] = counts.get(key, 0) + count
    return mode, counts


def main(argv):
    if len(argv) < 2:
        print("usage: merge-coverprofiles.py <out> <profile> [<profile>...]",
              file=sys.stderr)
        return 2
    mode, counts = merge(argv[1:])
    with open(argv[0], "w") as out:
        out.write(mode + "\n")
        for key, count in counts.items():
            out.write(f"{key} {count}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
